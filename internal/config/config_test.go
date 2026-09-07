package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadDefaults 验证无配置/无环境变量时返回可运行默认值。
func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Driver != "memory" {
		t.Fatalf("默认 driver 应为 memory，得到 %q", cfg.Database.Driver)
	}
	if cfg.Server.Port != 18110 || cfg.Redis.Enabled || cfg.RateLimit.Enabled {
		t.Fatalf("默认值异常: %+v", cfg)
	}
	if !cfg.Auth.Enabled {
		t.Fatal("默认应开启鉴权（控制台登录 / 控制面保护）")
	}
	// 鉴权开启但未配置凭据不应报错：由应用首启自动生成引导令牌。
	if cfg.Auth.OperatorToken != "" || cfg.Auth.TokenSecret != "" {
		t.Fatalf("默认不应携带凭据: %+v", cfg.Auth)
	}
	if cfg.Observability.TrendRetentionDays != 7 {
		t.Fatalf("默认趋势保留应为 7 天，得到 %d", cfg.Observability.TrendRetentionDays)
	}
	if cfg.Observability.TrafficRetentionDays != 90 {
		t.Fatalf("默认调用日志保留应为 90 天，得到 %d", cfg.Observability.TrafficRetentionDays)
	}
}

// TestTrafficRetentionDaysEnv 验证 traffic_retention_days 的 env 覆盖与校验边界：
// 正数生效、0 合法（关闭）；校验层拒绝负值与超 365。
func TestTrafficRetentionDaysEnv(t *testing.T) {
	t.Setenv("CONDUCTOR_OBSERVABILITY_TRAFFIC_RETENTION_DAYS", "120")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(120): %v", err)
	}
	if cfg.Observability.TrafficRetentionDays != 120 {
		t.Fatalf("env 覆盖失败，得到 %d", cfg.Observability.TrafficRetentionDays)
	}

	t.Setenv("CONDUCTOR_OBSERVABILITY_TRAFFIC_RETENTION_DAYS", "0")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load(0 关闭): %v", err)
	}
	if cfg.Observability.TrafficRetentionDays != 0 {
		t.Fatalf("0 应视为关闭，得到 %d", cfg.Observability.TrafficRetentionDays)
	}

	// 负值 env 与趋势保留一致：按「未设置」忽略并回退默认，不报错。
	t.Setenv("CONDUCTOR_OBSERVABILITY_TRAFFIC_RETENTION_DAYS", "-3")
	if cfg, err = Load(""); err != nil {
		t.Fatalf("负数 env 应忽略回退默认: %v", err)
	}
	if cfg.Observability.TrafficRetentionDays != 90 {
		t.Fatalf("负数 env 应回退默认 90，得到 %d", cfg.Observability.TrafficRetentionDays)
	}

	// 校验层边界：负值与超 365 必须报错。
	for _, bad := range []int{-1, 366} {
		c := Default()
		c.Observability.TrafficRetentionDays = bad
		if err := c.validate(); err == nil {
			t.Fatalf("traffic_retention_days=%d 应校验失败", bad)
		}
	}
}

// TestLoadEnvOverridesNewKeys 验证 config.yaml 有但此前缺 env 覆盖的项现在生效。
func TestLoadEnvOverridesNewKeys(t *testing.T) {
	t.Setenv("CONDUCTOR_REDIS_DB", "3")
	t.Setenv("CONDUCTOR_RATELIMIT_BURST", "7")
	t.Setenv("CONDUCTOR_GATEWAY_MAX_CONCURRENCY", "11")
	t.Setenv("CONDUCTOR_LOGGING_FORMAT", "json")
	t.Setenv("CONDUCTOR_RATELIMIT_ENABLED", "true")
	t.Setenv("CONDUCTOR_RATELIMIT_IP_QPS", "120")
	t.Setenv("CONDUCTOR_RATELIMIT_IP_BURST", "12")
	t.Setenv("CONDUCTOR_RATELIMIT_GLOBAL_QPS", "500")
	t.Setenv("CONDUCTOR_RATELIMIT_GLOBAL_BURST", "50")
	t.Setenv("CONDUCTOR_SERVER_TRUSTED_PROXIES", "127.0.0.1, 10.0.0.0/8")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Redis.DB != 3 {
		t.Fatalf("redis.db 覆盖失败: %d", cfg.Redis.DB)
	}
	if cfg.RateLimit.Burst != 7 {
		t.Fatalf("ratelimit.burst 覆盖失败: %d", cfg.RateLimit.Burst)
	}
	if cfg.Gateway.MaxConcurrency != 11 {
		t.Fatalf("gateway.max_concurrency 覆盖失败: %d", cfg.Gateway.MaxConcurrency)
	}
	if cfg.Logging.Format != "json" {
		t.Fatalf("logging.format 覆盖失败: %q", cfg.Logging.Format)
	}
	if !cfg.RateLimit.Enabled {
		t.Fatal("ratelimit.enabled 应为 true")
	}
	if cfg.RateLimit.IPQPS != 120 || cfg.RateLimit.IPBurst != 12 {
		t.Fatalf("ratelimit.ip 覆盖失败: %+v", cfg.RateLimit)
	}
	if cfg.RateLimit.GlobalQPS != 500 || cfg.RateLimit.GlobalBurst != 50 {
		t.Fatalf("ratelimit.global 覆盖失败: %+v", cfg.RateLimit)
	}
	if len(cfg.Server.TrustedProxies) != 2 || cfg.Server.TrustedProxies[1] != "10.0.0.0/8" {
		t.Fatalf("server.trusted_proxies 覆盖失败: %v", cfg.Server.TrustedProxies)
	}
}

// TestLoadEnvOverridesYAML 验证环境变量优先级高于 config.yaml。
func TestLoadEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := "server:\n  port: 9090\nredis:\n  db: 1\n"
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("写临时配置失败: %v", err)
	}

	t.Setenv("CONDUCTOR_SERVER_PORT", "7070")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 7070 {
		t.Fatalf("env 应覆盖 yaml 的 port: %d", cfg.Server.Port)
	}
	if cfg.Redis.DB != 1 {
		t.Fatalf("yaml 的 redis.db 应生效: %d", cfg.Redis.DB)
	}
	if cfg.Gateway.UpstreamTimeout != 10*time.Second {
		t.Fatalf("默认上游超时应为 10s: %v", cfg.Gateway.UpstreamTimeout)
	}
}

// TestLoadTrustedProxies 验证 server.trusted_proxies 从 YAML 加载且非法项被校验拦截。
func TestLoadTrustedProxies(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		p := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("写临时配置失败: %v", err)
		}
		return p
	}

	// 合法 IP/CIDR 列表。
	cfg, err := Load(write("server:\n  trusted_proxies:\n    - 10.0.0.0/8\n    - 127.0.0.1\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Server.TrustedProxies) != 2 {
		t.Fatalf("trusted_proxies 数量异常: %v", cfg.Server.TrustedProxies)
	}

	// 非法项应在启动前被校验拒绝。
	if _, err := Load(write("server:\n  trusted_proxies:\n    - not-an-ip\n")); err == nil {
		t.Fatal("非法的 trusted_proxies 应被拒绝")
	}
}

// TestSecurityIPBlocklist 验证 security.ip_blocklist 的 YAML/env 加载与校验。
func TestSecurityIPBlocklist(t *testing.T) {
	t.Setenv("CONDUCTOR_SECURITY_IP_BLOCKLIST", "203.0.113.9, 198.51.100.0/24")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Security.IPBlocklist) != 2 || cfg.Security.IPBlocklist[1] != "198.51.100.0/24" {
		t.Fatalf("security.ip_blocklist env 覆盖失败: %v", cfg.Security.IPBlocklist)
	}

	// 非法项应在启动前被拒绝。
	t.Setenv("CONDUCTOR_SECURITY_IP_BLOCKLIST", "not-an-ip")
	if _, err := Load(""); err == nil {
		t.Fatal("非法的 ip_blocklist 应被拒绝")
	}
}

// TestRateLimitWindowSeconds 验证 window_seconds 的 env 覆盖与范围校验。
func TestRateLimitWindowSeconds(t *testing.T) {
	t.Setenv("CONDUCTOR_RATELIMIT_WINDOW_SECONDS", "5")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RateLimit.WindowSeconds != 5 {
		t.Fatalf("window_seconds env 覆盖失败: %d", cfg.RateLimit.WindowSeconds)
	}

	for _, bad := range []string{"0", "4000", "-1"} {
		t.Setenv("CONDUCTOR_RATELIMIT_WINDOW_SECONDS", bad)
		if _, err := Load(""); err == nil {
			t.Fatalf("window_seconds=%s 应被拒绝", bad)
		}
	}
}

// TestAutoBanConfig 验证 auto_ban 的 env 覆盖与开启时参数校验。
func TestAutoBanConfig(t *testing.T) {
	t.Setenv("CONDUCTOR_RATELIMIT_AUTO_BAN_ENABLED", "true")
	t.Setenv("CONDUCTOR_RATELIMIT_AUTO_BAN_WINDOW_SECONDS", "30")
	t.Setenv("CONDUCTOR_RATELIMIT_AUTO_BAN_MAX_VIOLATIONS", "3")
	t.Setenv("CONDUCTOR_RATELIMIT_AUTO_BAN_BAN_SECONDS", "120")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ab := cfg.RateLimit.AutoBan
	if !ab.Enabled || ab.WindowSeconds != 30 || ab.MaxViolations != 3 || ab.BanSeconds != 120 {
		t.Fatalf("auto_ban env 覆盖失败: %+v", ab)
	}

	// 开启但参数非法（如 max=0）应拒绝。
	t.Setenv("CONDUCTOR_RATELIMIT_AUTO_BAN_ENABLED", "true")
	t.Setenv("CONDUCTOR_RATELIMIT_AUTO_BAN_MAX_VIOLATIONS", "0")
	if _, err := Load(""); err == nil {
		t.Fatal("auto_ban 开启且 max_violations=0 应被拒绝")
	}
}

// TestLoginLimitConfig 验证 auth.login_limit 的 env 覆盖与开启时参数校验。
func TestLoginLimitConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 安全默认：默认开启（与 auth.enabled 同风格），开箱即防。
	if !cfg.Auth.LoginLimit.Enabled {
		t.Fatal("auth.login_limit 默认应开启")
	}
	if cfg.Auth.LoginLimit.MaxFailures != 5 || cfg.Auth.LoginLimit.WindowSeconds != 300 || cfg.Auth.LoginLimit.BanSeconds != 900 {
		t.Fatalf("auth.login_limit 默认值异常: %+v", cfg.Auth.LoginLimit)
	}

	t.Setenv("CONDUCTOR_AUTH_LOGIN_LIMIT_ENABLED", "true")
	t.Setenv("CONDUCTOR_AUTH_LOGIN_LIMIT_MAX_FAILURES", "3")
	t.Setenv("CONDUCTOR_AUTH_LOGIN_LIMIT_WINDOW_SECONDS", "60")
	t.Setenv("CONDUCTOR_AUTH_LOGIN_LIMIT_BAN_SECONDS", "300")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ll := cfg.Auth.LoginLimit
	if !ll.Enabled || ll.MaxFailures != 3 || ll.WindowSeconds != 60 || ll.BanSeconds != 300 {
		t.Fatalf("auth.login_limit env 覆盖失败: %+v", ll)
	}

	// 开启但参数非法（如 max_failures=0）应拒绝。
	t.Setenv("CONDUCTOR_AUTH_LOGIN_LIMIT_ENABLED", "true")
	t.Setenv("CONDUCTOR_AUTH_LOGIN_LIMIT_MAX_FAILURES", "0")
	if _, err := Load(""); err == nil {
		t.Fatal("auth.login_limit 开启且 max_failures=0 应被拒绝")
	}
}

// TestSecurityIPWhitelist 验证 security.ip_whitelist 的 env 加载与非法项校验。
func TestSecurityIPWhitelist(t *testing.T) {
	t.Setenv("CONDUCTOR_SECURITY_IP_WHITELIST", "127.0.0.1, 10.0.0.0/8")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Security.IPWhitelist) != 2 || cfg.Security.IPWhitelist[1] != "10.0.0.0/8" {
		t.Fatalf("security.ip_whitelist env 覆盖失败: %v", cfg.Security.IPWhitelist)
	}
	t.Setenv("CONDUCTOR_SECURITY_IP_WHITELIST", "not-an-ip")
	if _, err := Load(""); err == nil {
		t.Fatal("非法的 ip_whitelist 应被拒绝")
	}
}
