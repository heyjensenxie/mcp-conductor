package access

import (
	"context"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
)

// fakeLegacy 是可控的遗留授权器：allowSet 内放行，其余返回授权错误；
// 模拟 policy.Engine 的默认放行/deny 行为。
type fakeLegacy struct {
	allowSet map[string]bool
}

func (f *fakeLegacy) Authorize(_ context.Context, subject, tool string) error {
	if f.allowSet[tool] {
		return nil
	}
	return errs.New(errs.CodeAuthorization, "legacy deny %s -> %s", subject, tool)
}

func managedKey(grants ...model.ToolGrant) *auth.Identity {
	return &auth.Identity{Subject: "partner-a", Key: &model.AccessKey{Subject: "partner-a", Grants: grants}}
}

func anonymous(id *auth.Identity) *auth.Identity {
	if id == nil {
		return &auth.Identity{Subject: "anonymous"}
	}
	return id
}

func TestAuthorizer_ManagedKeyWhitelist(t *testing.T) {
	ctx := context.Background()
	authz := NewAuthorizer(&fakeLegacy{allowSet: map[string]bool{"svc1.b": true}})

	key := managedKey(
		model.ToolGrant{GatewayName: "svc1.a"},
		model.ToolGrant{GatewayName: "svc2.f"},
	)
	for _, tool := range []string{"svc1.a", "svc2.f"} {
		if err := authz.Authorize(ctx, key, tool); err != nil {
			t.Fatalf("白名单工具 %q 应放行: %v", tool, err)
		}
	}
	if err := authz.Authorize(ctx, key, "svc1.b"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("未授权工具应拒绝，得到 %v", err)
	}
	// legacy 即使放行 svc1.b 也不应对 managed key 生效。
}

func TestAuthorizer_ManagedKeyWildcard(t *testing.T) {
	ctx := context.Background()
	authz := NewAuthorizer(&fakeLegacy{})

	key := managedKey(model.ToolGrant{GatewayName: "university.*"})
	if err := authz.Authorize(ctx, key, "university.search_policy"); err != nil {
		t.Fatalf("前缀通配应命中: %v", err)
	}
	if err := authz.Authorize(ctx, key, "billing.create"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("通配未命中的工具应拒绝，得到 %v", err)
	}

	all := managedKey(model.ToolGrant{GatewayName: "*"})
	if err := authz.Authorize(ctx, all, "anything.at.all"); err != nil {
		t.Fatalf("全量通配应放行任意工具: %v", err)
	}
}

func TestAuthorizer_LegacyFallback(t *testing.T) {
	ctx := context.Background()
	legacy := &fakeLegacy{allowSet: map[string]bool{"svc1.a": true}}
	authz := NewAuthorizer(legacy)

	if err := authz.Authorize(ctx, anonymous(nil), "svc1.a"); err != nil {
		t.Fatalf("非管理身份按遗留规则放行: %v", err)
	}
	if err := authz.Authorize(ctx, anonymous(nil), "svc1.b"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("非管理身份 hit legacy deny 应拒绝，得到 %v", err)
	}
}

func TestFilterTools_ManagedKeyOnlyGranted(t *testing.T) {
	tools := []model.Tool{
		{GatewayName: "svc1.a"},
		{GatewayName: "svc1.b"},
		{GatewayName: "svc2.f"},
	}
	key := managedKey(
		model.ToolGrant{GatewayName: "svc1.a"},
		model.ToolGrant{GatewayName: "svc2.f"},
	)
	got := FilterTools(key, tools)
	if len(got) != 2 {
		t.Fatalf("managed key 应只见 2 个工具，得到 %d: %+v", len(got), got)
	}
	for _, tool := range got {
		if tool.GatewayName != "svc1.a" && tool.GatewayName != "svc2.f" {
			t.Fatalf("过滤出未授权工具 %q", tool.GatewayName)
		}
	}
}

func TestFilterTools_LegacyAllVisible(t *testing.T) {
	tools := []model.Tool{{GatewayName: "svc1.a"}, {GatewayName: "svc1.b"}}
	got := FilterTools(anonymous(nil), tools)
	if len(got) != 2 {
		t.Fatalf("非管理身份应见全部工具，得到 %d", len(got))
	}
}

func TestAccessKey_GrantForReturnsConfig(t *testing.T) {
	key := &model.AccessKey{Subject: "p", Grants: []model.ToolGrant{
		{GatewayName: "svc1.a", Headers: map[string]string{"X-Tenant": "t1"}, DefaultArgs: map[string]any{"env": "prod"}},
		{GatewayName: "svc1.*"},
	}}
	grant := key.GrantFor("svc1.a")
	if grant == nil {
		t.Fatal("精确命中 grant 应返回配置")
	}
	if grant.Headers["X-Tenant"] != "t1" {
		t.Fatalf("headers 未带出: %+v", grant.Headers)
	}
	wildcard := key.GrantFor("svc1.b")
	if wildcard == nil || wildcard.GatewayName != "svc1.*" {
		t.Fatalf("通配 grant 应命中返回: %+v", wildcard)
	}
	if key.GrantFor("svc2.a") != nil {
		t.Fatal("不存在的工具 GrantFor 应为 nil")
	}
}

func TestAccessKey_GrantForWildcardBeforeExact(t *testing.T) {
	// 镜像 MySQL loadGrants 的 ORDER BY gateway_name：通配排在前。
	// 修复前首条命中会让精确项的 Headers/DefaultArgs 被通配项吞掉。
	key := &model.AccessKey{Subject: "p", Grants: []model.ToolGrant{
		{GatewayName: "svc1.*", Headers: map[string]string{"X-Env": "staging"}},
		{GatewayName: "svc1.prod", Headers: map[string]string{"X-Env": "prod"}, DefaultArgs: map[string]any{"mode": "canary"}},
	}}
	grant := key.GrantFor("svc1.prod")
	if grant == nil || grant.GatewayName != "svc1.prod" {
		t.Fatalf("精确项在通配之后仍应胜出，得到: %+v", grant)
	}
	if grant.Headers["X-Env"] != "prod" || grant.DefaultArgs["mode"] != "canary" {
		t.Fatalf("应使用精确项调用配置: %+v", grant)
	}
	// 更短前缀不应遮蔽更长前缀。
	nested := &model.AccessKey{Grants: []model.ToolGrant{
		{GatewayName: "svc.*"},
		{GatewayName: "svc.prod.*"},
	}}
	if g := nested.GrantFor("svc.prod.query"); g == nil || g.GatewayName != "svc.prod.*" {
		t.Fatalf("更长前缀应胜出: %+v", g)
	}
}
