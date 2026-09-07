package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// newRuntimeTestControl 构造带运行期缓存的 Control（fallback 含一条封禁种子）。
func newRuntimeTestControl(t *testing.T) (*Control, *memory.Store) {
	t.Helper()
	store := memory.New()
	fallback := &model.RuntimeConfig{IPBlocklist: []string{"203.0.113.9"}}
	cache := newRuntimeConfigCache(store, fallback)
	ctrl := NewControl(nil, store, nil, nil)
	ctrl.runtimeCache = cache
	ctrl.rateLimitEnabled = true
	return ctrl, store
}

func decodeRuntimeView(t *testing.T, rec *httptest.ResponseRecorder) runtimeConfigView {
	t.Helper()
	var out struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Data    runtimeConfigView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解码响应失败: %v (%s)", err, rec.Body.String())
	}
	return out.Data
}

func TestRuntimeConfig_GetUsesFallbackWhenUnpersisted(t *testing.T) {
	ctrl, _ := newRuntimeTestControl(t)
	rec := httptest.NewRecorder()
	ctrl.handleGetRuntimeConfig(rec, httptest.NewRequest(http.MethodGet, "/api/runtime-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 应 200, 得到 %d", rec.Code)
	}
	view := decodeRuntimeView(t, rec)
	if view.Persisted {
		t.Fatal("未保存时应 persisted=false")
	}
	if len(view.Config.IPBlocklist) != 1 || view.Config.IPBlocklist[0] != "203.0.113.9" {
		t.Fatalf("应返回回退种子 blocklist: %v", view.Config.IPBlocklist)
	}
	if !view.RateLimitEnabled {
		t.Fatal("rate_limit_enabled 应为 true（测试注入）")
	}
}

func TestRuntimeConfig_PutThenGetPersists(t *testing.T) {
	ctrl, _ := newRuntimeTestControl(t)

	body := `{"ratelimit":{"qps":12,"burst":6,"window_seconds":5,"ip_qps":0,"ip_burst":0,"global_qps":500,"global_burst":50},"auto_ban":{"enabled":true,"window_seconds":30,"max_violations":3,"ban_seconds":120},"ip_blocklist":["10.0.0.0/8","198.51.100.7"],"ip_whitelist":["127.0.0.1","10.0.0.0/8"]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/runtime-config", strings.NewReader(body))
	ctrl.handlePutRuntimeConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 应 200, 得到 %d: %s", rec.Code, rec.Body.String())
	}
	view := decodeRuntimeView(t, rec)
	if !view.Persisted || view.Config.RateLimit.QPS != 12 || view.Config.RateLimit.GlobalQPS != 500 ||
		view.Config.RateLimit.WindowSeconds != 5 || !view.Config.AutoBan.Enabled ||
		view.Config.AutoBan.MaxViolations != 3 {
		t.Fatalf("PUT 返回视图异常: %+v", view)
	}

	rec2 := httptest.NewRecorder()
	ctrl.handleGetRuntimeConfig(rec2, httptest.NewRequest(http.MethodGet, "/api/runtime-config", nil))
	got := decodeRuntimeView(t, rec2)
	if !got.Persisted || got.Config.RateLimit.QPS != 12 || got.Config.RateLimit.WindowSeconds != 5 ||
		!got.Config.AutoBan.Enabled || got.Config.AutoBan.BanSeconds != 120 ||
		len(got.Config.IPBlocklist) != 2 || len(got.Config.IPWhitelist) != 2 {
		t.Fatalf("PUT 后 GET 应以存值为准: %+v", got)
	}
}

// TestRuntimeConfig_WindowDefaultsToMinute 验证旧 PUT（未携带 window_seconds）被
// 归一为默认 60（1 分钟）。
func TestRuntimeConfig_WindowDefaultsToMinute(t *testing.T) {
	ctrl, _ := newRuntimeTestControl(t)
	body := `{"ratelimit":{"qps":3,"burst":1},"ip_blocklist":[]}`
	rec := httptest.NewRecorder()
	ctrl.handlePutRuntimeConfig(rec, httptest.NewRequest(http.MethodPut, "/api/runtime-config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 应 200: %s", rec.Body.String())
	}
	if view := decodeRuntimeView(t, rec); view.Config.RateLimit.WindowSeconds != 60 {
		t.Fatalf("未携带 window_seconds 应归一为 60（1 分钟）, 得到 %d", view.Config.RateLimit.WindowSeconds)
	}
}

func TestRuntimeConfig_PutRejectsInvalid(t *testing.T) {
	ctrl, _ := newRuntimeTestControl(t)

	cases := []string{
		`{"ratelimit":{"qps":-1,"burst":6},"ip_blocklist":[]}`,           // 负配额
		`{"ratelimit":{"qps":1,"burst":1},"ip_blocklist":["bogus"]}`,     // 非法 blocklist
		`{"ratelimit":{"qps":1,"burst":1},"ip_whitelist":["not-an-ip"]}`, // 非法 whitelist
	}
	for _, body := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/runtime-config", strings.NewReader(body))
		ctrl.handlePutRuntimeConfig(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("非法 PUT 应 400, 得到 %d (%s)", rec.Code, body)
		}
	}
}

// newRuntimeControlWithSeed 构造带观测种子的 Control（observability.record_args=true），
// 供种子回退与旧客户端合并语义测试。
func newRuntimeControlWithSeed(t *testing.T) *Control {
	t.Helper()
	store := memory.New()
	cache := newRuntimeConfigCache(store, &model.RuntimeConfig{
		IPBlocklist:   []string{"203.0.113.9"},
		Observability: &model.RuntimeObservability{RecordArgs: true},
	})
	ctrl := NewControl(nil, store, nil, nil)
	ctrl.runtimeCache = cache
	ctrl.rateLimitEnabled = true
	return ctrl
}

// TestRuntimeConfig_GetObservabilityFromSeed 验证无保存值时 GET 返回种子观测设置。
func TestRuntimeConfig_GetObservabilityFromSeed(t *testing.T) {
	ctrl := newRuntimeControlWithSeed(t)
	rec := httptest.NewRecorder()
	ctrl.handleGetRuntimeConfig(rec, httptest.NewRequest(http.MethodGet, "/api/runtime-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 应 200, 得到 %d", rec.Code)
	}
	view := decodeRuntimeView(t, rec)
	if view.Persisted {
		t.Fatal("未保存时应 persisted=false")
	}
	if view.Config.Observability == nil || !view.Config.Observability.RecordArgs {
		t.Fatalf("应回退种子 record_args=true, 得到 %+v", view.Config.Observability)
	}
}

// TestRuntimeConfig_PutObservabilityRoundTrip 验证 PUT 显式携带 observability 持久化，
// 且再次显式 false 可关闭。
func TestRuntimeConfig_PutObservabilityRoundTrip(t *testing.T) {
	ctrl, _ := newRuntimeTestControl(t)

	rec := httptest.NewRecorder()
	ctrl.handlePutRuntimeConfig(rec, httptest.NewRequest(http.MethodPut, "/api/runtime-config", strings.NewReader(
		`{"ratelimit":{"qps":5,"burst":1,"window_seconds":60},"auto_ban":{},"ip_blocklist":[],"ip_whitelist":[],"observability":{"record_args":true}}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 应 200: %s", rec.Body.String())
	}
	if view := decodeRuntimeView(t, rec); view.Config.Observability == nil || !view.Config.Observability.RecordArgs {
		t.Fatalf("PUT 应返回持久化 record_args=true: %+v", view.Config.Observability)
	}

	rec2 := httptest.NewRecorder()
	ctrl.handleGetRuntimeConfig(rec2, httptest.NewRequest(http.MethodGet, "/api/runtime-config", nil))
	got := decodeRuntimeView(t, rec2)
	if got.Config.Observability == nil || !got.Config.Observability.RecordArgs {
		t.Fatalf("PUT 后 GET 应保留 record_args=true: %+v", got.Config.Observability)
	}

	// 显式关闭（新客户端携带 observability 才覆盖）。
	rec3 := httptest.NewRecorder()
	ctrl.handlePutRuntimeConfig(rec3, httptest.NewRequest(http.MethodPut, "/api/runtime-config", strings.NewReader(
		`{"ratelimit":{"qps":5,"burst":1,"window_seconds":60},"auto_ban":{},"ip_blocklist":[],"ip_whitelist":[],"observability":{"record_args":false}}`)))
	if view := decodeRuntimeView(t, rec3); view.Config.Observability == nil || view.Config.Observability.RecordArgs {
		t.Fatalf("显式 false 应生效: %+v", view.Config.Observability)
	}
}

// TestRuntimeConfig_PutWithoutObservabilityKeepsCurrent 验证旧客户端（防护页）PUT
// 不携带 observability 时沿用当前有效值，避免把入参捕获静默关掉。
func TestRuntimeConfig_PutWithoutObservabilityKeepsCurrent(t *testing.T) {
	ctrl := newRuntimeControlWithSeed(t)

	rec := httptest.NewRecorder()
	ctrl.handlePutRuntimeConfig(rec, httptest.NewRequest(http.MethodPut, "/api/runtime-config", strings.NewReader(
		`{"ratelimit":{"qps":5,"burst":1,"window_seconds":60},"auto_ban":{},"ip_blocklist":[],"ip_whitelist":[]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 应 200: %s", rec.Body.String())
	}
	if view := decodeRuntimeView(t, rec); view.Config.Observability == nil || !view.Config.Observability.RecordArgs {
		t.Fatalf("未携带 observability 应沿用当前 record_args=true: %+v", view.Config.Observability)
	}
}

func decodePurged(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var out struct {
		Data purgeArgsView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解码响应失败: %v (%s)", err, rec.Body.String())
	}
	return out.Data.Purged
}

// TestPurgeTrafficArgsHandler 验证清除入参端点：清掉已捕获行数，行与元数据保留。
func TestPurgeTrafficArgsHandler(t *testing.T) {
	ctrl, store := newRuntimeTestControl(t)
	ctx := context.Background()
	now := model.Now()
	for i, withArgs := range []bool{true, false} {
		sample := model.TrafficSample{
			RequestID:  "req-" + string(rune('a'+i)),
			ServerID:   "srv-1",
			Tool:       "mock.search",
			Status:     "success",
			LatencyMS:  3,
			Timestamp:  now,
			RequestArgs: map[string]any{"q": "hello"},
		}
		if !withArgs {
			sample.RequestArgs = nil
		}
		if err := store.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	ctrl.handlePurgeTrafficArgs(rec, httptest.NewRequest(http.MethodPost, "/api/logs/purge-args", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST 应 200, 得到 %d: %s", rec.Code, rec.Body.String())
	}
	if n := decodePurged(t, rec); n != 1 {
		t.Fatalf("应清除 1 行, 得到 %d", n)
	}

	// 再次调用幂等：无入参可清，返回 0。
	rec2 := httptest.NewRecorder()
	ctrl.handlePurgeTrafficArgs(rec2, httptest.NewRequest(http.MethodPost, "/api/logs/purge-args", nil))
	if n := decodePurged(t, rec2); n != 0 {
		t.Fatalf("重复清除应返回 0, 得到 %d", n)
	}
}
