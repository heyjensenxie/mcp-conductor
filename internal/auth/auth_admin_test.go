package auth

import (
	"context"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
)

// adminService 返回启用管理员账号的认证服务（admin / root-pass-123）。
func adminService(t *testing.T) *Service {
	t.Helper()
	cfg := config.AuthConfig{
		Enabled:       true,
		TokenSecret:   testSecretHex,
		OperatorToken: testOperatorToken,
		AdminUsername: "root",
		AdminPassword: "root-pass-123",
		SessionTTL:    24 * time.Hour,
	}
	svc, err := NewService(cfg, seedKeyStore())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// TestAdminLoginAndSession 验证用管理员账号+密码登录签发会话。
func TestAdminLoginAndSession(t *testing.T) {
	svc := adminService(t)
	session, err := svc.Login(context.Background(), "root", "root-pass-123")
	if err != nil {
		t.Fatalf("管理员登录失败: %v", err)
	}
	if session.Token == "" || session.Subject != "root" {
		t.Fatalf("会话不正确: %+v", session)
	}

	identity, err := svc.Authenticate(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Authenticate 会话失败: %v", err)
	}
	if !identity.Operator || identity.Subject != "root" {
		t.Fatalf("会话应视为控制面身份: %+v", identity)
	}
}

func TestAdminLoginWrongPassword(t *testing.T) {
	svc := adminService(t)
	if _, err := svc.Login(context.Background(), "root", "wrong-pass"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("错误密码应返回认证错误，得到 %v", err)
	}
	if _, err := svc.Login(context.Background(), "nobody", "root-pass-123"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("未知用户名应返回认证错误，得到 %v", err)
	}
}

// TestOperatorTokenPasswordStillWorks 验证 operator token 作密码登录保持兼容。
func TestOperatorTokenPasswordStillWorks(t *testing.T) {
	svc := adminService(t)
	session, err := svc.Login(context.Background(), "", testOperatorToken)
	if err != nil {
		t.Fatalf("operator 登录失败: %v", err)
	}
	if session.Subject != operatorSubject {
		t.Fatalf("subject 应为 operator，得到 %q", session.Subject)
	}
}
