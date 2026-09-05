// Package access 提供按 API Key 的访问控制（白名单授权 + 工具过滤）。
//
// 语义：被管理的 API Key（Identity.Key 非空）采用严格白名单——只有 Grants
// 内命中的工具（支持 "*" 与 "server.*" 通配）才可见、可调；非管理身份
// （匿名 / Console 会话）回退到遗留 Policy 规则（保持默认放行语义）。
package access

import (
	"context"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
)

// LegacyAuthorizer 是面向非管理身份的遗留授权器（由 policy.Engine 满足）。
type LegacyAuthorizer interface {
	Authorize(ctx context.Context, subject string, tool string) error
}

// Authorizer 组合白名单与遗留规则：managed key 走白名单，其余走 Legacy。
type Authorizer struct {
	legacy LegacyAuthorizer
}

// NewAuthorizer 组装组合授权器。
func NewAuthorizer(legacy LegacyAuthorizer) *Authorizer {
	return &Authorizer{legacy: legacy}
}

// Authorize 裁定调用主体是否有权调用工具（GatewayName）。
func (a *Authorizer) Authorize(ctx context.Context, identity *auth.Identity, tool string) error {
	if identity.Key == nil {
		return a.legacy.Authorize(ctx, identity.Subject, tool)
	}
	if !identity.Key.Granted(tool) {
		return errs.New(errs.CodeAuthorization, "主体 %q 无权调用工具 %q", identity.Subject, tool)
	}
	return nil
}

// FilterTools 按调用主体裁剪工具注册表：managed key 只返回白名单命中的
// 工具（tools/list 不下发未授权工具），其余主体返回全量。
func FilterTools(identity *auth.Identity, tools []model.Tool) []model.Tool {
	if identity.Key == nil {
		return tools
	}
	out := make([]model.Tool, 0, len(tools))
	for _, tool := range tools {
		if identity.Key.Granted(tool.GatewayName) {
			out = append(out, tool)
		}
	}
	return out
}
