package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/ratelimit"
)

// ---- 客户端 IP 解析 ----

func TestResolveClientIP(t *testing.T) {
	trusted := trustedPrefixes([]string{"10.0.0.0/8", "127.0.0.1", "192.0.2.10"})

	cases := []struct {
		name, remote, xff, xReal string
		want                     string
	}{
		{"直连非可信无转发头", "203.0.113.5:8080", "", "", "203.0.113.5"},
		{"非可信对端伪造 XFF 被忽略", "203.0.113.5", "6.6.6.6", "", "203.0.113.5"},
		{"可信代理取 XFF 最右非可信", "10.0.0.1:8080", "203.0.113.9, 10.0.0.2", "", "203.0.113.9"},
		{"XFF 全为可信代理回退 X-Real-IP", "10.0.0.1", "10.0.0.2, 10.0.0.3", "198.51.100.7", "198.51.100.7"},
		{"无 XFF 时回退 X-Real-IP", "10.0.0.1", "", "198.51.100.7", "198.51.100.7"},
		{"IPv6 去端口方括号", "[::1]:8080", "", "", "::1"},
		{"IPv4-mapped 归一并信任", "[::ffff:10.0.0.1]:8080", "203.0.113.9", "", "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveClientIP(tc.remote, tc.xff, tc.xReal, trusted)
			if got != tc.want {
				t.Fatalf("resolveClientIP(%q, %q, %q) = %q, 期望 %q", tc.remote, tc.xff, tc.xReal, got, tc.want)
			}
		})
	}
}

func TestCaptureClientIPMiddleware(t *testing.T) {
	var got string

	// capture 中间件应在 ctx 写入解析出的客户端 IP。
	handler := captureClientIPMiddleware([]string{"10.0.0.0/8"})(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { got = ClientIPFrom(r.Context()) },
	))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "10.0.0.2:5678"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if got != "203.0.113.9" {
		t.Fatalf("可信代理后的客户端 IP = %q, 期望 203.0.113.9", got)
	}

	// 直连（非可信）应只认 RemoteAddr，忽略伪造转发头。
	req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req2.RemoteAddr = "203.0.113.9:5678"
	req2.Header.Set("X-Forwarded-For", "6.6.6.6")
	handler.ServeHTTP(httptest.NewRecorder(), req2)
	if got != "203.0.113.9" {
		t.Fatalf("非可信直连应忽略 XFF, 得到 %q", got)
	}
}

// ---- rateLimitMiddleware 三级 / 遗留分支 ----

// recordingLimiter 记录每次 Allow 的 key/limit，可按 key 拒绝（默认全放行）。
type recordingLimiter struct {
	calls []rlCall
	deny  map[string]bool
}

type rlCall struct {
	key   string
	limit ratelimit.Limit
}

func (r *recordingLimiter) Allow(_ context.Context, key string, limit ratelimit.Limit) bool {
	r.calls = append(r.calls, rlCall{key: key, limit: limit})
	return !r.deny[key]
}

func TestRateLimitMiddleware_McpTiers(t *testing.T) {
	lim := &recordingLimiter{deny: map[string]bool{}}
	policy := newRateLimitPolicy(config.RateLimitConfig{
		QPS: 10, Burst: 5, GlobalQPS: 100, GlobalBurst: 50,
	})
	handler := rateLimitMiddleware(lim, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func() int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "203.0.113.9:8080"
		req = req.WithContext(WithIdentity(req.Context(), &auth.Identity{
			Subject: "partner-a",
			Key:     &model.AccessKey{Subject: "partner-a", QPS: 3, Burst: 2},
		}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := call(); code != http.StatusOK {
		t.Fatalf("/mcp 三级放行应 200, 得到 %d", code)
	}
	wantKeys := []string{"mcp:global", "mcp:ip:203.0.113.9", "mcp:key:partner-a"}
	if len(lim.calls) != 3 {
		t.Fatalf("/mcp 应依次 Allow 三级, 得到 %d 次: %+v", len(lim.calls), lim.calls)
	}
	for i, k := range wantKeys {
		if lim.calls[i].key != k {
			t.Fatalf("第 %d 级 key = %q, 期望 %q", i+1, lim.calls[i].key, k)
		}
	}
	// key 级应携带 AccessKey 配额。
	if lim.calls[2].limit.QPS != 3 {
		t.Fatalf("key 级应使用 AccessKey 配额 QPS=3, 得到 %+v", lim.calls[2].limit)
	}
}

// TestRateLimitMiddleware_McpTierRejects429AndStops 验证 IP 级超限以 429 拒绝。
func TestRateLimitMiddleware_McpTierRejects429AndStops(t *testing.T) {
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5, GlobalQPS: 100})
	handler := rateLimitMiddleware(&recordingLimiter{deny: map[string]bool{"mcp:ip:203.0.113.9": true}}, policy)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("IP 级拒绝应 429, 得到 %d", rec.Code)
	}
}

// TestRateLimitMiddleware_GlobalDisabledNoDoubleCount 防回归：global_qps=0 时
// /mcp 不调用全局级（否则 memory 会把 Limit{} 回退成 qps，双重限流）。
func TestRateLimitMiddleware_GlobalDisabledNoDoubleCount(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5}) // global 0
	handler := rateLimitMiddleware(lim, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(lim.calls) != 1 || lim.calls[0].key != "mcp:ip:203.0.113.9" {
		t.Fatalf("全局级关闭时应只 Allow IP 级, 得到 %+v", lim.calls)
	}
}

// TestRateLimitMiddleware_LegacyNonMcp 验证 /api 控制面沿用单维度 subject key。
func TestRateLimitMiddleware_LegacyNonMcp(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	req = req.WithContext(WithIdentity(req.Context(), &auth.Identity{Subject: "operator", Operator: true}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if len(lim.calls) != 1 || lim.calls[0].key != "subject:operator" {
		t.Fatalf("/api 应走遗留 subject 维度, 得到 %+v", lim.calls)
	}
}

// TestRateLimitMiddleware_KeyOwnWindowOverridesDefault 验证：key 配置专属滑动窗口时，
// key 级 Limit.WindowSec 覆盖全局默认（容量=QPS×该 key 窗口）。
func TestRateLimitMiddleware_KeyOwnWindowOverridesDefault(t *testing.T) {
	lim := &recordingLimiter{}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5}) // 静态默认窗口 60
	handler := rateLimitMiddleware(lim, policy)(okNext())

	key := &model.AccessKey{Subject: "partner-a", QPS: 0, WindowSeconds: 5}
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
			if c.limit.QPS != 10 || c.limit.WindowSec != 5 {
				t.Fatalf("key 级应沿用默认 QPS=10 且用该 key 专属窗口=5, 得到 %+v", c.limit)
			}
			return
		}
	}
	t.Fatalf("应产生 mcp:key 调用: %+v", lim.calls)
}
