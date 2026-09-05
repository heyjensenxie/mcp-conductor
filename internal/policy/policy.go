// Package policy 提供 Tool 级权限裁定（授权）。
//
// MVP 实现较简单的 RBAC：主体（Subject）× Tool（支持前缀通配）→ allow/deny。
// 存储和规则结构均预留未来的 Tenant / Application / User / Agent 等多主体模型。
package policy

import (
	"context"
	"strings"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
)

// ListPolicies 是 Engine 读取策略所需的最小存储能力。
type ListPolicies interface {
	ListPolicies(ctx context.Context) ([]model.Policy, error)
}

// Engine 按"最特异规则生效"裁定权限；deny 与 allow 特异性相同时 deny 胜。
type Engine struct {
	store ListPolicies
}

// NewEngine 创建策略引擎。
func NewEngine(store ListPolicies) *Engine {
	return &Engine{store: store}
}

// Authorize 裁定 subject 是否有权限调用 tool（GatewayName）。
//
// 匹配语义：在所有命中的规则中取"最特异"者——工具维度精确 > 更长前缀 >
// 更短前缀 > "*"，主体维度精确 > "*"（工具维度优先比较）。若 deny 命中且
// （无 allow 或 deny 的特异性不弱于 allow）则拒绝；仅 allow 命中则放行；
// 无任何命中规则时默认放行（MVP 阶段 auth 常关闭，避免过度封锁）。
// 裁定结果与规则声明顺序/存储顺序无关，避免通配 allow 遮蔽精确 deny，
// 也消除 memory 存储 map 迭代顺序带来的不稳定裁定。
func (e *Engine) Authorize(ctx context.Context, subject string, tool string) error {
	policies, err := e.store.ListPolicies(ctx)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取策略失败")
	}
	var allowSpec, denySpec ruleSpec
	var allowHit, denyHit bool
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		for _, rule := range p.Rules {
			tspec, ok := toolSpec(rule.Tool, tool)
			if !ok {
				continue
			}
			sspec, ok := subjectSpec(rule.Subject, subject)
			if !ok {
				continue
			}
			spec := ruleSpec{tool: tspec, subject: sspec}
			if rule.Effect == model.PolicyEffectDeny {
				if !denyHit || spec.moreSpecific(denySpec) {
					denySpec, denyHit = spec, true
				}
				continue
			}
			if !allowHit || spec.moreSpecific(allowSpec) {
				allowSpec, allowHit = spec, true
			}
		}
	}
	// deny 平局胜：仅当 allow 严格更特异时才放行，否则拒绝。
	if denyHit && (!allowHit || !allowSpec.moreSpecific(denySpec)) {
		return errs.New(errs.CodeAuthorization, "主体 %q 无权调用工具 %q（命中 deny 策略）", subject, tool)
	}
	return nil
}

// ruleSpec 表示一条命中规则的特异性（越高越具体）。
type ruleSpec struct {
	tool    int // 工具模式字面量长度：* →0，前缀 →len(prefix)，精确 →len(pattern)
	subject int // 主体：* →0，精确 →1
}

// moreSpecific 报告 a 是否严格比 b 更特异（工具维度优先，其次主体）。
func (a ruleSpec) moreSpecific(b ruleSpec) bool {
	if a.tool != b.tool {
		return a.tool > b.tool
	}
	return a.subject > b.subject
}

// toolSpec 返回规则工具模式对目标工具命中时的特异性；ok=false 表示未命中。
// 精确工具名得分最高，前缀按字面量长度排序，全量 "*" 得分为 0。
func toolSpec(pattern, tool string) (spec int, ok bool) {
	if pattern == "*" {
		return 0, true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-1] // 去掉尾部 "*"，保留结尾 "."
		if strings.HasPrefix(tool, prefix) {
			return len(prefix), true
		}
		return 0, false
	}
	if pattern == tool {
		return len(pattern), true
	}
	return 0, false
}

// subjectSpec 返回规则主体对调用主体的特异性；"*" 匹配任意（0），精确匹配（1）。
func subjectSpec(ruleSubject, subject string) (spec int, ok bool) {
	if ruleSubject == "*" {
		return 0, true
	}
	if ruleSubject == subject {
		return 1, true
	}
	return 0, false
}
