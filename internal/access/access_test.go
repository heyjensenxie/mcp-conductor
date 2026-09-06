package access

import (
	"context"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

func managedKey(grants ...model.ToolGrant) *auth.Identity {
	return &auth.Identity{Subject: "partner-a", Key: &model.AccessKey{Subject: "partner-a", Grants: grants}}
}

func keyless(id *auth.Identity) *auth.Identity {
	if id == nil {
		return &auth.Identity{Subject: "anonymous"}
	}
	return id
}

func TestAuthorizer_ManagedKeyWhitelist(t *testing.T) {
	ctx := context.Background()
	authz := NewAuthorizer()

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
}

func TestAuthorizer_ManagedKeyWildcard(t *testing.T) {
	ctx := context.Background()
	authz := NewAuthorizer()

	key := managedKey(model.ToolGrant{GatewayName: "university.*"})
	if err := authz.Authorize(ctx, key, "university.search"); err != nil {
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

func TestAuthorizer_OperatorAllowed(t *testing.T) {
	ctx := context.Background()
	authz := NewAuthorizer()
	operator := &auth.Identity{Subject: "operator", Operator: true}
	if err := authz.Authorize(ctx, operator, "any.tool"); err != nil {
		t.Fatalf("Operator 应放行任意工具: %v", err)
	}
}

func TestAuthorizer_UnmanagedNonOperatorDenied(t *testing.T) {
	ctx := context.Background()
	authz := NewAuthorizer()
	if err := authz.Authorize(ctx, keyless(nil), "svc1.a"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("无 Key 且非 Operator 的主体应拒绝，得到 %v", err)
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

func TestFilterTools_KeylessAllVisible(t *testing.T) {
	tools := []model.Tool{{GatewayName: "svc1.a"}, {GatewayName: "svc1.b"}}
	got := FilterTools(keyless(nil), tools)
	if len(got) != 2 {
		t.Fatalf("无 Key 主体应见全部工具，得到 %d", len(got))
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
