package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/eval"
	"github.com/heyjensenxie/mcp-conductor/internal/mcpclient"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// evalControlUpstream 是评测控制层测试用的最小 MCP 上游。
func evalControlUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		write := func(result string) {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(req.ID), result)))
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			write(`{"protocolVersion":"2025-11-25","serverInfo":{"name":"mock","version":"1.0"},"capabilities":{"tools":{}}}`)
		case "tools/list":
			write(`{"tools":[{"name":"search","description":"查询","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`)
		case "tools/call":
			write(`{"content":[{"type":"text","text":"result ok"}],"isError":false}`)
		default:
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}`, string(req.ID))))
		}
	}))
}

// seedEvalControl 建评测所需的 Control（真实 adapter + memory store + 上游）。
// 返回 control/serverID/健康实例ID/禁用实例ID（供定向评测用例）。
func seedEvalControl(t *testing.T) (*Control, string, string, string) {
	t.Helper()
	upstream := evalControlUpstream(t)
	t.Cleanup(upstream.Close)

	store := memory.New()
	ctx := context.Background()
	now := time.Now().UTC()
	server := &model.Server{Name: "Mock", Enabled: true, HealthStatus: model.ServerStatusHealthy, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	healthy := &model.Instance{
		ServerID: server.ID, Endpoint: upstream.URL, Transport: model.TransportStreamableHTTP,
		Enabled: true, HealthStatus: model.ServerStatusHealthy, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateInstance(ctx, healthy); err != nil {
		t.Fatal(err)
	}
	disabled := &model.Instance{
		ServerID: server.ID, Endpoint: upstream.URL, Transport: model.TransportStreamableHTTP,
		Enabled: false, HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateInstance(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID: server.ID, OriginalName: "search", GatewayName: "mock.search", Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	adapter := mcpclient.New()
	svc := eval.NewService(store, adapter, adapter, func(string) (eval.RuntimeStats, bool) {
		return eval.RuntimeStats{}, false
	})
	ctrl := NewControl(nil, store, nil, nil)
	ctrl.eval = svc
	return ctrl, server.ID, healthy.ID, disabled.ID
}

// TestControlEval_MetaQualitySuite 覆盖 meta / quality / suite 三个端点的 JSON 形状，
// 并覆盖 ?instance_id= 定向（有效 → 命中该实例；陌生 → 404；禁用 → 400）。
func TestControlEval_MetaQualitySuite(t *testing.T) {
	ctrl, id, healthyID, disabledID := seedEvalControl(t)

	// Meta。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/evaluations/servers/"+id, nil)
	req.SetPathValue("id", id)
	ctrl.handleEvalMeta(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("meta 应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var meta struct {
		Server struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"server"`
		Probe struct {
			ID       string `json:"id"`
			Endpoint string `json:"endpoint"`
		} `json:"probe"`
		StoredToolCount int `json:"stored_tool_count"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &meta); err != nil {
		t.Fatalf("解析 meta: %v", err)
	}
	if meta.Server.ID != id || meta.Probe.Endpoint == "" || meta.Probe.ID != healthyID || meta.StoredToolCount != 1 {
		t.Fatalf("meta 异常: %+v", meta)
	}

	// Quality：缺省（首个可拨测=健康实例）。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/evaluations/servers/"+id+"/quality", strings.NewReader(`{}`))
	req.SetPathValue("id", id)
	ctrl.handleEvalQuality(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("quality 应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var report struct {
		OverallScore float64 `json:"overall_score"`
		Dimensions   []struct {
			Key       string `json:"key"`
			Available bool   `json:"available"`
		} `json:"dimensions"`
		ToolChecks []struct {
			Tool string `json:"tool"`
		} `json:"tool_checks"`
		Server struct {
			ProbedInstance *struct {
				ID string `json:"id"`
			} `json:"probed_instance"`
		} `json:"server"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &report); err != nil {
		t.Fatalf("解析 report: %v", err)
	}
	if len(report.Dimensions) != 6 || len(report.ToolChecks) != 1 || report.ToolChecks[0].Tool != "search" {
		t.Fatalf("report 异常: %+v", report)
	}
	if report.Server.ProbedInstance == nil || report.Server.ProbedInstance.ID != healthyID {
		t.Fatalf("缺省 quality 应拨测健康实例 %q，得到 %+v", healthyID, report.Server.ProbedInstance)
	}

	// Quality：定向健康实例 → probed_instance 命中该实例。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/evaluations/servers/"+id+"/quality?instance_id="+healthyID, strings.NewReader(`{}`))
	req.SetPathValue("id", id)
	ctrl.handleEvalQuality(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("定向 quality 应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &report); err != nil {
		t.Fatalf("解析定向 report: %v", err)
	}
	if report.Server.ProbedInstance == nil || report.Server.ProbedInstance.ID != healthyID {
		t.Fatalf("定向 quality 应命中实例 %q，得到 %+v", healthyID, report.Server.ProbedInstance)
	}

	// Quality：定向陌生实例 → 404。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/evaluations/servers/"+id+"/quality?instance_id=ghost", strings.NewReader(`{}`))
	req.SetPathValue("id", id)
	ctrl.handleEvalQuality(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("陌生实例应 404，得到 %d", rec.Code)
	}

	// Quality：定向禁用实例 → 400 invalid_argument。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/evaluations/servers/"+id+"/quality?instance_id="+disabledID, strings.NewReader(`{}`))
	req.SetPathValue("id", id)
	ctrl.handleEvalQuality(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("禁用实例应 400，得到 %d", rec.Code)
	}

	// Suite：命中 + 缺失，并定向实例。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/evaluations/servers/"+id+"/suite?instance_id="+healthyID,
		strings.NewReader(`{"cases":[{"name":"ok","gateway_tool":"mock.search","arguments":{"q":"x"},"expected_substring":"result"}]}`))
	req.SetPathValue("id", id)
	ctrl.handleEvalSuite(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("suite 应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Summary struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &out); err != nil {
		t.Fatalf("解析 suite: %v", err)
	}
	if out.Summary.Total != 1 || out.Summary.Passed != 1 {
		t.Fatalf("suite 汇总异常: %+v", out.Summary)
	}
}

// TestControlEval_errorPaths 覆盖未知 Server / 空 cases / 未装配评测。
func TestControlEval_errorPaths(t *testing.T) {
	ctrl, id, _, _ := seedEvalControl(t)

	// 未知 Server。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/evaluations/servers/ghost", nil)
	req.SetPathValue("id", "ghost")
	ctrl.handleEvalMeta(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知 Server 应 404，得到 %d", rec.Code)
	}

	// 空 cases。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/evaluations/servers/"+id+"/suite", strings.NewReader(`{"cases":[]}`))
	req.SetPathValue("id", id)
	ctrl.handleEvalSuite(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空 cases 应 400，得到 %d", rec.Code)
	}

	// 未装配评测服务。
	plain := NewControl(nil, memory.New(), nil, nil)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/evaluations/servers/"+id, nil)
	req.SetPathValue("id", id)
	plain.handleEvalMeta(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未装配评测应 500，得到 %d", rec.Code)
	}
}
