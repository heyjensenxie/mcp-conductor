package gateway

import (
	"context"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// 请求上下文键：跨中间件传递调用主体与请求标识。
type ctxKey string

const (
	keyIdentity   ctxKey = "gateway.identity"
	keyRequestID  ctxKey = "gateway.request_id"
	keyTraceID    ctxKey = "gateway.trace_id"
	keyClientIP   ctxKey = "gateway.client_ip"
	keyRuntimeCfg ctxKey = "gateway.runtime_config"
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

// WithClientIP 把解析出的客户端来源 IP 写入上下文（由最外层 capture 中间件写入）。
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, keyClientIP, ip)
}

// ClientIPFrom 读取客户端来源 IP；缺失时返回空串（限流/日志按需回退直连地址）。
func ClientIPFrom(ctx context.Context) string {
	if v, ok := ctx.Value(keyClientIP).(string); ok {
		return v
	}
	return ""
}

// WithRuntimeConfig 把本次请求"生效的运行期治理配置"写入上下文（/mcp 守卫解析一次）。
func WithRuntimeConfig(ctx context.Context, cfg *model.RuntimeConfig) context.Context {
	return context.WithValue(ctx, keyRuntimeCfg, cfg)
}

// RuntimeConfigFrom 读取运行期治理配置；无（非 /mcp 或守卫缺失）时返回 nil。
func RuntimeConfigFrom(ctx context.Context) *model.RuntimeConfig {
	if cfg, ok := ctx.Value(keyRuntimeCfg).(*model.RuntimeConfig); ok {
		return cfg
	}
	return nil
}
