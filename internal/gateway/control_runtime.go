package gateway

import (
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// runtimeConfigView 是 /api/runtime-config 的返回：config 为生效配置（存值或回退
// 种子），persisted 标识是否已有后台保存值，rate_limit_enabled 反映启动配置
// ratelimit.enabled（关闭时三级阈值不生效，Console 据此提示惰态）。
type runtimeConfigView struct {
	Config           model.RuntimeConfig `json:"config"`
	Persisted        bool                `json:"persisted"`
	RateLimitEnabled bool                `json:"ratelimit_enabled"`
}

// handleGetRuntimeConfig 返回当前运行期治理配置（存值优先，无则回退种子）。
func (c *Control) handleGetRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	cfg, exists, err := c.store.GetRuntimeConfig(r.Context())
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if !exists {
		cfg = c.runtimeFallbackConfig()
	}
	writeOK(w, RequestIDFrom(r.Context()), runtimeConfigView{Config: *cfg, Persisted: exists, RateLimitEnabled: c.rateLimitEnabled})
}

// handlePutRuntimeConfig 整份覆盖保存运行期治理配置（校验通过后持久化并清缓存）。
func (c *Control) handlePutRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	var cfg model.RuntimeConfig
	if err := decodeBody(r, &cfg); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if err := validateRuntimeConfig(&cfg); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	cfg.UpdatedAt = time.Now().UTC()
	if err := c.store.PutRuntimeConfig(r.Context(), &cfg); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if c.runtimeCache != nil {
		c.runtimeCache.invalidate() // 下次 /mcp 请求即读取新配置
	}
	writeOK(w, RequestIDFrom(r.Context()), runtimeConfigView{Config: cfg, Persisted: true, RateLimitEnabled: c.rateLimitEnabled})
}

// runtimeFallbackConfig 返回无存值时应生效的种子（config.yaml）；守卫缓存未注入
// （单测）时回退空配置。
func (c *Control) runtimeFallbackConfig() *model.RuntimeConfig {
	if c.runtimeCache != nil && c.runtimeCache.fallback != nil {
		return cloneRuntimeConfig(c.runtimeCache.fallback)
	}
	return &model.RuntimeConfig{}
}

// validateRuntimeConfig 校验运行期配置：配额非负；封禁名单每项须为 IP 或 CIDR
// （去空格后规范化）。窗口 window_seconds：0 归一为 60（默认 1 分钟，兼容旧客户端
// 未携带），>3600 拒绝。配额允许 0（沿用默认 / 关闭某级），负数拒绝。
func validateRuntimeConfig(cfg *model.RuntimeConfig) error {
	rl := cfg.RateLimit
	for _, n := range []int{rl.QPS, rl.Burst, rl.IPQPS, rl.IPBurst, rl.GlobalQPS, rl.GlobalBurst} {
		if n < 0 {
			return errs.New(errs.CodeInvalidArgument, "限流配额不能为负数")
		}
	}
	if rl.WindowSeconds < 0 {
		return errs.New(errs.CodeInvalidArgument, "限流窗口不能为负数")
	}
	if rl.WindowSeconds > 3600 {
		return errs.New(errs.CodeInvalidArgument, "限流窗口须在 1..3600 秒")
	}
	if rl.WindowSeconds == 0 {
		cfg.RateLimit.WindowSeconds = 60
	}
	// 自动封禁：非负；<=0 的窗口/次数/时长归一为默认（60s / 5 次 / 300s）。
	ab := cfg.AutoBan
	if ab.WindowSeconds < 0 || ab.MaxViolations < 0 || ab.BanSeconds < 0 {
		return errs.New(errs.CodeInvalidArgument, "自动封禁参数不能为负数")
	}
	if ab.WindowSeconds <= 0 {
		ab.WindowSeconds = 60
	}
	if ab.MaxViolations <= 0 {
		ab.MaxViolations = 5
	}
	if ab.BanSeconds <= 0 {
		ab.BanSeconds = 300
	}
	cfg.AutoBan = ab
	cleaned := make([]string, 0, len(cfg.IPBlocklist))
	for _, entry := range cfg.IPBlocklist {
		e := strings.TrimSpace(entry)
		if e == "" {
			continue
		}
		if _, err := netip.ParseAddr(e); err != nil {
			if _, err := netip.ParsePrefix(e); err != nil {
				return errs.New(errs.CodeInvalidArgument, "封禁名单项 %q 须为 IP 或 CIDR", entry)
			}
		}
		cleaned = append(cleaned, e)
	}
	cfg.IPBlocklist = cleaned
	cleanedW := make([]string, 0, len(cfg.IPWhitelist))
	for _, entry := range cfg.IPWhitelist {
		e := strings.TrimSpace(entry)
		if e == "" {
			continue
		}
		if _, err := netip.ParseAddr(e); err != nil {
			if _, err := netip.ParsePrefix(e); err != nil {
				return errs.New(errs.CodeInvalidArgument, "白名单项 %q 须为 IP 或 CIDR", entry)
			}
		}
		cleanedW = append(cleanedW, e)
	}
	cfg.IPWhitelist = cleanedW
	return nil
}
