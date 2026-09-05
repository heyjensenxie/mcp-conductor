package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/mcp"
	"github.com/xmj128/mcp-conductor/internal/ratelimit"
)

// Middleware 是 HTTP 中间件类型。
type Middleware func(http.Handler) http.Handler

// chain 按给定顺序组合中间件（第一个作为最外层）。
func chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// mcpPath 是统一 MCP 端点的挂载路径。
const mcpPath = "/mcp"

// newRequestID 生成随机十六进制请求/追踪标识。
func newRequestID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// requestIDMiddleware 生成并透传请求/追踪标识。
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = newRequestID()
		}
		ctx := WithTraceID(WithRequestID(r.Context(), requestID), traceID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loggingMiddleware 记录访问日志（状态码、耗时、请求标识）。
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		slog.Info("http_access",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()),
			"remote", clientIP(r),
		)
	})
}

// statusRecorder 包装 ResponseWriter 以捕获状态码，供访问日志使用。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后透传。
func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// authMiddleware 校验调用凭据并写入调用主体；同时执行控制面授权。
//
// 免认证路径：
//   - /api/auth/*、/healthz、/readyz（登录/探针）；
//   - Console SPA 与其静态资源（/、/assets/*、favicon 等）——前端只在 /api、/mcp
//     调用时携带凭据，因此需先能加载登录界面/设置页录入凭据；
//
// 其余路径（/api/*、/mcp）均须通过认证。
// 控制面授权：/api/* 只放行 Operator 身份（管理令牌 / 会话令牌；认证关闭时匿名
// 亦视为 Operator）。数据面 API Key（Identity.Key 非空）即使凭据有效，也仅限
// /mcp 使用，无法访问 /api——与内部 access.Authorizer 的 key×工具白名单正交。
func authMiddleware(authenticator auth.Authenticator) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if strings.HasPrefix(path, "/api/auth/") ||
				path == "/healthz" || path == "/readyz" ||
				(!strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, mcpPath)) {
				next.ServeHTTP(w, r)
				return
			}
			identity, err := authenticator.Authenticate(r.Context(), extractToken(r))
			if err != nil {
				status := http.StatusUnauthorized
				if errs.Is(err, errs.CodeAuthentication) {
					status = http.StatusUnauthorized
				} else {
					status = http.StatusInternalServerError
				}
				writeGatewayError(w, r, status, err)
				return
			}
			if strings.HasPrefix(path, "/api") && !identity.Operator {
				writeGatewayError(w, r, http.StatusForbidden,
					errs.New(errs.CodeAuthorization, "控制面需要管理令牌"))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

// extractToken 从 Authorization: Bearer <key> 或 X-Api-Key 提取凭据。
func extractToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return r.Header.Get("X-Api-Key")
}

// rateLimitMiddleware 按调用主体/IP 执行限流，超限返回 429。
// 被管理的 API Key（identity.Key）携带独立配额时按 key 限额，
// 否则回退到限流器构造时的全局默认。
func rateLimitMiddleware(limiter ratelimit.Limiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := ratelimit.Limit{}
			if identity := IdentityFrom(r.Context()); identity.Key != nil && identity.Key.QPS > 0 {
				limit = ratelimit.Limit{QPS: identity.Key.QPS, Burst: identity.Key.Burst}
			}
			if !limiter.Allow(r.Context(), clientKey(r), limit) {
				writeGatewayError(w, r, http.StatusTooManyRequests,
					errs.New(errs.CodeRateLimit, "请求过于频繁，请稍后重试"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientKey 生成限流维度：认证后优先用主体，否则用来源 IP。
func clientKey(r *http.Request) string {
	if subject := IdentityFrom(r.Context()).Subject; subject != "anonymous" {
		return "subject:" + subject
	}
	return "ip:" + clientIP(r)
}

// clientIP 取客户端来源 IP（忽略端口）。
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// writeGatewayError 按目标路径输出统一错误：
// /mcp 走 JSON-RPC 错误，其余走控制面信封。
func writeGatewayError(w http.ResponseWriter, r *http.Request, status int, err error) {
	code := errs.CodeOf(err)
	if strings.HasPrefix(r.URL.Path, mcpPath) {
		rpcCode := -32603
		switch code {
		case errs.CodeAuthentication:
			rpcCode = -32002
		case errs.CodeAuthorization:
			rpcCode = -32003
		case errs.CodeRateLimit:
			rpcCode = -32029
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		encodeJSON(w, mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &mcp.RPCError{Code: rpcCode, Message: err.Error()},
		})
		return
	}
	writeEnvelope(w, status, string(code), err.Error(), RequestIDFrom(r.Context()), nil)
}

// encodeJSON 便捷 JSON 写出。
func encodeJSON(w http.ResponseWriter, v any) {
	_ = marshal(w, v)
}
