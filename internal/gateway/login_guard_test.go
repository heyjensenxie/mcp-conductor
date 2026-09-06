package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
)

// newTestLoginGuard 构造可拨钟的登录防爆破守卫：max 为窗口内失败阈值。
func newTestLoginGuard(t *time.Time, max int) *loginGuard {
	g := newLoginGuard(config.LoginLimitConfig{Enabled: true, MaxFailures: max})
	g.now = func() time.Time { return *t }
	return g
}

// loginGuardAt 构造窗口 10s、封禁 1min 的守卫（静态时钟），便于断言滑窗/到期语义。
func loginGuardAt(t *time.Time, max int) *loginGuard {
	g := newTestLoginGuard(t, max)
	g.window = 10 * time.Second
	g.ban = time.Minute
	return g
}

// TestLoginGuard_BlocksAtThresholdAndExpires 验证：窗口内失败达阈值即封禁，到期惰性解封。
func TestLoginGuard_BlocksAtThresholdAndExpires(t *testing.T) {
	now := time.Unix(0, 0)
	g := loginGuardAt(&now, 2)
	ip := "203.0.113.9"

	if g.blocked(ip) {
		t.Fatal("未达阈值不应封禁")
	}
	if g.noteFailure(ip) {
		t.Fatal("第 1 次失败不应触发封禁")
	}
	if g.blocked(ip) {
		t.Fatal("未达阈值不应封禁")
	}
	if !g.noteFailure(ip) {
		t.Fatal("第 2 次失败应触发封禁")
	}
	if !g.blocked(ip) {
		t.Fatal("达阈值后来源应被封禁")
	}
	// 到期惰性解封。
	now = now.Add(time.Minute + time.Second)
	if g.blocked(ip) {
		t.Fatal("到 TTL 应自动解封")
	}
}

// TestLoginGuard_PruneAndResetAfterBan 验证：旧记录滑出窗口不累计；触发封禁后计数清空。
func TestLoginGuard_PruneAndResetAfterBan(t *testing.T) {
	now := time.Unix(0, 0)
	g := loginGuardAt(&now, 2)
	ip := "198.51.100.7"

	if g.noteFailure(ip) {
		t.Fatal("第 1 次不应触发")
	}
	now = now.Add(11 * time.Second) // 旧记录滑出 10s 窗口
	if g.noteFailure(ip) {
		t.Fatal("旧记录滑出窗口, 单次失败不应触发")
	}
	if !g.noteFailure(ip) {
		t.Fatal("窗口内 2 条应触发封禁")
	}
	if !g.blocked(ip) {
		t.Fatal("达阈值应封禁")
	}
	// 解封后计数已清空：单次失败不再瞬时触发。
	now = now.Add(time.Minute + time.Second)
	if g.blocked(ip) {
		t.Fatal("封禁应已到期")
	}
	if g.noteFailure(ip) {
		t.Fatal("计数已清空, 单次失败不应触发")
	}
}

// TestLoginGuard_DisabledNeverBans 验证：未启用时不计数、不封禁。
func TestLoginGuard_DisabledNeverBans(t *testing.T) {
	g := newLoginGuard(config.LoginLimitConfig{Enabled: false, MaxFailures: 1})
	if g.blocked("1.2.3.4") {
		t.Fatal("未启用不应封禁")
	}
	for i := 0; i < 5; i++ {
		if g.noteFailure("1.2.3.4") {
			t.Fatal("未启用不应触发封禁")
		}
	}
	if g.blocked("1.2.3.4") {
		t.Fatal("未启用不应封禁")
	}
}

// loginSvc 返回启用管理员账号的认证服务（root / root-pass-123），供 HTTP 层登录测试。
func loginSvc(t *testing.T) *auth.Service {
	t.Helper()
	cfg := config.AuthConfig{
		Enabled:       true,
		TokenSecret:   "0123456789abcdef0123456789abcdef",
		OperatorToken: "op-token-123",
		AdminUsername: "root",
		AdminPassword: "root-pass-123",
	}
	svc, err := auth.NewService(cfg, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// loginRequest 构造指向 /api/auth/login 的请求：同一来源 IP，可拨钟重放。
func loginRequest(ip, password string) *http.Request {
	body := fmt.Sprintf(`{"username":"root","password":%q}`, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.RemoteAddr = ip + ":8080"
	return req
}

// TestLoginGuard_MiddlewareCountsOnly401 验证：登录失败(401)计数，缺 password(400)不计；
// 达阈值后即使密码正确也拒绝；到期后恢复。
func TestLoginGuard_MiddlewareCountsOnly401(t *testing.T) {
	svc := loginSvc(t)
	now := time.Unix(0, 0)
	g := loginGuardAt(&now, 2)
	handler := loginGuardMiddleware(g)(handleLogin(svc))
	ip := "203.0.113.9"

	serve := func(password string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, loginRequest(ip, password))
		return rec.Code
	}

	// 缺 password（400）不计入失败。
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, loginRequest(ip, "")) // password 为空串 → decode 后校验失败 400
	if code := rec.Code; code != http.StatusBadRequest {
		t.Fatalf("缺 password 应 400, 得到 %d", code)
	}
	// 一次错误密码(401)后仍不足阈值：正确密码可登录。
	if code := serve("wrong-pass"); code != http.StatusUnauthorized {
		t.Fatalf("错误密码应 401, 得到 %d", code)
	}
	if code := serve("root-pass-123"); code != http.StatusOK {
		t.Fatalf("未达阈值时正确密码应 200, 得到 %d", code)
	}
	// 再错满 2 次 → 触发封禁；随后正确密码也被 429 拒绝。
	if code := serve("wrong-pass"); code != http.StatusUnauthorized {
		t.Fatalf("错误密码应 401, 得到 %d", code)
	}
	if code := serve("wrong-pass"); code != http.StatusTooManyRequests {
		t.Fatalf("达阈值后登录应 429, 得到 %d", code)
	}
	if !g.blocked(ip) {
		t.Fatal("达阈值后来源应被封禁")
	}
	// 到期解封后正确密码恢复 200。
	now = now.Add(time.Minute + time.Second)
	if code := serve("root-pass-123"); code != http.StatusOK {
		t.Fatalf("解封后正确密码应 200, 得到 %d", code)
	}
}
