// Package access 提供按 API Key 的访问控制（白名单授权 + 工具过滤）。
//
// 语义：受管 API Key（Identity.Key 非空）采用严格白名单——只有 Grants 内
// 命中的工具（支持 "*" 与 "server.*" 通配）才可见、可调；控制面 Operator
// 身份（含鉴权关闭时的匿名放行）是可信身份，直接放行；其余无 Key 且非
// Operator 的调用一律拒绝（请改用受管 API Key）。legacy 策略规则已随
// v0.1 授权模型收口下线。
package access

import (
	"context"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// Authorizer 裁定调用主体对工具（GatewayName）的访问：受管 Key 走白名单，
// Operator 信任放行，其余拒绝。
type Authorizer struct{}

// NewAuthorizer 创建授权器（无外部依赖，保留构造以稳定调用面）。
func NewAuthorizer() *Authorizer { return &Authorizer{} }

// Authorize 裁定调用主体是否有权调用工具（GatewayName）。
func (a *Authorizer) Authorize(_ context.Context, identity *auth.Identity, tool string) error {
	if identity.Key != nil {
		if !identity.Key.Granted(tool) {
			return errs.New(errs.CodeAuthorization, "调用方 %q 无权调用工具 %q", identity.Subject, tool)
		}
		return nil
	}
	if identity.Operator {
		return nil
	}
	return errs.New(errs.CodeAuthorization, "未托管主体 %q 无权调用工具 %q：请使用受管 API Key", identity.Subject, tool)
}

// FilterTools 按调用主体裁剪工具注册表：受管 Key 只返回白名单命中的工具
// （tools/list 不下发未授权工具）；Operator/其余无 Key 主体返回全量，
// 便于控制面身份在工具列表中看到全部可用工具。
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
