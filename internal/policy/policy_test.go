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
		t.Fatalf("精确 allow 应放行（不依赖声明顺序）: %v", err)
	}
	if err := engine.Authorize(ctx, "agent-c", "payment.query"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("其他主体应被通配 deny 拒绝，得到 %v", err)
	}
}

func TestEngine_WildcardAllowDoesNotShadowExactDeny(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	// 修复前：宽泛 allow 排在精确 deny 之前会被首条命中遮蔽，deny 永不生效。
	store.CreatePolicy(ctx, &model.Policy{
		Name:    "宽泛允许在前",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "*", Tool: "payment.*", Effect: model.PolicyEffectAllow},
			{Subject: "agent-b", Tool: "payment.query", Effect: model.PolicyEffectDeny},
		},
	})
	engine := NewEngine(store)

	if err := engine.Authorize(ctx, "agent-b", "payment.query"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("精确 deny 不应被前置通配 allow 遮蔽，得到 %v", err)
	}
	if err := engine.Authorize(ctx, "agent-c", "payment.query"); err != nil {
		t.Fatalf("未被 deny 覆盖的主体应放行: %v", err)
	}
}

func TestEngine_MostSpecificAllowBeatsBroaderDeny(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	// deny 在前也不影响：最特异 allow 胜过更宽 deny（顺序无关）。
	store.CreatePolicy(ctx, &model.Policy{
		Name:    "宽泛拒绝在前",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "*", Tool: "payment.*", Effect: model.PolicyEffectDeny},
			{Subject: "agent-b", Tool: "payment.query", Effect: model.PolicyEffectAllow},
		},
	})
	engine := NewEngine(store)

	if err := engine.Authorize(ctx, "agent-b", "payment.query"); err != nil {
		t.Fatalf("精确 subject+tool allow 应胜过更宽 deny: %v", err)
	}
	if err := engine.Authorize(ctx, "agent-c", "payment.query"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("agent-c 仍应被宽泛 deny 拒绝，得到 %v", err)
	}
}

func TestEngine_DenyTieWins(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	store.CreatePolicy(ctx, &model.Policy{
		Name:    "同特异平局",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "*", Tool: "payment.query", Effect: model.PolicyEffectAllow},
			{Subject: "*", Tool: "payment.query", Effect: model.PolicyEffectDeny},
		},
	})
	engine := NewEngine(store)

	if err := engine.Authorize(ctx, "agent-x", "payment.query"); !errs.Is(err, errs.CodeAuthorization) {
		t.Fatalf("allow/deny 同特异时应 deny 胜，得到 %v", err)
	}
}

func TestEngine_OrderIndependentAcrossPolicies(t *testing.T) {
	ctx := context.Background()
	build := func(denyFirst bool) *Engine {
		store := memory.New()
		a := &model.Policy{Name: "a", Enabled: true, Rules: []model.PolicyRule{
			{Subject: "*", Tool: "payment.query", Effect: model.PolicyEffectAllow},
		}}
		b := &model.Policy{Name: "b", Enabled: true, Rules: []model.PolicyRule{
			{Subject: "agent-b", Tool: "payment.query", Effect: model.PolicyEffectDeny},
		}}
		if denyFirst {
			_ = store.CreatePolicy(ctx, b)
			_ = store.CreatePolicy(ctx, a)
		} else {
			_ = store.CreatePolicy(ctx, a)
			_ = store.CreatePolicy(ctx, b)
		}
		return NewEngine(store)
	}

	for _, denyFirst := range []bool{true, false} {
		engine := build(denyFirst)
		if err := engine.Authorize(ctx, "agent-b", "payment.query"); !errs.Is(err, errs.CodeAuthorization) {
			t.Fatalf("denyFirst=%v: 跨策略重叠时精确 deny 应生效: %v", denyFirst, err)
		}
		if err := engine.Authorize(ctx, "agent-c", "payment.query"); err != nil {
			t.Fatalf("denyFirst=%v: 其他主体应放行: %v", denyFirst, err)
		}
	}
}
