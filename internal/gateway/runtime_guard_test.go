package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// TestRuntimeGuard_BlocksMcpIPsOnly 验证：/mcp 命中封禁 IP/CIDR → 403；其余 IP 放行
// 且 ctx 携带生效配置；非 /mcp 请求不受守卫影响。
func TestRuntimeGuard_BlocksMcpIPsOnly(t *testing.T) {
	fallback := &model.RuntimeConfig{IPBlocklist: []string{"203.0.113.9", "198.51.100.0/24"}}
	cache := newRuntimeConfigCache(memory.New(), fallback)

	var servedCfg *model.RuntimeConfig
	handler := runtimeGuardMiddleware(cache)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servedCfg = RuntimeConfigFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	call := func(remote, path string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for _, remote := range []string{"203.0.113.9:8080", "198.51.100.7:8080"} {
		if code := call(remote, "/mcp"); code != http.StatusForbidden {
			t.Fatalf("封禁 IP %s 应 403 拒绝, 得到 %d", remote, code)
		}
	}
	if code := call("10.0.0.1:8080", "/mcp"); code != http.StatusOK {
		t.Fatalf("非封禁 IP 应放行, 得到 %d", code)
	}
	if servedCfg == nil || len(servedCfg.IPBlocklist) != 2 {
		t.Fatalf("放行请求 ctx 应携带生效运行期配置: %+v", servedCfg)
	}
	// 黑名单只作用于 /mcp：/api 即使来自封禁 IP 也不拦。
	if code := call("203.0.113.9:8080", "/api/logs"); code != http.StatusOK {
		t.Fatalf("/api 不受 IP 封禁影响, 得到 %d", code)
	}
}

// TestRuntimeGuard_StoredOverridesFallback 验证：有存值时以存值为准（替换种子）。
func TestRuntimeGuard_StoredOverridesFallback(t *testing.T) {
	store := memory.New()
	fallback := &model.RuntimeConfig{IPBlocklist: []string{"10.0.0.0/8"}}
	if err := store.PutRuntimeConfig(context.Background(), &model.RuntimeConfig{
		IPBlocklist: []string{"198.51.100.0/24"},
	}); err != nil {
		t.Fatalf("PutRuntimeConfig: %v", err)
	}
	cache := newRuntimeConfigCache(store, fallback)

	handler := runtimeGuardMiddleware(cache)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "10.0.0.5:8080"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("种子中的 10.0.0.0/8 已被存值替换, 不应封禁, 得到 %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req2.RemoteAddr = "198.51.100.5:8080"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("存值中的 198.51.100.0/24 应 403 拒绝, 得到 %d", rec2.Code)
	}
}

// TestRateLimitMiddleware_UsesRuntimeCfgFromCtx 验证：ctx 带运行期配置时 /mcp 限流
// 采用其阈值（global_qps=500 应触发全局级 Allow；静态 policy 的 global 为 0）。
func TestRateLimitMiddleware_UsesRuntimeCfgFromCtx(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5}) // global 0
	handler := rateLimitMiddleware(lim, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	req = req.WithContext(WithRuntimeConfig(req.Context(), &model.RuntimeConfig{
		RateLimit: model.RuntimeRateLimit{GlobalQPS: 500, GlobalBurst: 50},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应放行, 得到 %d", rec.Code)
	}
	if len(lim.calls) != 1 || lim.calls[0].key != "mcp:global" || lim.calls[0].limit.QPS != 500 {
		t.Fatalf("运行期配额应覆盖静态策略（仅全局级 500）: %+v", lim.calls)
	}
}

// TestRateLimitMiddleware_RuntimeWindowFlowsToLimits 验证运行期 window_seconds 进入
// 各 tier 的 Limit.WindowSec（内存/Redis 据此算容量 QPS×窗长）。
func TestRateLimitMiddleware_RuntimeWindowFlowsToLimits(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5}) // 静态窗口 1
	handler := rateLimitMiddleware(lim, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	req = req.WithContext(WithRuntimeConfig(req.Context(), &model.RuntimeConfig{
		RateLimit: model.RuntimeRateLimit{QPS: 1, GlobalQPS: 5, WindowSeconds: 3},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应放行, 得到 %d", rec.Code)
	}
	if len(lim.calls) != 2 { // global + ip
		t.Fatalf("应有 global+ip 两级 Allow: %+v", lim.calls)
	}
	for _, c := range lim.calls {
		if c.limit.WindowSec != 3 {
			t.Fatalf("运行期 window_seconds 应进入各 tier Limit, 得到 %+v", c.limit)
		}
	}
}

// TestRateLimitMiddleware_KeyNoCapSkipsKeyTier 验证：key.qps=-1（不设 key 上限）时
// 跳过 key 级 Allow（仍受 IP/全局闸约束），不落 key 计数。
func TestRateLimitMiddleware_KeyNoCapSkipsKeyTier(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy)(okNext())

	key := &model.AccessKey{Subject: "partner-a", QPS: -1}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	req = req.WithContext(WithIdentity(req.Context(), &auth.Identity{Subject: "partner-a", Key: key}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应放行, 得到 %d", rec.Code)
	}
	for _, c := range lim.calls {
		if c.key == "mcp:key:partner-a" {
			t.Fatalf("-1 应跳过 key 级 Allow: %+v", c)
		}
	}
}

// TestRuntimeGuard_WhitelistExemptsBlocklist 验证：命中白名单的来源即使也在黑名单
// 里，仍被可信豁免放行（403 逻辑不生效）。
func TestRuntimeGuard_WhitelistExemptsBlocklist(t *testing.T) {
	fallback := &model.RuntimeConfig{
		IPBlocklist: []string{"203.0.113.9"},
		IPWhitelist: []string{"203.0.113.9"},
	}
	cache := newRuntimeConfigCache(memory.New(), fallback)
	handler := runtimeGuardMiddleware(cache)(okNext())

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("白名单来源应豁免黑名单, 得到 %d", rec.Code)
	}
}

// TestRateLimitMiddleware_WhitelistedIPSkipsIPTier 验证：白名单来源跳过单 IP 级限流
// （不产生 mcp:ip Allow），但请求仍正常放行。
func TestRateLimitMiddleware_WhitelistedIPSkipsIPTier(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy)(okNext())

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	rc := &model.RuntimeConfig{
		RateLimit:   model.RuntimeRateLimit{QPS: 10, WindowSeconds: 60},
		IPWhitelist: []string{"203.0.113.9"},
	}
	req = req.WithContext(WithRuntimeConfig(req.Context(), rc))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应放行, 得到 %d", rec.Code)
	}
	for _, c := range lim.calls {
		if c.key == "mcp:ip:203.0.113.9" {
			t.Fatalf("白名单来源不应进入单 IP 层: %+v", c)
		}
	}
}
