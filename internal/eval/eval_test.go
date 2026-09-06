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

// seedEvalFixture 建 1 逻辑 Server + 1 健康实例 + 1 网关工具行，返回 store/server/实例。
func seedEvalFixture(t *testing.T, upstreamURL string) (*memory.Store, *model.Server, *model.Instance) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()
	now := time.Now().UTC()
	server := &model.Server{Name: "Mock", Enabled: true, HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	inst := &model.Instance{
		ServerID: server.ID, Endpoint: upstreamURL, Transport: model.TransportStreamableHTTP,
		Enabled: true, HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateInstance(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID: server.ID, OriginalName: "search", GatewayName: "mock.search",
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return store, server, inst
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
	store, server, inst := seedEvalFixture(t, upstream.URL)
	svc := newEvalService(store, upstream)

	meta, err := svc.Describe(context.Background(), server.ID, "")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if meta.Server.Name != "Mock" || meta.Probe == nil || meta.Probe.Endpoint != upstream.URL || meta.Probe.ID != inst.ID {
		t.Fatalf("Describe 异常: %+v", meta)
	}
	if meta.StoredToolCount != 1 || meta.RuntimeMetricsAvailable {
		t.Fatalf("工具数/流量状态异常: %+v", meta)
	}

	// 定向某实例：存在且可拨测 → probe 指向该实例；陌生实例 → 404。
	meta, err = svc.Describe(context.Background(), server.ID, inst.ID)
	if err != nil || meta.Probe == nil || meta.Probe.ID != inst.ID {
		t.Fatalf("定向实例 Describe 异常: meta=%+v err=%v", meta, err)
	}
	if _, err := svc.Describe(context.Background(), server.ID, "ghost"); err == nil {
		t.Fatal("陌生实例应报错")
	}
}

// TestService_RunQuality 验证质量评测输出（现场拨测 + 静态分 + runtime n/a）。
func TestService_RunQuality(t *testing.T) {
	upstream := evalUpstream(t)
	defer upstream.Close()
	store, server, inst := seedEvalFixture(t, upstream.URL)
	svc := newEvalService(store, upstream)

	report, err := svc.RunQuality(context.Background(), server.ID, "")
	if err != nil {
		t.Fatalf("RunQuality: %v", err)
	}
	if report.Server.ID != server.ID || report.Server.ProbedInstance == nil || report.Server.ProbedInstance.ID != inst.ID {
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

	// 定向实例：有效 → ProbedInstance.ID 命中；陌生 → not_found。
	report, err = svc.RunQuality(context.Background(), server.ID, inst.ID)
	if err != nil || report.Server.ProbedInstance.ID != inst.ID {
		t.Fatalf("定向实例质量评测异常: err=%v report=%+v", err, report)
	}
	if _, err := svc.RunQuality(context.Background(), server.ID, "ghost"); err == nil {
		t.Fatal("陌生实例应报错")
	}
}

// TestService_RunSuite 验证回归：命中通过、错误工具 not_found。
func TestService_RunSuite(t *testing.T) {
	upstream := evalUpstream(t)
	defer upstream.Close()
	store, server, inst := seedEvalFixture(t, upstream.URL)
	svc := newEvalService(store, upstream)

	cases := []SuiteCase{
		{Name: "命中", GatewayTool: "mock.search", Arguments: map[string]any{"q": "x"}, ExpectedSubstring: "result"},
		{Name: "缺失", GatewayTool: "mock.none", ExpectedSubstring: "result"},
	}
	out, err := svc.RunSuite(context.Background(), server.ID, "", cases)
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

	// 定向实例执行回归。
	out, err = svc.RunSuite(context.Background(), server.ID, inst.ID, cases)
	if err != nil || out.Summary.Total != 2 {
		t.Fatalf("定向实例回归异常: err=%v out=%+v", err, out)
	}
	if _, err := svc.RunSuite(context.Background(), server.ID, "ghost", cases); err == nil {
		t.Fatal("陌生实例应报错")
	}
}

// TestService_RunQuality_missingServer 覆盖不存在 Server 的错误路径。
func TestService_RunQuality_missingServer(t *testing.T) {
	store := memory.New()
	svc := NewService(store, nil, nil, func(string) (RuntimeStats, bool) { return RuntimeStats{}, false })
	if _, err := svc.RunQuality(context.Background(), "ghost", ""); err == nil {
		t.Fatal("不存在的 Server 应报错")
	}
}

// TestService_TargetsNonCallableInstance 验证定向不可拨测实例（禁用/unhealthy）
// 返回明确的 invalid_argument，而非静默回退首个可拨测实例。
func TestService_TargetsNonCallableInstance(t *testing.T) {
	upstream := evalUpstream(t)
	defer upstream.Close()
	store, server, good := seedEvalFixture(t, upstream.URL)
	ctx := context.Background()
	now := time.Now().UTC()

	disabled := &model.Instance{
		ServerID: server.ID, Endpoint: upstream.URL, Transport: model.TransportStreamableHTTP,
		Enabled: false, HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateInstance(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	unhealthy := &model.Instance{
		ServerID: server.ID, Endpoint: upstream.URL, Transport: model.TransportStreamableHTTP,
		Enabled: true, HealthStatus: model.ServerStatusUnhealthy, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateInstance(ctx, unhealthy); err != nil {
		t.Fatal(err)
	}

	svc := newEvalService(store, upstream)
	for _, bad := range []*model.Instance{disabled, unhealthy} {
		if _, err := svc.RunQuality(ctx, server.ID, bad.ID); err == nil {
			t.Fatalf("定向实例 %q 应因不可拨测报错", bad.ID)
		}
		if _, err := svc.RunSuite(ctx, server.ID, bad.ID, []SuiteCase{{Name: "x", GatewayTool: "mock.search"}}); err == nil {
			t.Fatalf("定向实例 %q 的回归应报错", bad.ID)
		}
	}

	// 多实例 Server 上默认仍取首个可拨测实例（good），不受禁用/坏实例干扰。
	report, err := svc.RunQuality(ctx, server.ID, "")
	if err != nil || report.Server.ProbedInstance == nil || report.Server.ProbedInstance.ID != good.ID {
		t.Fatalf("默认应落在可拨测实例 %q: err=%v report=%+v", good.ID, err, report)
	}
}
