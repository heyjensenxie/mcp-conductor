package gateway

import (
	"context"

	"github.com/xmj128/mcp-conductor/internal/auth"
)

// 请求上下文键：跨中间件传递调用主体与请求标识。
type ctxKey string

const (
	keyIdentity  ctxKey = "gateway.identity"
	keyRequestID ctxKey = "gateway.request_id"
	keyTraceID   ctxKey = "gateway.trace_id"
)

// WithIdentity 把认证后的调用主体写入上下文。
func WithIdentity(ctx context.Context, identity *auth.Identity) context.Context {
	return context.WithValue(ctx, keyIdentity, identity)
}

// IdentityFrom 读取调用主体；未认证时返回 anonymous，防止中间件缺失导致空指针。
func IdentityFrom(ctx context.Context) *auth.Identity {
	if identity, ok := ctx.Value(keyIdentity).(*auth.Identity); ok && identity != nil {
		return identity
	}
	return &auth.Identity{Subject: "anonymous"}
}

// WithRequestID 把请求标识写入上下文。
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, keyRequestID, requestID)
}

// RequestIDFrom 读取请求标识；缺失时返回空串。
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(keyRequestID).(string); ok {
		return v
	}
	return ""
}

// WithTraceID 把追踪标识写入上下文。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, keyTraceID, traceID)
}

// TraceIDFrom 读取追踪标识；缺失时返回空串。
func TraceIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(keyTraceID).(string); ok {
		return v
	}
	return ""
}
