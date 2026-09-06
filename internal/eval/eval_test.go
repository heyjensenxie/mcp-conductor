package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/mcpclient"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// evalUpstream 是评测用的最小 MCP 上游：initialize / tools/list / tools/call。
func evalUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeResult := func(result string) {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(req.ID), result)))
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			writeResult(`{"protocolVersion":"2025-11-25","serverInfo":{"name":"mock","version":"1.0"},"capabilities":{"tools":{"listChanged":false}}}`)
		case "tools/list":
			writeResult(`{"tools":[{"name":"search","description":"查询","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`)
		case "tools/call":
			writeResult(`{"content":[{"type":"text","text":"hello result for search"}],"isError":false}`)
		default:
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}`, string(req.ID))))
		}
	}))
}

// seedEvalFixture 建 1 逻辑 Server + 1 健康实例 + 1 网关工具行，返回 store 与 server。
func seedEvalFixture(t *testing.T, upstreamURL string) (*memory.Store, *model.Server) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()
	now := time.Now().UTC()
	server := &model.Server{Name: "Mock", Enabled: true, HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateInstance(ctx, &model.Instance{
		ServerID: server.ID, Endpoint: upstreamURL, Transport: model.TransportStreamableHTTP,
		Enabled: true, HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID: server.ID, OriginalName: "search", GatewayName: "mock.search",
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return store, server
}

func newEvalService(store *memory.Store, upstream *httptest.Server) *Service {
	adapter := mcpclient.New()
	return NewService(store, adapter, adapter, func(string) (RuntimeStats, bool) {
		return RuntimeStats{}, false
	})
}

// TestService_Describe 验证概况（拨测实例、工具数、流量/覆盖状态）。
func TestService_Describe(t *testing.T) {
	upstream := evalUpstream(t)
	defer upstream.Close()
	store, server := seedEvalFixture(t, upstream.URL)
	svc := newEvalService(store, upstream)

	meta, err := svc.Describe(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if meta.Server.Name != "Mock" || meta.Probe == nil || meta.Probe.Endpoint != upstream.URL {
		t.Fatalf("Describe 异常: %+v", meta)
	}
	if meta.StoredToolCount != 1 || meta.RuntimeMetricsAvailable {
		t.Fatalf("工具数/流量状态异常: %+v", meta)
	}
}

// TestService_RunQuality 验证质量评测输出（现场拨测 + 静态分 + runtime n/a）。
func TestService_RunQuality(t *testing.T) {
	upstream := evalUpstream(t)
	defer upstream.Close()
	store, server := seedEvalFixture(t, upstream.URL)
	svc := newEvalService(store, upstream)

	report, err := svc.RunQuality(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("RunQuality: %v", err)
	}
	if report.Server.ID != server.ID || report.Server.ProbedInstance == nil {
		t.Fatalf("报告 Server 段异常: %+v", report.Server)
	}
	if report.OverallScore <= 0 || len(report.Dimensions) != 6 {
		t.Fatalf("报告维度异常: overall=%v dims=%d", report.OverallScore, len(report.Dimensions))
	}
	if len(report.ToolChecks) != 1 || report.ToolChecks[0].Tool != "search" {
		t.Fatalf("应检查 1 个上游工具: %+v", report.ToolChecks)
	}
	if report.Runtime == nil || report.Runtime.Available {
		t.Fatalf("无流量时 runtime 应为 n/a: %+v", report.Runtime)
	}
	if len(report.Dimensions) != 6 {
		t.Fatalf("应始终输出 6 个维度（含 n/a）")
	}
}

// TestService_RunSuite 验证回归：命中通过、错误工具 not_found。
func TestService_RunSuite(t *testing.T) {
	upstream := evalUpstream(t)
	defer upstream.Close()
	store, server := seedEvalFixture(t, upstream.URL)
	svc := newEvalService(store, upstream)

	cases := []SuiteCase{
		{Name: "命中", GatewayTool: "mock.search", Arguments: map[string]any{"q": "x"}, ExpectedSubstring: "result"},
		{Name: "缺失", GatewayTool: "mock.none", ExpectedSubstring: "result"},
	}
	out, err := svc.RunSuite(context.Background(), server.ID, cases)
	if err != nil {
		t.Fatalf("RunSuite: %v", err)
	}
	if out.Summary.Total != 2 || out.Summary.Passed != 1 || out.Summary.Failed != 1 {
		t.Fatalf("汇总异常: %+v", out.Summary)
	}
	if out.Cases[0].OriginalTool != "search" || !out.Cases[0].Passed {
		t.Fatalf("命中 case 应通过并回填原名: %+v", out.Cases[0])
	}
	if out.Cases[1].ErrorCode != "not_found" {
		t.Fatalf("缺失工具应 not_found: %+v", out.Cases[1])
	}
}

// TestService_RunQuality_missingServer 覆盖不存在 Server 的错误路径。
func TestService_RunQuality_missingServer(t *testing.T) {
	store := memory.New()
	svc := NewService(store, nil, nil, func(string) (RuntimeStats, bool) { return RuntimeStats{}, false })
	if _, err := svc.RunQuality(context.Background(), "ghost"); err == nil {
		t.Fatal("不存在的 Server 应报错")
	}
}
