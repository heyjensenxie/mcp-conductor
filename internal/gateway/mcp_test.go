package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xmj128/mcp-conductor/internal/access"
	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/balancer"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/observability"
	"github.com/xmj128/mcp-conductor/internal/policy"
	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/router"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

// fakeCaller 记录上游调用参数，验证 Grant 的 default_args/Headers 生效。
type fakeCaller struct {
	capturedArgs map[string]any
	capturedHdrs map[string]string
}

func (f *fakeCaller) Call(_ context.Context, _ model.Server, _ string, args map[string]any, hdrs map[string]string) ([]registry.CallContent, error) {
	f.capturedArgs = args
	f.capturedHdrs = hdrs
	return []registry.CallContent{{Type: "text", Text: "ok"}}, nil
}

// seedGateway 构造一个带单 Server + 两工具的内存网关，返回网关与捕获器。
func seedGateway(t *testing.T, callers ...*fakeCaller) *MCPGateway {
	t.Helper()
	store := memory.New()
	now := time.Now().UTC()
	if err := store.CreateServer(context.Background(), &model.Server{
		ID: "srv-1", Name: "mock", Endpoint: "http://localhost:9000/mcp",
		Transport: model.TransportStreamableHTTP, Enabled: true,
		HealthStatus: model.ServerStatusHealthy, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	for _, spec := range []struct{ id, name string }{
		{"tool-1", "search"},
		{"tool-2", "detail"},
	} {
		if err := store.UpsertTool(context.Background(), &model.Tool{
			ID: spec.id, ServerID: "srv-1", OriginalName: spec.name,
			GatewayName: "mock." + spec.name, Enabled: true, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("UpsertTool: %v", err)
		}
	}

	var caller registry.ToolCaller
	if len(callers) > 0 && callers[0] != nil {
		caller = callers[0]
	} else {
		caller = &fakeCaller{}
	}
	authz := access.NewAuthorizer(policy.NewEngine(store))
	return NewMCPGateway(
		store, router.NewResolver(store, store), balancer.NewRoundRobin(),
		caller, authz, observability.NewMetrics(),
		observability.NewRecorder(store, false, 1.0),
	)
}

// TestCallTool_appliesGrantConfig 验证 key×tool 的 default_args 合并与 headers 传递。
func TestCallTool_appliesGrantConfig(t *testing.T) {
	caller := &fakeCaller{}
	g := seedGateway(t, caller)

	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "p", Key: &model.AccessKey{
		Subject: "p",
		Grants: []model.ToolGrant{{
			GatewayName: "mock.search",
			Headers:     map[string]string{"X-Tenant": "p"},
			DefaultArgs: map[string]any{"tenant": "t-1"},
		}},
	}})

	res, err := g.CallTool(ctx, "mock.search", map[string]any{"q": "x"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应返回错误: %+v", res)
	}
	// default_args 为底、客户端同名可覆盖；此处未覆盖 tenant。
	if caller.capturedArgs["tenant"] != "t-1" || caller.capturedArgs["q"] != "x" {
		t.Fatalf("参数注入不正确: %+v", caller.capturedArgs)
	}
	if caller.capturedHdrs["X-Tenant"] != "p" {
		t.Fatalf("工具级 header 未传递: %+v", caller.capturedHdrs)
	}
}

// TestCallTool_clientOverridesDefaultArg 验证同名参数以客户端为准。
func TestCallTool_clientOverridesDefaultArg(t *testing.T) {
	caller := &fakeCaller{}
	g := seedGateway(t, caller)
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "p", Key: &model.AccessKey{
		Subject: "p",
		Grants:  []model.ToolGrant{{GatewayName: "mock.search", DefaultArgs: map[string]any{"tenant": "t-1"}}},
	}})
	if _, err := g.CallTool(ctx, "mock.search", map[string]any{"tenant": "override"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if caller.capturedArgs["tenant"] != "override" {
		t.Fatalf("客户端参数应覆盖默认值: %+v", caller.capturedArgs)
	}
}

// TestListTools_filtersByGrants 验证 managed key 的 tools/list 只返回授权工具。
func TestListTools_filtersByGrants(t *testing.T) {
	g := seedGateway(t)
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "p", Key: &model.AccessKey{
		Subject: "p",
		Grants:  []model.ToolGrant{{GatewayName: "mock.search"}},
	}})
	tools, err := g.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "mock.search" {
		t.Fatalf("白名单过滤错误: %+v", tools)
	}

	// 未管理身份（匿名）应看到全部工具。
	all, err := g.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools(anonymous): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("匿名应见全部工具，得到 %d", len(all))
	}
}

// TestCallTool_deniesUngrantedTool 验证 managed key 调用未授权工具被拒绝。
func TestCallTool_deniesUngrantedTool(t *testing.T) {
	g := seedGateway(t)
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "p", Key: &model.AccessKey{
		Subject: "p",
		Grants:  []model.ToolGrant{{GatewayName: "mock.search"}},
	}})
	res, err := g.CallTool(ctx, "mock.detail", map[string]any{})
	if err != nil {
		t.Fatalf("应返回 isError 结果而非 Go error: %v", err)
	}
	if !res.IsError {
		t.Fatal("未授权工具应返回 isError")
	}
	text := ""
	if len(res.Content) > 0 {
		text = res.Content[0].Text
	}
	if !strings.Contains(text, "authorization") {
		t.Fatalf("错误文本应含 authorization，得到 %q", text)
	}
}
