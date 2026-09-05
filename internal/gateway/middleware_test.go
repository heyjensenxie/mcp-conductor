package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
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
