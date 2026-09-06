package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

func okNext() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func mcpReqWithRuntime(remote, subject string, key *model.AccessKey, ab model.RuntimeAutoBan) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = remote
	ident := &auth.Identity{Subject: subject, Operator: key == nil && subject == "operator"}
	if key != nil {
		ident.Key = key
	}
	ctx := WithIdentity(req.Context(), ident)
	// 运行期配置需带可用的速率（否则会覆盖静态策略为不限流）；auto_ban 控制计数。
	ctx = WithRuntimeConfig(ctx, &model.RuntimeConfig{
		RateLimit: model.RuntimeRateLimit{QPS: 10, WindowSeconds: 60},
		AutoBan:   ab,
	})
	return req.WithContext(ctx)
}

// TestAutoBan_IPBannedAfterRateLimitRejections 验证：/mcp 被 429 拒绝计数，窗口内达
// 阈值即临时封禁该 IP。
func TestAutoBan_IPBannedAfterRateLimitRejections(t *testing.T) {
	manager := newAutoBanManager()
	lim := &recordingLimiter{deny: map[string]bool{"mcp:ip:203.0.113.9": true}}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy, manager)(okNext())

	ab := model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 2, BanSeconds: 300}
	call := func() int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, mcpReqWithRuntime("203.0.113.9:8080", "anonymous", nil, ab))
		return rec.Code
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("第 1 次应 429, 得到 %d", code)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("第 2 次应 429, 得到 %d", code)
	}
	if !manager.isIPBanned("203.0.113.9") {
		t.Fatal("窗口内达阈值应触发 IP 临时封禁")
	}
}

// TestAutoBan_GuardRejectsBannedIP 验证：被封 IP 在运行时守卫层 403 拒绝。
func TestAutoBan_GuardRejectsBannedIP(t *testing.T) {
	store := memory.New()
	cache := newRuntimeConfigCache(store, &model.RuntimeConfig{})
	manager := newAutoBanManager()
	handler := runtimeGuardMiddleware(cache, manager)(okNext())

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "10.0.0.1:8080"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("未封禁应放行, 得到 %d", rec.Code)
	}

	manager.banIP("10.0.0.1", time.Minute)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("封禁中应 403, 得到 %d", rec2.Code)
	}
}

// TestAutoBan_SuspendedKeyRejected403 验证：被临时停用的 key 在限流前直接 403。
func TestAutoBan_SuspendedKeyRejected403(t *testing.T) {
	manager := newAutoBanManager()
	manager.suspendKey("partner-a", time.Minute)
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(&recordingLimiter{}, policy, manager)(okNext())

	key := &model.AccessKey{Subject: "partner-a", QPS: 0}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, mcpReqWithRuntime("203.0.113.9:8080", "partner-a", key, model.RuntimeAutoBan{}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("停用 key 应 403, 得到 %d", rec.Code)
	}
}

// TestAutoBan_KeySuspendedAfterRateLimitRejections 验证：key 被限流达阈值即停用。
func TestAutoBan_KeySuspendedAfterRateLimitRejections(t *testing.T) {
	manager := newAutoBanManager()
	lim := &recordingLimiter{deny: map[string]bool{"mcp:key:partner-a": true}}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy, manager)(okNext())

	ab := model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 2, BanSeconds: 300}
	key := &model.AccessKey{Subject: "partner-a", QPS: 0}
	call := func() int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, mcpReqWithRuntime("203.0.113.9:8080", "partner-a", key, ab))
		return rec.Code
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("第 1 次应 429, 得到 %d", code)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("第 2 次应 429, 得到 %d", code)
	}
	if !manager.isKeySuspended("partner-a") {
		t.Fatal("key 达阈值应被临时停用")
	}
}

// TestAutoBan_OperatorNotCounted 验证：operator 触发的 429 不计 IP，避免封管理出口。
func TestAutoBan_OperatorNotCounted(t *testing.T) {
	manager := newAutoBanManager()
	lim := &recordingLimiter{deny: map[string]bool{"mcp:ip:203.0.113.9": true}}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy, manager)(okNext())

	ab := model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 2, BanSeconds: 300}
	req := mcpReqWithRuntime("203.0.113.9:8080", "operator", nil, ab)
	req = req.WithContext(WithIdentity(req.Context(), &auth.Identity{Subject: "operator", Operator: true}))
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("第 %d 次应 429, 得到 %d", i+1, rec.Code)
		}
	}
	if manager.isIPBanned("203.0.113.9") {
		t.Fatal("operator 触发的 429 不应计入 IP 自动封禁")
	}
}

// TestAutoBan_WhitelistedIPNotCounted 验证：白名单来源即使因全局闸被 429 拒绝，
// 也不计 IP 违规、不会触发 IP 自动封禁（可信豁免）。
func TestAutoBan_WhitelistedIPNotCounted(t *testing.T) {
	manager := newAutoBanManager()
	lim := &recordingLimiter{deny: map[string]bool{"mcp:global": true}}
	policy := newRateLimitPolicy(config.RateLimitConfig{QPS: 10, Burst: 5})
	handler := rateLimitMiddleware(lim, policy, manager)(okNext())

	ab := model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 2, BanSeconds: 300}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "203.0.113.9:8080"
	ctx := WithIdentity(req.Context(), &auth.Identity{Subject: "anonymous"})
	req = req.WithContext(WithRuntimeConfig(ctx, &model.RuntimeConfig{
		RateLimit:   model.RuntimeRateLimit{GlobalQPS: 5, WindowSeconds: 60},
		AutoBan:     ab,
		IPWhitelist: []string{"203.0.113.9"},
	}))
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("第 %d 次应 429（全局闸）, 得到 %d", i+1, rec.Code)
		}
	}
	if manager.isIPBanned("203.0.113.9") {
		t.Fatal("白名单来源不应被 IP 自动封禁")
	}
}
