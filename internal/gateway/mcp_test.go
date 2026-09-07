package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/access"
	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/balancer"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/router"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeCaller 记录上游调用参数，验证 Grant 的 default_args/Headers 生效。
type fakeCaller struct {
	capturedArgs map[string]any
	capturedHdrs map[string]string
}

func (f *fakeCaller) Call(_ context.Context, _ model.Server, _ model.Instance, _ string, args map[string]any, hdrs map[string]string) ([]registry.CallContent, error) {
	f.capturedArgs = args
	f.capturedHdrs = hdrs
	return []registry.CallContent{{Type: "text", Text: "ok"}}, nil
}

// errCaller 返回固定上游错误，用于验证失败观测只记一次。
type errCaller struct{ err error }

func (f *errCaller) Call(context.Context, model.Server, model.Instance, string, map[string]any, map[string]string) ([]registry.CallContent, error) {
	return nil, f.err
}

// seedGatewayFull 构造带单 Server（一条 healthy 实例）+ 两工具的内存网关，
// 并返回网关与其所用的 store/metrics，便于断言观测结果（指标快照、调用日志）。
// opts 为可选的网关构造选项（如 WithInstanceProbe）。
func seedGatewayFull(t *testing.T, caller registry.ToolCaller, opts ...Option) (*MCPGateway, *memory.Store, *observability.Metrics) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()
	now := model.Now()
	if err := store.CreateServer(ctx, &model.Server{
		ID: "srv-1", Name: "mock", Enabled: true,
		HealthStatus: model.ServerStatusHealthy, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if err := store.CreateInstance(ctx, &model.Instance{
		ID: "inst-1", ServerID: "srv-1", Endpoint: "http://localhost:9000/mcp",
		Transport: model.TransportStreamableHTTP, Enabled: true,
		HealthStatus: model.ServerStatusHealthy, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	for _, spec := range []struct{ id, name string }{
		{"tool-1", "search"},
		{"tool-2", "detail"},
	} {
		if err := store.UpsertTool(ctx, &model.Tool{
			ID: spec.id, ServerID: "srv-1", OriginalName: spec.name,
			GatewayName: "mock." + spec.name, Enabled: true, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("UpsertTool: %v", err)
		}
	}

	if caller == nil {
		caller = &fakeCaller{}
	}
	metrics := observability.NewMetrics()
	authz := access.NewAuthorizer()
	resolver := router.NewResolver(store, store).WithInstances(store)
	g := NewMCPGateway(
		store, resolver, balancer.NewRoundRobin(),
		caller, authz, metrics,
		observability.NewRecorder(store, 1.0),
		opts...,
	)
	return g, store, metrics
}

// seedGateway 构造内存网关（仅返回网关，供大多数用例使用）。
func seedGateway(t *testing.T, callers ...*fakeCaller) *MCPGateway {
	t.Helper()
	var caller registry.ToolCaller
	if len(callers) > 0 && callers[0] != nil {
		caller = callers[0]
	}
	g, _, _ := seedGatewayFull(t, caller)
	return g
}

// snapshotOfKey 从指标快照中取指定维度。
func snapshotOfKey(snaps []observability.Snapshot, key string) (observability.Snapshot, bool) {
	for _, s := range snaps {
		if s.Key == key {
			return s, true
		}
	}
	return observability.Snapshot{}, false
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

// TestCallTool_upstreamError_recordsOnceAndMasksBody 验证上游失败时工具与
// Server 维度各只计一次（防重复计数），且调用日志错误列为脱敏摘要而非正文。
func TestCallTool_upstreamError_recordsOnceAndMasksBody(t *testing.T) {
	// 模拟 adapter：正文进 Err，对外 Message 为固定短语（与真实 IsError 路径一致）。
	caller := &errCaller{err: errs.Wrap(errs.CodeUpstream, errors.New("secret-echo-body"), "上游工具执行失败")}
	g, store, metrics := seedGatewayFull(t, caller)
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "anon", Operator: true})

	res, err := g.CallTool(ctx, "mock.search", map[string]any{})
	if err != nil {
		t.Fatalf("应返回 isError 结果而非 Go error: %v", err)
	}
	if !res.IsError {
		t.Fatal("上游失败应返回 isError")
	}

	snaps := metrics.SnapshotAll()
	if tool, ok := snapshotOfKey(snaps, "mock.search"); !ok || tool.Totals != 1 || tool.Errors != 1 || tool.Success != 0 {
		t.Fatalf("工具维度应只记 1 次失败，得到 %+v", tool)
	}
	if srv, ok := snapshotOfKey(snaps, observability.ServerDimPrefix+"srv-1"); !ok || srv.Totals != 1 || srv.Errors != 1 {
		t.Fatalf("Server 维度应只记 1 次失败，得到 %+v", srv)
	}
	if inst, ok := snapshotOfKey(snaps, observability.InstanceDimPrefix+"srv-1:inst-1"); !ok || inst.Totals != 1 || inst.Errors != 1 {
		t.Fatalf("实例维度应只记 1 次失败，得到 %+v", inst)
	}

	logs, err := store.RecentTraffic(ctx, 10)
	if err != nil {
		t.Fatalf("RecentTraffic: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("应写 1 条调用日志，得到 %d", len(logs))
	}
	if logs[0].Status != "upstream_error" {
		t.Fatalf("调用日志 status 应为 upstream_error，得到 %q", logs[0].Status)
	}
	if logs[0].InstanceID != "inst-1" {
		t.Fatalf("调用日志应记录命中实例，得到 %q", logs[0].InstanceID)
	}
	if logs[0].Error != "上游工具执行失败" {
		t.Fatalf("调用日志 error 应为脱敏短语，得到 %q", logs[0].Error)
	}
	if strings.Contains(logs[0].Error, "secret-echo-body") {
		t.Fatalf("调用日志 error 不得携带上游回显正文: %q", logs[0].Error)
	}
}

// TestCallTool_success_recordsOnce 验证成功路径工具与 Server 维度各只记一次，
// 且调用日志不写 Error。
func TestCallTool_success_recordsOnce(t *testing.T) {
	g, store, metrics := seedGatewayFull(t, nil)
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "anon", Operator: true})

	if _, err := g.CallTool(ctx, "mock.search", map[string]any{}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	snaps := metrics.SnapshotAll()
	if tool, ok := snapshotOfKey(snaps, "mock.search"); !ok || tool.Totals != 1 || tool.Success != 1 || tool.Errors != 0 {
		t.Fatalf("工具维度应只记 1 次成功，得到 %+v", tool)
	}
	if srv, ok := snapshotOfKey(snaps, observability.ServerDimPrefix+"srv-1"); !ok || srv.Totals != 1 || srv.Success != 1 {
		t.Fatalf("Server 维度应只记 1 次成功，得到 %+v", srv)
	}
	if inst, ok := snapshotOfKey(snaps, observability.InstanceDimPrefix+"srv-1:inst-1"); !ok || inst.Totals != 1 || inst.Success != 1 {
		t.Fatalf("实例维度应只记 1 次成功，得到 %+v", inst)
	}

	logs, err := store.RecentTraffic(ctx, 10)
	if err != nil {
		t.Fatalf("RecentTraffic: %v", err)
	}
	if len(logs) != 1 || logs[0].Status != "success" || logs[0].Error != "" {
		t.Fatalf("成功日志应无 Error 且 status=success，得到 %+v", logs)
	}
	if logs[0].InstanceID != "inst-1" {
		t.Fatalf("调用日志应记录命中实例，得到 %q", logs[0].InstanceID)
	}
}

// TestCallTool_recordArgsGating 验证调用日志入参是否捕获由运行期有效配置决定：
// ctx（/mcp 守卫解析）携带 observability.record_args 时以其为准，未携带时回退
// 静态种子（WithRecordArgsSeed）。这是 record() 落库前的唯一捕获决策点。
func TestCallTool_recordArgsGating(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name       string
		seed       bool
		capture    *bool // nil=ctx 不带运行期配置（回退种子）
		expectArgs bool
	}{
		{name: "runtime_on_captures", seed: false, capture: &on, expectArgs: true},
		{name: "runtime_off_drops", seed: true, capture: &off, expectArgs: false},
		{name: "no_runtime_seed_on", seed: true, capture: nil, expectArgs: true},
		{name: "no_runtime_seed_off", seed: false, capture: nil, expectArgs: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, store, _ := seedGatewayFull(t, nil, WithRecordArgsSeed(tc.seed))
			ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "anon", Operator: true})
			if tc.capture != nil {
				ctx = WithRuntimeConfig(ctx, &model.RuntimeConfig{
					Observability: &model.RuntimeObservability{RecordArgs: *tc.capture},
				})
			}
			if _, err := g.CallTool(ctx, "mock.search", map[string]any{"q": "hello"}); err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			logs, err := store.RecentTraffic(ctx, 10)
			if err != nil || len(logs) != 1 {
				t.Fatalf("应写入 1 条调用日志: len=%d err=%v", len(logs), err)
			}
			if got := logs[0].HasArgs; got != tc.expectArgs {
				t.Fatalf("HasArgs=%v, 期望 %v（捕获决策未按预期生效）", got, tc.expectArgs)
			}
		})
	}
}

// TestCallTool_transportFailureCoolsAndProbes 验证传输/实例级失败触发失败反馈：
// 唯一实例进入短期冷却（第二次调用直接 route_error、不再拨测），并触发快速探活。
func TestCallTool_transportFailureCoolsAndProbes(t *testing.T) {
	probeCalls := 0
	caller := &errCaller{err: errs.New(errs.CodeTimeout, "上游调用超时")}
	g, _, metrics := seedGatewayFull(t, caller, WithInstanceProbe(func(string) { probeCalls++ }))
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "anon", Operator: true})

	first, err := g.CallTool(ctx, "mock.search", map[string]any{})
	if err != nil || !first.IsError {
		t.Fatalf("首次拨测应 isError: err=%v res=%+v", err, first)
	}
	if probeCalls != 1 {
		t.Fatalf("传输失败应触发快速探活，得到 %d", probeCalls)
	}

	// 唯一实例冷却中 → 第二次不再拨测，直接 route_error（无可用上游实例）。
	second, err := g.CallTool(ctx, "mock.search", map[string]any{})
	if err != nil || !second.IsError {
		t.Fatalf("冷却中应 isError: err=%v res=%+v", err, second)
	}
	text := ""
	if len(second.Content) > 0 {
		text = second.Content[0].Text
	}
	if !strings.Contains(text, "无可用") {
		t.Fatalf("冷却中应返回无可用上游实例，得到 %q", text)
	}
	// 冷却期内的第二次调用未拨测：Server 维失败数仍为 1（未被重复计为拨测失败）。
	if srv, ok := snapshotOfKey(metrics.SnapshotAll(), observability.ServerDimPrefix+"srv-1"); !ok || srv.Errors != 1 {
		t.Fatalf("冷却期调用不应记 Server 维拨测失败，得到 %+v", srv)
	}
}

// TestCallTool_toolFailureDoesNotCool 验证 isError 业务失败（上游连通但工具执行
// 失败）不会触发冷却与快速探活——避免把业务失败误判成实例宕机。
func TestCallTool_toolFailureDoesNotCool(t *testing.T) {
	probeCalls := 0
	toolFail := registry.NewToolFailedError(errs.Wrap(errs.CodeUpstream, errors.New("boom"), "上游工具执行失败"))
	g, _, metrics := seedGatewayFull(t, &errCaller{err: toolFail}, WithInstanceProbe(func(string) { probeCalls++ }))
	ctx := WithIdentity(context.Background(), &auth.Identity{Subject: "anon", Operator: true})

	for range 2 {
		res, err := g.CallTool(ctx, "mock.search", map[string]any{})
		if err != nil || !res.IsError {
			t.Fatalf("isError 业务失败应 isError: err=%v res=%+v", err, res)
		}
	}
	if probeCalls != 0 {
		t.Fatalf("业务失败不应触发快速探活，得到 %d", probeCalls)
	}
	// 未冷却 → 两次都真实拨测：Server 维失败数应为 2（业务失败不摘除实例）。
	if srv, ok := snapshotOfKey(metrics.SnapshotAll(), observability.ServerDimPrefix+"srv-1"); !ok || srv.Errors != 2 {
		t.Fatalf("业务失败不应冷却实例，Server 维应记 2 次拨测失败，得到 %+v", srv)
	}
}
