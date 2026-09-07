package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/ratelimit"
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
			"remote", requestClientIP(r),
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

// rateLimitPolicy 是数据面 /mcp 三级限流的配额视图（由 cfg 解析，构造时定格）。
//
// defaultLimit 是"维度默认"：AccessKey 未单独配置时、以及 IP 级未单独配置时的兜底；
// ipLimit 为单来源 IP 的有效配额（ip_qps=0 → 沿用 defaultLimit）；
// globalLimit 为整网关 /mcp 全局总闸（<=0 → 不启用）。每级仅在有效 QPS>0 时执行，
// 避免把 Limit{} 交给 memory/redis 触发"0=回退构造默认"导致全局级被静默激活。
type rateLimitPolicy struct {
	defaultLimit ratelimit.Limit
	ipLimit      ratelimit.Limit
	globalLimit  ratelimit.Limit
}

// newRateLimitPolicy 由限流配置解析三级配额：ip 为 0 时复用 qps；窗长取配置值。
func newRateLimitPolicy(rl config.RateLimitConfig) rateLimitPolicy {
	return buildRateLimitPolicy(rl.QPS, rl.IPQPS, rl.GlobalQPS, rl.WindowSeconds)
}

// buildRateLimitPolicy 由各数值组装三级策略；被 newRateLimitPolicy（静态）与
// runtimeRatePolicy（运行期配置覆盖）共用，保证语义一致。windowSec<=0 归一为 60
// （1 分钟默认）。每级 Limit 均携带 WindowSec（滑动窗口长度），内存/Redis 实现据此
// 算容量 QPS×窗长；Burst 已废弃不再参与。
func buildRateLimitPolicy(qps, ipQPS, globalQPS, windowSec int) rateLimitPolicy {
	if windowSec <= 0 {
		windowSec = 60
	}
	if ipQPS <= 0 {
		ipQPS = qps
	}
	return rateLimitPolicy{
		defaultLimit: ratelimit.Limit{QPS: qps, WindowSec: windowSec},
		ipLimit:      ratelimit.Limit{QPS: ipQPS, WindowSec: windowSec},
		globalLimit:  ratelimit.Limit{QPS: globalQPS, WindowSec: windowSec},
	}
}

// rateLimitMiddleware 执行限流，超限返回 429。
//
// 数据面 /mcp 走三级（全局 → IP → key，任一级拒绝即 429）；其余路径（/api 控制面、
// Console 静态等）沿用单维度行为：被管理的 API Key 按自身配额，否则回退限流器
// 构造时的全局默认。两级使用独立限流 key 命名空间（mcp:* vs subject:/ip:），
// 避免控制面流量污染数据面的 IP/全局桶。
//
// ab（可选 autoBan）：被限流拒绝时按来源计违规，达阈值触发临时封禁（IP）/停用
// （key）；被停用的 key 在三级判定前直接 403。ab 为 nil 时不启用。
func rateLimitMiddleware(limiter ratelimit.Limiter, p rateLimitPolicy, ab ...*autoBanManager) Middleware {
	ban := autoBanOf(ab)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			isMcp := strings.HasPrefix(r.URL.Path, mcpPath)
			if isMcp {
				// key 已被自动停用（临时）→ 直接拒绝，不再走限流判定。
				if key := IdentityFrom(ctx).Key; key != nil && ban != nil && ban.isKeySuspended(key.Subject) {
					writeGatewayError(w, r, http.StatusForbidden,
						errs.New(errs.CodeAuthorization, "请求被拒绝"))
					return
				}
			}
			var allowed bool
			if isMcp {
				// /mcp 三级：若守卫已把运行期配置写入 ctx（后台动态阈值），优先采用。
				allowed = allowMcpTiers(ctx, limiter, runtimeRatePolicy(ctx, p), r)
			} else {
				allowed = allowLegacyDimension(ctx, limiter, r)
			}
			if !allowed {
				if isMcp && ban != nil {
					recordAutoBan(ctx, ban, r)
				}
				writeGatewayError(w, r, http.StatusTooManyRequests,
					errs.New(errs.CodeRateLimit, "服务繁忙，请稍后再试"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// autoBanOf 取 variadic 中的第一个非 nil 自动封禁管理器；未提供/为 nil 返回 nil。
func autoBanOf(ab []*autoBanManager) *autoBanManager {
	if len(ab) > 0 {
		return ab[0]
	}
	return nil
}

// recordAutoBan 在被限流拒绝后按来源计一次违规；窗口内达到阈值即临时封禁/停用。
// 仅当运行期配置开启 auto_ban 才生效。operator 不计 IP；key 维度按 identity.Key。
func recordAutoBan(ctx context.Context, ban *autoBanManager, r *http.Request) {
	rc := RuntimeConfigFrom(ctx)
	if rc == nil || !rc.AutoBan.Enabled {
		return
	}
	abCfg := rc.AutoBan
	window := secondsDuration(abCfg.WindowSeconds)
	banDur := secondsDuration(abCfg.BanSeconds)
	max := abCfg.MaxViolations
	if max <= 0 {
		return
	}
	ident := IdentityFrom(ctx)
	ip := ""
	if !ident.Operator {
		ip = requestClientIP(r)
	}
	// 白名单来源可信豁免：不计 IP 违规（避免可信出口被自动封禁）。
	if ip != "" && !ipInEntries(ip, rc.IPWhitelist) && ban.recordIPViolation(ip, window, max) {
		ban.banIP(ip, banDur)
	}
	if ident.Key != nil && ban.recordKeyViolation(ident.Key.Subject, window, max) {
		ban.suspendKey(ident.Key.Subject, banDur)
	}
}

// secondsDuration 把秒数换算为时长；<=0 回退 60s（默认 1 分钟）。
func secondsDuration(n int) time.Duration {
	if n <= 0 {
		n = 60
	}
	return time.Duration(n) * time.Second
}

// authFailGuardMiddleware 观察 /mcp 认证失败并按运行期 auto_ban 配置计来源 IP 违规：
// 刷无效凭据不再绕过防护链——窗口内失败达阈值即临时封禁该 IP（封禁期由运行期守卫
// 对 /mcp 统一 403）。与 loginGuardMiddleware（Console 登录防爆破）同构，但复用数据面
// autoBanManager 与运行期 auto_ban 阈值（同一计数器，429 违规共用）。仅统计 401
// （认证失败），500 等非认证错误不误计；非 /mcp 路径透传；ab 为 nil 时不启用。
func authFailGuardMiddleware(ab ...*autoBanManager) Middleware {
	ban := autoBanOf(ab)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ban == nil || !strings.HasPrefix(r.URL.Path, mcpPath) {
				next.ServeHTTP(w, r)
				return
			}
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status != http.StatusUnauthorized {
				return
			}
			recordAuthFailureViolation(r.Context(), ban, r)
		})
	}
}

// recordAuthFailureViolation 认证失败时计一次来源 IP 的自动封禁违规（白名单豁免）。
// 只走 IP 维度：失败时无身份可归属（operator 不可能认证失败）。阈值/窗口/时长取
// 运行期 auto_ban；白名单来源可信豁免（与 recordAutoBan 的 429 违规一致）。达阈值
// 即临时封禁，等待期由运行期守卫统一 403。
func recordAuthFailureViolation(ctx context.Context, ban *autoBanManager, r *http.Request) {
	rc := RuntimeConfigFrom(ctx)
	if rc == nil || !rc.AutoBan.Enabled {
		return
	}
	ab := rc.AutoBan
	if max := ab.MaxViolations; max <= 0 {
		return
	}
	ip := requestClientIP(r)
	if ip == "" || ipInEntries(ip, rc.IPWhitelist) {
		return
	}
	if ban.recordIPViolation(ip, secondsDuration(ab.WindowSeconds), ab.MaxViolations) {
		ban.banIP(ip, secondsDuration(ab.BanSeconds))
		slog.Warn("MCP 认证失败达阈值，临时封禁来源 IP",
			"ip", ip, "max_violations", ab.MaxViolations,
			"ban_seconds", int(secondsDuration(ab.BanSeconds)/time.Second))
	}
}

// allowMcpTiers 依次裁定 /mcp 的全局、IP、key 三级；每级仅在有效 QPS>0 时消耗配额。
func allowMcpTiers(ctx context.Context, limiter ratelimit.Limiter, p rateLimitPolicy, r *http.Request) bool {
	// 1. 全局总闸：整网关 /mcp 聚合配额。
	if p.globalLimit.QPS > 0 && !limiter.Allow(ctx, "mcp:global", p.globalLimit) {
		return false
	}
	// 2. 单来源 IP（operator/匿名也受此约束）；白名单来源可信豁免，跳过单 IP 层。
	ip := requestClientIP(r)
	if p.ipLimit.QPS > 0 && !whitelistedIP(ctx, ip) && !limiter.Allow(ctx, "mcp:ip:"+ip, p.ipLimit) {
		return false
	}
	// 3. 被管理 API Key（identity.Key 非空）：以默认配额为底，仅覆盖该 key 的 QPS
	//    与（可选）专属滑动窗口（>0 覆盖；0 沿用全局），两者决定该 key 每窗口容量。
	//    QPS=-1（哨兵）表示该 key 不受 key 级限制（仅受 IP/全局闸约束），跳过本层。
	//    Burst 已废弃不再使用。
	if key := IdentityFrom(ctx).Key; key != nil && key.QPS >= 0 {
		limit := p.defaultLimit
		if key.QPS > 0 {
			limit.QPS = key.QPS
		}
		if key.WindowSeconds > 0 {
			limit.WindowSec = key.WindowSeconds
		}
		if limit.QPS > 0 && !limiter.Allow(ctx, "mcp:key:"+key.Subject, limit) {
			return false
		}
	}
	return true
}

// allowLegacyDimension 保留 /api 控制面与 Console 的单维度限流：认证主体优先，
// 否则按来源 IP；被管理的 API Key 携带独立配额时按 key 限额。窗长走 limiter
// 构造默认（控制面不参与运行期治理）。
func allowLegacyDimension(ctx context.Context, limiter ratelimit.Limiter, r *http.Request) bool {
	limit := ratelimit.Limit{}
	if identity := IdentityFrom(ctx); identity.Key != nil && identity.Key.QPS > 0 {
		limit = ratelimit.Limit{QPS: identity.Key.QPS}
	}
	return limiter.Allow(ctx, clientKey(r), limit)
}

// clientKey 生成遗留单维度限流 key：认证后优先用主体，否则用来源 IP。
func clientKey(r *http.Request) string {
	if subject := IdentityFrom(r.Context()).Subject; subject != "anonymous" {
		return "subject:" + subject
	}
	return "ip:" + requestClientIP(r)
}

// requestClientIP 返回本次请求的客户端来源 IP：优先取上下文（最外层 capture
// 中间件已按可信代理规则解析），缺失时回退直连 RemoteAddr（忽略端口）。
func requestClientIP(r *http.Request) string {
	if ip := ClientIPFrom(r.Context()); ip != "" {
		return ip
	}
	return hostOfRemote(r.RemoteAddr)
}

// captureClientIPMiddleware 在最外层解析一次客户端来源 IP 并写入请求上下文，
// 供访问日志、限流与调用观测（tool_call / traffic 落库）共用同一来源。
// 仅当直连对端命中 trusted（可信代理）时才信任转发头，否则只认直连地址。
func captureClientIPMiddleware(trusted []string) Middleware {
	prefixes := trustedPrefixes(trusted)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := resolveClientIP(r.RemoteAddr,
				r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"), prefixes)
			next.ServeHTTP(w, r.WithContext(WithClientIP(r.Context(), ip)))
		})
	}
}

// resolveClientIP 解析客户端来源 IP：
//   - 直连对端（RemoteAddr 主机）未命中 trusted → 只认直连地址（不可伪造）；
//   - 命中 trusted → 从 X-Forwarded-For 右往左取第一个不在 trusted 内的条目，
//     否则回退 X-Real-IP，再回退直连地址。
//
// 纯函数便于单测。IP 均归一：去端口/方括号、IPv4-mapped（::ffff:1.2.3.4）转 IPv4。
func resolveClientIP(remote, xff, xRealIP string, trusted []netip.Prefix) string {
	peer := hostOfRemote(remote)
	if peerAddr, err := netip.ParseAddr(peer); err != nil || !prefixContains(trusted, peerAddr) {
		return peer
	}
	if xff != "" {
		for _, entry := range reverseCSV(xff) {
			if addr, err := netip.ParseAddr(entry); err != nil || !prefixContains(trusted, addr) {
				return entry
			}
		}
	}
	if xRealIP = strings.TrimSpace(xRealIP); xRealIP != "" {
		return normalizeIP(xRealIP)
	}
	return peer
}

// hostOfRemote 从 RemoteAddr 提取纯主机（忽略端口/IPv6 方括号）并归一化。
// 形如 "203.0.113.5:8080"、"1.2.3.4"、"[::1]:8080"、"::1"。
func hostOfRemote(remote string) string {
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return normalizeIP(host)
	}
	if addr, err := netip.ParseAddr(strings.Trim(remote, "[]")); err == nil {
		return normalizeIP(addr.String())
	}
	return normalizeIP(remote)
}

// normalizeIP 归一化 IP 字符串：IPv4-mapped 转 IPv4；非 IP（测试 mock/异常）去方括号原样返回。
func normalizeIP(s string) string {
	s = strings.TrimSpace(strings.Trim(s, "[]"))
	if addr, err := netip.ParseAddr(s); err == nil {
		if addr.Is4In6() {
			addr = addr.Unmap()
		}
		return addr.String()
	}
	return s
}

// reverseCSV 把逗号分隔的转发链拆成条目并逆序（XFF 最右侧最近接直连）。
func reverseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		if entry := normalizeIP(parts[i]); entry != "" {
			out = append(out, entry)
		}
	}
	return out
}

// trustedPrefixes 把可信代理配置（IP 或 CIDR）解析为前缀集合；单 IP 转为 /32、/128。
func trustedPrefixes(entries []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if p, err := netip.ParsePrefix(e); err == nil {
			out = append(out, p)
			continue
		}
		if addr, err := netip.ParseAddr(e); err == nil {
			addr = addr.Unmap()
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return out
}

// prefixContains 判断 addr 是否命中任一可信前缀。
func prefixContains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// writeGatewayError 按目标路径输出统一错误：
// /mcp 走 JSON-RPC 错误（HTTP 状态码 = 拒绝语义，如 403/429），其余走控制面信封。
// 拒绝类文案对外保持通用（不暴露黑名单等内部策略）。
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
