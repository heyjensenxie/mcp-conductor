package auth

import (
	"context"
	"testing"
	"time"

	"github.com/xmj128/mcp-conductor/internal/config"
	"github.com/xmj128/mcp-conductor/internal/errs"
)

// testSecretHex 是 16 字节 HMAC 密钥（32 位 hex）。
const testSecretHex = "00112233445566778899aabbccddeeff"

func testService(t *testing.T, enabled bool) *Service {
	t.Helper()
	svc, err := NewService(config.AuthConfig{
		Enabled:     enabled,
		APIKeys:     []string{"admin:pw"},
		TokenSecret: testSecretHex,
		SessionTTL:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestLoginAndAuthenticate(t *testing.T) {
	svc := testService(t, true)
	session, err := svc.Login("admin", "pw")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if session.Token == "" || session.Subject != "admin" {
		t.Fatalf("会话不正确: %+v", session)
	}

	identity, err := svc.Authenticate(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Authenticate 会话令牌失败: %v", err)
	}
	if identity.Subject != "admin" {
		t.Fatalf("主体应为 admin，得到 %q", identity.Subject)
	}
}

func TestAuthenticateStaticKeyStillWorks(t *testing.T) {
	svc := testService(t, true)
	identity, err := svc.Authenticate(context.Background(), "pw")
	if err != nil {
		t.Fatalf("静态 Key 也应放行: %v", err)
	}
	if identity.Subject != "admin" {
		t.Fatalf("静态 Key 主体应为 admin，得到 %q", identity.Subject)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc := testService(t, true)
	if _, err := svc.Login("admin", "wrong"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("错误密码应拒绝，得到 %v", err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	svc := testService(t, true)
	session, _ := svc.Login("admin", "pw")
	tampered := session.Token[:len(session.Token)-4] + "beef"
	if _, err := svc.Authenticate(context.Background(), tampered); err == nil {
		t.Fatal("篡改令牌应被拒绝")
	}
}

func TestWrongSecretRejected(t *testing.T) {
	svc := testService(t, true)
	session, _ := svc.Login("admin", "pw")
	other, _ := NewService(config.AuthConfig{
		Enabled: true, APIKeys: []string{"admin:pw"},
		TokenSecret: "ffeeddccbbaa99887766554433221100",
		SessionTTL:  24 * time.Hour,
	})
	if _, err := other.Authenticate(context.Background(), session.Token); err == nil {
		t.Fatal("错误密钥应拒绝令牌")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	svc := testService(t, true)
	// 用内部 sign 构造已过期令牌。
	token, err := svc.sign("admin", time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), token); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("过期令牌应拒绝，得到 %v", err)
	}
}

func TestDisabledAuthAnonymous(t *testing.T) {
	svc := testService(t, false)
	identity, err := svc.Authenticate(context.Background(), "")
	if err != nil || identity.Subject != "anonymous" {
		t.Fatalf("认证关闭应匿名放行: %v / %+v", err, identity)
	}
	session, err := svc.Login("admin", "pw")
	if err != nil || session.Token != "" {
		t.Fatalf("认证关闭时登录返回空令牌: %v / %+v", err, session)
	}
}