package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
)

// reject_audit.go —— /mcp 拒绝事件的审计落库（认证失败 / 限流 / 封禁 / key 停用）。
//
// 这类请求在进入 tools/call（g.CallTool）之前就被中间件拦下，原先完全不留痕；
// 通过 context 注入的降格审计器（*observability.Recorder 满足）在拒绝点写一条
// 调用日志行（tool 为空串，靠 status + 来源 IP 区分），供按 IP 追溯攻击/异常来源。
// 审计器经 rejectionAuditMiddleware 注入请求上下文，未注入时 rejectAudit 为 no-op，
// 中间件签名与既有测试因此零改动（与 WithIdentity / WithClientIP 同模式）。

// RejectionAuditer 是拒绝事件审计的最小能力；*observability.Recorder 满足。
// 测试可注入假实现断言审计发生。
type RejectionAuditer interface {
	RecordRejection(ctx context.Context, rej observability.Rejection)
}

// rejectionAuditKey 是请求上下文中的拒绝审计器键。
type rejectionAuditKey struct{}

// WithRejectionAudit 把拒绝审计器写入请求上下文；auditer 为 nil 时不注入（no-op）。
func WithRejectionAudit(ctx context.Context, auditer RejectionAuditer) context.Context {
	if auditer == nil {
		return ctx
	}
	return context.WithValue(ctx, rejectionAuditKey{}, auditer)
}

// RejectionAuditFrom 返回请求上下文中的拒绝审计器；未注入返回 nil。
func RejectionAuditFrom(ctx context.Context) RejectionAuditer {
	a, _ := ctx.Value(rejectionAuditKey{}).(RejectionAuditer)
	return a
}

// rejectionAuditMiddleware 把调用日志 Recorder 作为拒绝审计器注入请求上下文。
// auditer 为 nil 时整体不作为（测试/未装配场景）。
func rejectionAuditMiddleware(auditer RejectionAuditer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithRejectionAudit(r.Context(), auditer)))
		})
	}
}

// rejectAudit 在 /mcp 拒绝点写一条拒绝审计。仅当请求确为数据面 /mcp 且上下文已
// 注入审计器时生效（控制面 /api 的限流/鉴权拒绝不进入调用日志，避免污染工具流量）。
func rejectAudit(ctx context.Context, r *http.Request, status string, err error) {
	auditer := RejectionAuditFrom(ctx)
	if auditer == nil || !strings.HasPrefix(r.URL.Path, mcpPath) {
		return
	}
	rej := observability.Rejection{
		RequestID: RequestIDFrom(ctx),
		TraceID:   TraceIDFrom(ctx),
		Status:    status,
		Error:     errs.SafeMessage(err),
		ClientIP:  ClientIPFrom(ctx),
		Timestamp: model.Now(),
	}
	// 仅被管理 API Key 的请求具备可归属主体；匿名/operator/控制面调用不填 Client。
	if ident := IdentityFrom(ctx); ident.Key != nil {
		rej.Client = ident.Subject
	}
	auditer.RecordRejection(ctx, rej)
}
