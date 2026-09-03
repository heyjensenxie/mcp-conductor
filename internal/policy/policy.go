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

// Engine 按"首条匹配规则生效"裁定权限。
type Engine struct {
	store ListPolicies
}

// NewEngine 创建策略引擎。
func NewEngine(store ListPolicies) *Engine {
	return &Engine{store: store}
}

// Authorize 裁定 subject 是否有权限调用 tool（GatewayName）。
//
// 规则匹配顺序：先声明者优先；命中 allow 直接放行，命中 deny 拒绝。
// 无任何命中规则时默认放行（MVP 阶段 auth 常关闭，避免过度封锁）。
func (e *Engine) Authorize(ctx context.Context, subject string, tool string) error {
	policies, err := e.store.ListPolicies(ctx)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取策略失败")
	}
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		for _, rule := range p.Rules {
			if !matchSubject(rule.Subject, subject) || !matchTool(rule.Tool, tool) {
				continue
			}
			if rule.Effect == model.PolicyEffectDeny {
				return errs.New(errs.CodeAuthorization, "主体 %q 无权调用工具 %q（策略 %q）", subject, tool, p.Name)
			}
			return nil
		}
	}
	return nil
}

// matchSubject 判断规则主体是否与调用主体匹配；"*" 匹配任意。
func matchSubject(ruleSubject, subject string) bool {
	return ruleSubject == "*" || ruleSubject == subject
}

// matchTool 判断规则工具是否与目标工具匹配；支持 "server.*" 等前缀通配。
func matchTool(pattern, tool string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(tool, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == tool
}
