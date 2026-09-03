package policy

import (
	"context"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

func TestEngine_NoPoliciesAllows(t *testing.T) {
	store := memory.New()
	engine := NewEngine(store)
	if err := engine.Authorize(context.Background(), "any", "university.search"); err != nil {
		t.Fatalf("空策略应放行: %v", err)
	}
}

func TestEngine_DenyMatch(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	store.CreatePolicy(ctx, &model.Policy{
		Name:    "支付风险",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "anon", Tool: "payment.create", Effect: model.PolicyEffectDeny},
		},
	})
	engine := NewEngine(store)

	if err := engine.Authorize(ctx, "anon", "payment.create"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("应拒绝 payment.create，得到 %v", err)
	}
	if err := engine.Authorize(ctx, "anon", "payment.query"); err != nil {
		t.Fatalf("未命中规则应放行: %v", err)
	}
}

func TestEngine_WildcardToolPrefix(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	store.CreatePolicy(ctx, &model.Policy{
		Name:    "大学域禁写",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "*", Tool: "university.*", Effect: model.PolicyEffectDeny},
		},
	})
	engine := NewEngine(store)

	if err := engine.Authorize(ctx, "agent-a", "university.create"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("前缀通配应拒绝 university.create，得到 %v", err)
	}
	if err := engine.Authorize(ctx, "agent-a", "course.search"); err != nil {
		t.Fatalf("非目标域应放行: %v", err)
	}
}

func TestEngine_FirstMatchWins(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	store.CreatePolicy(ctx, &model.Policy{
		Name:    "精确允许优先于通配拒绝",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "agent-b", Tool: "payment.query", Effect: model.PolicyEffectAllow},
			{Subject: "*", Tool: "payment.*", Effect: model.PolicyEffectDeny},
		},
	})
	engine := NewEngine(store)

	if err := engine.Authorize(ctx, "agent-b", "payment.query"); err != nil {
		t.Fatalf("命中前置 allow 应放行: %v", err)
	}
	if err := engine.Authorize(ctx, "agent-c", "payment.query"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("其他主体应被通配 deny 拒绝，得到 %v", err)
	}
}
