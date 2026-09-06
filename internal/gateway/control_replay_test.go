package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeReplay 是 ReplayService 的可注入替身。
type fakeReplay struct {
	res *TrafficReplayResult
	err error
}

func (f *fakeReplay) Replay(_ context.Context, _ int64, _ time.Duration) (*TrafficReplayResult, error) {
	return f.res, f.err
}

// 编译期保证替身跟随接口。
var _ ReplayService = (*fakeReplay)(nil)

// logReq 构造带 {id} 路径参数的直接 handler 请求。
func logReq(t *testing.T, method, path, id string, body string) *http.Request {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.SetPathValue("id", id)
	return req
}

// TestReplayLogUnassembled 验证回放未装配时返回 500。
func TestReplayLogUnassembled(t *testing.T) {
	ctrl := NewControl(nil, memory.New(), observability.NewMetrics(), nil)
	rec := httptest.NewRecorder()
	ctrl.handleReplayLog(rec, logReq(t, http.MethodPost, "/api/logs/1/replay", "1", ""))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未装配应 500，得到 %d", rec.Code)
	}
}

// TestGetLogDetail 验证详情：缺失 404，含入参行带出 request_args。
func TestGetLogDetail(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, observability.NewMetrics(), nil)

	// 缺失 id → 404。
	rec := httptest.NewRecorder()
	ctrl.handleGetLogDetail(rec, logReq(t, http.MethodGet, "/api/logs/999", "999", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺失 id 应 404，得到 %d", rec.Code)
	}

	// 种子一行含入参。
	if err := store.AppendTraffic(t.Context(), model.TrafficSample{
		RequestID: "req-1", ServerID: "srv-1", Tool: "demo", Status: "success",
		LatencyMS: 3, Timestamp: time.Now().UTC(), RequestArgs: map[string]any{"q": "hi"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec = httptest.NewRecorder()
	ctrl.handleGetLogDetail(rec, logReq(t, http.MethodGet, "/api/logs/1", "1", ""))
	code, _, data := rawEnvelope(t, rec)
	if code != http.StatusOK {
		t.Fatalf("详情应成功，得到 %d", code)
	}
	var detail struct {
		RequestArgs map[string]any `json:"request_args"`
		HasArgs     bool           `json:"has_args"`
	}
	if err := json.Unmarshal(data, &detail); err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}
	if detail.RequestArgs["q"] != "hi" || !detail.HasArgs {
		t.Fatalf("详情应带出入参: %+v", detail)
	}
}

// TestReplayLogInjected 验证注入替身时回放成功信封（含 is_error 形态）。
func TestReplayLogInjected(t *testing.T) {
	ctrl := NewControl(nil, memory.New(), observability.NewMetrics(), nil)
	ctrl.replay = &fakeReplay{res: &TrafficReplayResult{
		ServerID: "srv-1", InstanceID: "inst-1", LatencyMS: 12, Content: "pong",
	}}
	rec := httptest.NewRecorder()
	ctrl.handleReplayLog(rec, logReq(t, http.MethodPost, "/api/logs/1/replay", "1", `{"timeout_ms":5000}`))
	code, _, data := rawEnvelope(t, rec)
	if code != http.StatusOK {
		t.Fatalf("回放应 200，得到 %d", code)
	}
	if !strings.Contains(string(data), `"pong"`) {
		t.Fatalf("响应应含内容: %s", data)
	}

	// 结构性失败 → HTTP 映射。
	ctrl.replay = &fakeReplay{err: errs.New(errs.CodeInvalidArgument, "no args")}
	rec = httptest.NewRecorder()
	ctrl.handleReplayLog(rec, logReq(t, http.MethodPost, "/api/logs/1/replay", "1", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无入参应 400，得到 %d", rec.Code)
	}
}
