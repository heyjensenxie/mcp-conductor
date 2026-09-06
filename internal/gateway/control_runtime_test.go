package gateway

import (
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
