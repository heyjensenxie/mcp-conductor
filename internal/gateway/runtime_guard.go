package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// runtimeSnapshot 是某次解析出的"生效运行期配置"及预编译封禁前缀。
// cfg 永不为 nil（无存值/读失败时回退种子，种子为空则空配置）。
type runtimeSnapshot struct {
	cfg      *model.RuntimeConfig
	prefixes []netip.Prefix // 由 cfg.IPBlocklist 预编译，避免每请求逐条解析
	loadedAt time.Time
}

// runtimeGetter 是守卫读取运行期配置所需的最小存储能力（storage.Store 满足）。
type runtimeGetter interface {
	GetRuntimeConfig(ctx context.Context) (*model.RuntimeConfig, bool, error)
}

// runtimeConfigCache 是进程内运行期配置缓存：TTL 内不访问存储，避免被封攻击流
// 每请求打库（DoS 放大器）；PUT 后由控制面 invalidate 立即生效。多实例无 pub/sub，
// 各自 ≤TTL 收敛。
type runtimeConfigCache struct {
	mu       sync.Mutex
	store    runtimeGetter
	fallback *model.RuntimeConfig
	snap     *runtimeSnapshot
	ttl      time.Duration
}

// cacheTTL 是运行期配置缓存有效期。
const cacheTTL = time.Second

// newRuntimeConfigCache 创建缓存；fallback 为无存值时的种子（可含空配置）。
func newRuntimeConfigCache(store runtimeGetter, fallback *model.RuntimeConfig) *runtimeConfigCache {
	return &runtimeConfigCache{store: store, fallback: fallback, ttl: cacheTTL}
}

// snapshot 返回当前生效快照：缓存未命中或过期时从存储重载（读失败 fail-open 回退种子）。
func (c *runtimeConfigCache) snapshot(ctx context.Context) *runtimeSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snap == nil || time.Since(c.snap.loadedAt) > c.ttl {
		c.snap = c.load(ctx)
	}
	return c.snap
}

// invalidate 使缓存失效（控制面保存后调用，下次请求即重新读取）。
func (c *runtimeConfigCache) invalidate() {
	c.mu.Lock()
	c.snap = nil
	c.mu.Unlock()
}

// load 从存储重载快照：无存值或读取失败时回退种子。
func (c *runtimeConfigCache) load(ctx context.Context) *runtimeSnapshot {
	cfg, exists, err := c.store.GetRuntimeConfig(ctx)
	if err != nil {
		slog.Warn("读取运行期治理配置失败，回退种子", "error", err)
		cfg, exists = nil, false
	}
	if !exists {
		cfg = c.fallback
	}
	if cfg == nil {
		cfg = &model.RuntimeConfig{}
	}
	return &runtimeSnapshot{cfg: cloneRuntimeConfig(cfg), prefixes: trustedPrefixes(cfg.IPBlocklist), loadedAt: time.Now()}
}

// cloneRuntimeConfig 深拷贝运行期配置（blocklist/whitelist 切片隔离），避免调用方改动缓存内数据。
func cloneRuntimeConfig(cfg *model.RuntimeConfig) *model.RuntimeConfig {
	cp := *cfg
	if cfg.IPBlocklist != nil {
		cp.IPBlocklist = append([]string(nil), cfg.IPBlocklist...)
	}
	if cfg.IPWhitelist != nil {
		cp.IPWhitelist = append([]string(nil), cfg.IPWhitelist...)
	}
	return &cp
}

// runtimeGuardMiddleware 在数据面 /mcp 应用"运行期治理"：
//   - 解析生效运行期配置（存储存值 > config.yaml 种子）写入 ctx，供限流中间件复用；
//   - 来源 IP 命中封禁名单 → 403（errs.CodeAuthorization，/mcp 映射 JSON-RPC -32003）；
//   - （可选 autoBan）来源 IP 处于自动封禁期 → 同样 403 通用。
//
// 仅拦 /mcp；/api 控制面与 Console 不受影响（误封管理出口不致锁死后台）。放在
// 访问日志之后（被封请求留日志）、auth 之前（封禁不依赖身份）。ab 为 nil 时不启用
// 自动封禁（variadic 便于既有调用/测试不变）。
func runtimeGuardMiddleware(cache *runtimeConfigCache, ab ...*autoBanManager) Middleware {
	ban := autoBanOf(ab)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, mcpPath) {
				next.ServeHTTP(w, r)
				return
			}
			snap := cache.snapshot(r.Context())
			ctx := WithRuntimeConfig(r.Context(), snap.cfg)
			ip := requestClientIP(r)
			trusted := ipInEntries(ip, snap.cfg.IPWhitelist)
			if !trusted && (blockedByPrefix(ip, snap.prefixes) || (ban != nil && ban.isIPBanned(ip))) {
				// 对外只回通用 403，不暴露"黑名单/自动封禁"，避免让被封方得知策略细节。
				writeGatewayError(w, r, http.StatusForbidden,
					errs.New(errs.CodeAuthorization, "请求被拒绝"))
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// blockedByPrefix 判定来源 IP 是否命中封禁前缀；非 IP 输入视为未命中。
func blockedByPrefix(ip string, prefixes []netip.Prefix) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return prefixContains(prefixes, addr)
}

// ipInEntries 判定来源 IP 是否命中一组 IP/CIDR 文本条目（白名单/黑名单通用）。
func ipInEntries(ip string, entries []string) bool {
	if len(entries) == 0 {
		return false
	}
	return blockedByPrefix(ip, trustedPrefixes(entries))
}

// whitelistedIP 判定请求来源是否在运行期配置的可信白名单中（命中即可信豁免）。
func whitelistedIP(ctx context.Context, ip string) bool {
	rc := RuntimeConfigFrom(ctx)
	return rc != nil && ipInEntries(ip, rc.IPWhitelist)
}

// fallbackRuntimeConfig 由静态配置组装"无存值时的种子"运行期配置。
func fallbackRuntimeConfig(cfg config.Config) *model.RuntimeConfig {
	rl := cfg.RateLimit
	return &model.RuntimeConfig{
		RateLimit: model.RuntimeRateLimit{
			QPS:           rl.QPS,
			Burst:         rl.Burst,
			WindowSeconds: rl.WindowSeconds,
			IPQPS:         rl.IPQPS,
			IPBurst:       rl.IPBurst,
			GlobalQPS:     rl.GlobalQPS,
			GlobalBurst:   rl.GlobalBurst,
		},
		AutoBan: model.RuntimeAutoBan{
			Enabled:       rl.AutoBan.Enabled,
			WindowSeconds: rl.AutoBan.WindowSeconds,
			MaxViolations: rl.AutoBan.MaxViolations,
			BanSeconds:    rl.AutoBan.BanSeconds,
		},
		IPBlocklist: cfg.Security.IPBlocklist,
		IPWhitelist: cfg.Security.IPWhitelist,
	}
}

// runtimeRatePolicy 返回本次请求应使用的限流策略：ctx 已带运行期配置（/mcp 守卫
// 已解析）则用其阈值与窗长，否则用静态回退（如非 /mcp 或守卫缺失的测试路径）。
func runtimeRatePolicy(ctx context.Context, static rateLimitPolicy) rateLimitPolicy {
	if rc := RuntimeConfigFrom(ctx); rc != nil {
		rl := rc.RateLimit
		return buildRateLimitPolicy(rl.QPS, rl.IPQPS, rl.GlobalQPS, rl.WindowSeconds)
	}
	return static
}
