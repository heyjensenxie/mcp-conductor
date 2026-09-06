package gateway

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
)

// loginGuard 实现 Console 登录防爆破的进程内状态（仅作用于 POST /api/auth/login）：
//   - 按来源 IP 维护检测窗口内的滑动失败计数（ms FIFO）；
//   - 窗口内失败次数达到上限即临时封禁该 IP（时长 ban，到期惰性解封并清计数）；
//   - 封禁期间该 IP 的登录请求（无论密码对错）一律拒绝，不读请求体。
//
// 与数据面 autoBanManager 同构但状态独立：登录失败（401）计数，不消耗 /mcp 的
// 429 违规统计。状态为进程内存态：重启即清，不写持久黑名单、不改账号配置。
type loginGuard struct {
	enabled     bool
	maxFailures int
	window      time.Duration
	ban         time.Duration

	mu    sync.Mutex
	now   func() time.Time     // 便于测试注入时钟
	fails map[string][]int64   // ip → 窗口内失败时刻(ms，升序)
	bans  map[string]time.Time // ip → 解封时刻
}

// newLoginGuard 由静态配置构造登录防爆破守卫；各秒级字段 <=0 由 secondsDuration 归一。
func newLoginGuard(cfg config.LoginLimitConfig) *loginGuard {
	return &loginGuard{
		enabled:     cfg.Enabled,
		maxFailures: cfg.MaxFailures,
		window:      secondsDuration(cfg.WindowSeconds),
		ban:         secondsDuration(cfg.BanSeconds),
		now:         time.Now,
		fails:       make(map[string][]int64),
		bans:        make(map[string]time.Time),
	}
}

// blocked 返回来源 IP 是否处于临时封禁期；已过 TTL 则视为解封并惰性清理。
// 未启用/空 IP 一律不封禁。
func (g *loginGuard) blocked(ip string) bool {
	if g == nil || !g.enabled || ip == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.bans[ip]
	if !ok {
		return false
	}
	if !until.After(g.now()) {
		delete(g.bans, ip)
		return false
	}
	return true
}

// noteFailure 记录来源 IP 的一次登录失败；窗口内失败达到上限则写入封禁、清空该 IP
// 计数并返回 true（调用方据此记日志）。解封后旧计数已清空，需重新攒满才再触发。
func (g *loginGuard) noteFailure(ip string) bool {
	if g == nil || !g.enabled || ip == "" {
		return false
	}
	if g.maxFailures < 1 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	cutoff := now.UnixMilli() - g.window.Milliseconds()

	arr := g.fails[ip]
	start := 0
	for start < len(arr) && arr[start] <= cutoff {
		start++
	}
	arr = arr[start:]
	arr = append(arr, now.UnixMilli())
	if len(arr) >= g.maxFailures {
		g.fails[ip] = nil // 触发即清空，避免解封后旧计数瞬时再触发
		g.bans[ip] = now.Add(g.ban)
		return true
	}
	g.fails[ip] = arr
	return false
}

// loginGuardMiddleware 为登录端点叠加防爆破：封禁期内的请求直接 429（不读 body），
// 否则透传并用 statusRecorder 观察内层结果——仅当返回 401（认证失败）才计一次失败，
// 达阈值则记录封禁日志。guard 为 nil 或未启用时透传（保持调用方/测试向后兼容）。
func loginGuardMiddleware(g *loginGuard) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if g == nil || !g.enabled {
				next.ServeHTTP(w, r)
				return
			}
			ip := requestClientIP(r)
			if g.blocked(ip) {
				writeGatewayError(w, r, http.StatusTooManyRequests,
					errs.New(errs.CodeRateLimit, "登录失败次数过多，请稍后再试"))
				return
			}
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == http.StatusUnauthorized && g.noteFailure(ip) {
				slog.Warn("登录失败达阈值，临时封禁来源 IP",
					"ip", ip, "max_failures", g.maxFailures,
					"ban_seconds", int(g.ban/time.Second))
			}
		})
	}
}
