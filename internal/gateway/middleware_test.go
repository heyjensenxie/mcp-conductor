package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeAuth 记录 Authenticate 是否被调用，放行非空凭据。
type fakeAuth struct {
	called int
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (*auth.Identity, error) {
	f.called++
	if token == "" {
		return nil, errs.New(errs.CodeAuthentication, "缺少凭据")
	}
	return &auth.Identity{Subject: "any"}, nil
}

func TestAuthMiddleware_ExemptsSPAButProtectsAPI(tt *testing.T) {
	fake := &fakeAuth{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := authMiddleware(fake)(next)

	call := func(method, path, token string) int {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("X-Api-Key", token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Console SPA 与静态资源免认证加载（登录前录入凭据的前提）。
	before := fake.called
	if code := call(http.MethodGet, "/", ""); code != http.StatusOK {
		tt.Fatalf("GET / 应免认证放行, 得到 %d", code)
	}
	if code := call(http.MethodGet, "/assets/index-abc.js", ""); code != http.StatusOK {
		tt.Fatalf("GET /assets 应免认证放行, 得到 %d", code)
	}
	if fake.called != before {
		tt.Fatal("免认证路径不应触发 Authenticate")
	}

	// 数据面与控制面必须鉴权。
	if code := call(http.MethodGet, "/api/servers", ""); code != http.StatusUnauthorized {
		tt.Fatalf("无凭据 /api 应 401, 得到 %d", code)
	}
	if code := call(http.MethodPost, "/mcp", "secret"); code != http.StatusOK {
		tt.Fatalf("带凭据 /mcp 应放行, 得到 %d", code)
	}

	// 登录端点本就不经认证（供前端登录）。
	before = fake.called
	if code := call(http.MethodPost, "/api/auth/login", ""); code != http.StatusOK {
		tt.Fatalf("/api/auth/login 应免认证, 得到 %d", code)
	}
	if fake.called != before {
		tt.Fatal("/api/auth/* 不应触发 Authenticate")
	}
}

// roleAuth 按令牌返回不同身份：op → Operator；key → 数据面 API Key。
type roleAuth struct{}

func (roleAuth) Authenticate(_ context.Context, token string) (*auth.Identity, error) {
	switch token {
	case "op":
		return &auth.Identity{Subject: "operator", Operator: true}, nil
	case "key":
		return &auth.Identity{Subject: "partner-a", Key: &model.AccessKey{Subject: "partner-a"}}, nil
	}
	return nil, errs.New(errs.CodeAuthentication, "缺少凭据")
}

func TestAuthMiddleware_OperatorGateOnControlPlane(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := authMiddleware(roleAuth{})(next)

	call := func(method, path, token string) int {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("X-Api-Key", token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// /api 控制面只放行 Operator 身份。
	if code := call(http.MethodGet, "/api/servers", "op"); code != http.StatusOK {
		t.Fatalf("Operator 访问 /api 应放行, 得到 %d", code)
	}
	if code := call(http.MethodGet, "/api/servers", "key"); code != http.StatusForbidden {
		t.Fatalf("数据面 API Key 访问 /api 应 403, 得到 %d", code)
	}
	if code := call(http.MethodGet, "/api/servers", ""); code != http.StatusUnauthorized {
		t.Fatalf("无凭据 /api 应 401, 得到 %d", code)
	}
	// 创建/管理类端点同样被拦截。
	if code := call(http.MethodPost, "/api/keys", "key"); code != http.StatusForbidden {
		t.Fatalf("API Key POST /api/keys 应 403, 得到 %d", code)
	}

	// /mcp 数据面：Operator 与数据面 API Key 均可入（授权交给 access 白名单）。
	if code := call(http.MethodPost, "/mcp", "key"); code != http.StatusOK {
		t.Fatalf("API Key 访问 /mcp 应放行, 得到 %d", code)
	}
	if code := call(http.MethodPost, "/mcp", "op"); code != http.StatusOK {
		t.Fatalf("Operator 访问 /mcp 应放行, 得到 %d", code)
	}
}

// failAuth 固定返回认证失败（模拟刷无效凭据 / 未带凭据）。
type failAuth struct{}

func (failAuth) Authenticate(_ context.Context, _ string) (*auth.Identity, error) {
	return nil, errs.New(errs.CodeAuthentication, "无效的凭据")
}

// authBanChain 组装「运行期守卫 → 认证失败守卫 → 恒失败认证」最小链路，
// 观察 /mcp 认证失败是否按运行期 auto_ban 计 IP 违规并封禁来源 IP。
func authBanChain(t *testing.T, ban *autoBanManager, rc *model.RuntimeConfig) http.Handler {
	t.Helper()
	store := memory.New()
	cache := newRuntimeConfigCache(store, rc)
	return chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
		runtimeGuardMiddleware(cache, ban),
		authFailGuardMiddleware(ban),
		authMiddleware(failAuth{}),
	)
}

// TestAuthFailGuard_BansIPAfterRepeatedFailures 验证 /mcp 凭证失败达阈值即临时封禁
// 来源 IP；封禁期由运行期守卫对同 IP 统一 403（不再逐次计费）。
func TestAuthFailGuard_BansIPAfterRepeatedFailures(t *testing.T) {
	ban := newAutoBanManager()
	h := authBanChain(t, ban, &model.RuntimeConfig{
		AutoBan: model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 3, BanSeconds: 120},
	})
	call := func() int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "198.51.100.7:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// 前 max 次：认证失败 401（此时已计入违规）。
	for i := 0; i < 3; i++ {
		if code := call(); code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次应 401, 得到 %d", i+1, code)
		}
	}
	if !ban.isIPBanned("198.51.100.7") {
		t.Fatal("3 次失败后应临时封禁来源 IP")
	}
	// 封禁期：运行期守卫直接 403。
	if code := call(); code != http.StatusForbidden {
		t.Fatalf("封禁后应 403, 得到 %d", code)
	}
}

// TestAuthFailGuard_WhitelistExempt 验证白名单来源的认证失败不计违规、不封禁。
func TestAuthFailGuard_WhitelistExempt(t *testing.T) {
	ban := newAutoBanManager()
	h := authBanChain(t, ban, &model.RuntimeConfig{
		AutoBan:     model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 2, BanSeconds: 60},
		IPWhitelist: []string{"198.51.100.7"},
	})
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "198.51.100.7:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("白名单第 %d 次失败应仍 401, 得到 %d", i+1, rec.Code)
		}
	}
	if ban.isIPBanned("198.51.100.7") {
		t.Fatal("白名单来源不应被自动封禁")
	}
}

// TestAuthFailGuard_DisabledNoBan 验证 auto_ban 关闭时不计数、不封禁（交由静态配置与
// 其他层），但认证失败仍照常 401。
func TestAuthFailGuard_DisabledNoBan(t *testing.T) {
	ban := newAutoBanManager()
	h := authBanChain(t, ban, &model.RuntimeConfig{}) // auto_ban 默认关
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "198.51.100.7:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("应 401, 得到 %d", rec.Code)
		}
	}
	if ban.isIPBanned("198.51.100.7") {
		t.Fatal("auto_ban 关闭时不应封禁 IP")
	}
}

// TestAuthFailGuard_OnlyMcp 验证该守卫只统计 /mcp：控制面 /api 认证失败不计违规。
func TestAuthFailGuard_OnlyMcp(t *testing.T) {
	ban := newAutoBanManager()
	store := memory.New()
	cache := newRuntimeConfigCache(store, &model.RuntimeConfig{
		AutoBan: model.RuntimeAutoBan{Enabled: true, WindowSeconds: 60, MaxViolations: 2, BanSeconds: 60},
	})
	h := chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
		runtimeGuardMiddleware(cache, ban),
		authFailGuardMiddleware(ban),
		authMiddleware(failAuth{}),
	)
	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/api 认证失败应 401, 得到 %d", rec.Code)
	}
	if ban.isIPBanned("198.51.100.7") {
		t.Fatal("非 /mcp 认证失败不应计入自动封禁")
	}
}
