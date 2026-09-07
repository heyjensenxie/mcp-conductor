// Package config 负责统一加载 MCP Conductor 的分组配置。
//
// 配置来源：config.yaml（可选）+ 环境变量。
// 环境变量优先级高于 config.yaml，命名规则为 CONDUCTOR_<GROUP>_<FIELD>，
// 例如 CONDUCTOR_SERVER_PORT、CONDUCTOR_DATABASE_DSN。避免散落全局环境变量解析。
package config

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是后端全部分组配置的根结构。
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Redis         RedisConfig         `yaml:"redis"`
	Gateway       GatewayConfig       `yaml:"gateway"`
	RateLimit     RateLimitConfig     `yaml:"ratelimit"`
	Security      SecurityConfig      `yaml:"security"`
	Auth          AuthConfig          `yaml:"auth"`
	Credentials   CredentialsConfig   `yaml:"credentials"`
	Logging       LoggingConfig       `yaml:"logging"`
	Observability ObservabilityConfig `yaml:"observability"`
}

// ServerConfig 控制 HTTP 服务监听地址与客户端 IP 解析。
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	// TrustedProxies 是可信反向代理（IP 或 CIDR）。仅当直连对端命中该列表时，
	// 才信任 X-Forwarded-For / X-Real-IP 作为客户端来源 IP（默认不信任转发头，
	// 避免未设代理时客户端伪造来源）。网关在 nginx/docker 网关后时须配置。
	TrustedProxies []string `yaml:"trusted_proxies"`
}

// DatabaseConfig 控制持久化后端；driver 支持 memory（默认）与 mysql。
// 生产数据库为 MySQL 5.7+，所有 SQL 必须保持 5.7 兼容（见 docs/architecture/database.md）。
type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

// RedisConfig 控制 Redis 连接；Cursor 关闭时应用仍以 memory 模式运行。
type RedisConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// GatewayConfig 控制 MCP 聚合端点与治理参数。
type GatewayConfig struct {
	// UpstreamTimeout 是转发到上游 MCP Server 的请求超时。
	UpstreamTimeout time.Duration `yaml:"upstream_timeout"`
	// MaxConcurrency 是 Gateway 最大并发工具调用数；0 表示不限制。
	MaxConcurrency int `yaml:"max_concurrency"`
}

// RateLimitConfig 控制数据面 /mcp 三级限流；默认关闭（允许所有）。
// 采用「N 秒滑动窗口」：任意连续 WindowSeconds 秒内最多放行 该级QPS×WindowSeconds 次。
type RateLimitConfig struct {
	Enabled bool `yaml:"enabled"`
	// QPS 是每维度的默认速率（每秒）：key 未单独配置时、以及 IP 级未单独配置时沿用。
	QPS int `yaml:"qps"`
	// Burst 已废弃：字段保留兼容既有配置，滑动窗口判定不再使用。
	Burst int `yaml:"burst"`
	// WindowSeconds 是滑动窗口长度（秒，默认 60，1..3600）。各 tier 每窗口容量 = 其有效 QPS×此值。
	WindowSeconds int `yaml:"window_seconds"`
	// GlobalQPS 是整网关 /mcp 的全局总闸速率；<=0 表示不启用全局级。
	GlobalQPS   int `yaml:"global_qps"`
	GlobalBurst int `yaml:"global_burst"`
	// IPQPS 是单来源 IP 的独立速率；0 表示沿用 QPS。
	IPQPS   int `yaml:"ip_qps"`
	IPBurst int `yaml:"ip_burst"`
	// AutoBan 控制自动封禁：来源在检测窗口内被限流 429 达到次数即临时封禁（TTL 自动解封）。
	AutoBan AutoBanConfig `yaml:"auto_ban"`
}

// AutoBanConfig 描述自动封禁参数（IP 与 key 共用一组；运行期可调）。
type AutoBanConfig struct {
	Enabled       bool `yaml:"enabled"`
	WindowSeconds int  `yaml:"window_seconds"` // 检测窗口（秒，默认 60）
	MaxViolations int  `yaml:"max_violations"` // 窗口内触发次数阈值（默认 5）
	BanSeconds    int  `yaml:"ban_seconds"`    // 临时封禁时长（秒，默认 300）
}

// SecurityConfig 控制数据面治理的运行期种子（无后台保存值时生效）。
type SecurityConfig struct {
	// IPBlocklist 是启动期的来源 IP/CIDR 封禁种子（后台保存运行期配置后以保存值为准）。
	IPBlocklist []string `yaml:"ip_blocklist"`
	// IPWhitelist 是可信豁免名单种子（命中来源不受 IP 黑名单/自动封禁(IP)/单 IP 限流影响）。
	IPWhitelist []string `yaml:"ip_whitelist"`
}

// CredentialsConfig 控制 Gateway→Upstream 凭证的加密存储。
type CredentialsConfig struct {
	// EncryptionKey 是 AES-256-GCM 的 32 字节密钥（64 位 hex）。
	// 为空时 MySQL 存储拒绝落库明文（仅 memory 模式可在进程内承载）；生产必须配置。
	EncryptionKey string `yaml:"encryption_key"`
}

// AuthConfig 控制 Gateway 与 Control Plane 的认证。
type AuthConfig struct {
	// Enabled 开启后，控制面 /api 需登录会话或管理令牌，数据面 /mcp 接受 API Key。
	Enabled bool `yaml:"enabled"`
	// APIKeys 是本实例数据面引导凭据（格式 subject:key，全量授权、仅 /mcp）。
	APIKeys []string `yaml:"api_keys"`
	// OperatorToken 是控制面程序化/API 管理令牌；为空时应用首启自动生成（不打印）。
	OperatorToken string `yaml:"operator_token"`
	// TokenSecret 用于签发会话令牌的 HMAC 密钥；为空时应用首启自动生成。
	TokenSecret string `yaml:"token_secret"`
	// AdminUsername / AdminPassword 是 Console 登录的管理员账号（账号+密码）。
	// admin_password 为空时应用首启自动生成并在启动日志打印一次。
	AdminUsername string `yaml:"admin_username"`
	AdminPassword string `yaml:"admin_password"`
	// SessionTTL 登录会话有效期（默认 12h）。
	SessionTTL time.Duration `yaml:"session_ttl"`
	// LoginLimit 控制 Console 登录防爆破：按来源 IP 统计登录失败次数，检测窗口内
	// 达到上限即临时封禁该 IP（TTL 自动解封，进程内状态，与数据面 auto_ban 独立）。
	LoginLimit LoginLimitConfig `yaml:"login_limit"`
}

// LoginLimitConfig 描述 Console 登录防爆破参数（仅作用于 POST /api/auth/login；
// operator_token 走 Authorization 直连 /api 不受影响）。
type LoginLimitConfig struct {
	Enabled bool `yaml:"enabled"`
	// MaxFailures 是检测窗口内登录失败次数阈值，达到即封禁该来源 IP（默认 5）。
	MaxFailures int `yaml:"max_failures"`
	// WindowSeconds 是失败计数滑动窗口（秒，默认 300=5 分钟）。
	WindowSeconds int `yaml:"window_seconds"`
	// BanSeconds 是达到阈值后的临时封禁时长（秒，默认 900=15 分钟），到期自动解封。
	BanSeconds int `yaml:"ban_seconds"`
}

// LoggingConfig 控制结构化日志输出级别。
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // text | json
}

// ObservabilityConfig 控制调用观测记录。
type ObservabilityConfig struct {
	// RecordArgs 为 true 时在调用日志捕获 tools/call 入参（Traffic Replay 回放
	// 用）。默认关（入参可能含隐私/敏感数据）；响应**永不**落库。受 sample_rate
	// 采样影响：被采样丢弃的行连同入参一并丢弃，无法回放。
	RecordArgs bool `yaml:"record_args"`
	// SampleRate 为 0-1 之间的采样率，控制调用日志采样。
	SampleRate float64 `yaml:"sample_rate"`
	// TrendRetentionDays 控制分钟桶趋势（trend_minute）保留天数（1..365，默认 7）。
	// 已闭合分钟每 60s 幂等落库，超过该天数由 app 每小时清理。
	TrendRetentionDays int `yaml:"trend_retention_days"`
	// TrafficRetentionDays 控制调用日志（traffic_log）保留天数（默认 90；0=关闭
	// 自动清理）。app 每小时按该天数分块删除超过保留期的调用日志，遏制流水无限膨胀。
	TrafficRetentionDays int `yaml:"traffic_retention_days"`
}

// Default 返回适合本地开发的最小配置，保证无外部依赖也可启动。
func Default() Config {
	return Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 18110},
		Database: DatabaseConfig{
			Driver: "memory", // 无 MySQL 时以内存存储启动（生产 MySQL 5.7+）
			DSN:    "",
		},
		Redis: RedisConfig{
			Enabled:  false,
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
		Gateway: GatewayConfig{
			UpstreamTimeout: 10 * time.Second,
			MaxConcurrency:  0,
		},
		RateLimit: RateLimitConfig{
			Enabled:       false,
			QPS:           0,
			Burst:         1,
			WindowSeconds: 60, // 查询型流量默认 1 分钟滑动窗口
			AutoBan: AutoBanConfig{
				Enabled:       false,
				WindowSeconds: 60,
				MaxViolations: 5,
				BanSeconds:    300,
			},
		},
		Auth: AuthConfig{
			Enabled:       true, // 默认开启控制台/控制面鉴权；凭据为空时应用首启自动生成并打印引导
			APIKeys:       []string{},
			OperatorToken: "",
			TokenSecret:   "",
			AdminUsername: "admin",
			AdminPassword: "",
			SessionTTL:    12 * time.Hour,
			LoginLimit: LoginLimitConfig{
				Enabled:       true, // 登录防爆破开箱即防：5 次失败 / 5 分钟 → 封该 IP 15 分钟
				MaxFailures:   5,
				WindowSeconds: 300,
				BanSeconds:    900,
			},
		},
		Credentials: CredentialsConfig{
			EncryptionKey: "",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Observability: ObservabilityConfig{
			RecordArgs:           false,
			SampleRate:           1.0,
			TrendRetentionDays:   7,
			TrafficRetentionDays: 90,
		},
	}
}

// Load 加载配置：优先读取 path 指定的 YAML 文件（缺失时使用默认值），
// 再以 CONDUCTOR_<GROUP>_<FIELD> 环境变量覆盖，最后校验必填项。
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		content, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(content, &cfg); err != nil {
				return Config{}, fmt.Errorf("解析 config.yaml 失败: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("读取 config.yaml 失败: %w", err)
		}
	}

	applyEnvOverrides(&cfg)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyEnvOverrides 逐个覆盖关键配置项。使用 lookupEnv 以便区分"未设置"。
func applyEnvOverrides(cfg *Config) {
	if v := lookupEnv("CONDUCTOR_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := lookupEnv("CONDUCTOR_SERVER_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}
	if v := lookupEnv("CONDUCTOR_SERVER_TRUSTED_PROXIES"); v != "" {
		// 多个可信代理以逗号分隔（IP 或 CIDR）。
		cfg.Server.TrustedProxies = splitCSV(v)
	}
	if v := lookupEnv("CONDUCTOR_DATABASE_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := lookupEnv("CONDUCTOR_DATABASE_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := lookupEnv("CONDUCTOR_REDIS_ENABLED"); v != "" {
		cfg.Redis.Enabled = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := lookupEnv("CONDUCTOR_REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Redis.DB = n
		}
	}
	if v := lookupEnv("CONDUCTOR_REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := lookupEnv("CONDUCTOR_AUTH_ENABLED"); v != "" {
		cfg.Auth.Enabled = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_AUTH_API_KEYS"); v != "" {
		// 多个 key 以逗号分隔。
		cfg.Auth.APIKeys = splitCSV(v)
	}
	if v := lookupEnv("CONDUCTOR_AUTH_OPERATOR_TOKEN"); v != "" {
		cfg.Auth.OperatorToken = v
	}
	if v := lookupEnv("CONDUCTOR_AUTH_TOKEN_SECRET"); v != "" {
		cfg.Auth.TokenSecret = v
	}
	if v := lookupEnv("CONDUCTOR_AUTH_ADMIN_USERNAME"); v != "" {
		cfg.Auth.AdminUsername = v
	}
	if v := lookupEnv("CONDUCTOR_AUTH_ADMIN_PASSWORD"); v != "" {
		cfg.Auth.AdminPassword = v
	}
	if v := lookupEnv("CONDUCTOR_AUTH_SESSION_TTL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Auth.SessionTTL = time.Duration(n) * time.Minute
		}
	}
	if v := lookupEnv("CONDUCTOR_AUTH_LOGIN_LIMIT_ENABLED"); v != "" {
		cfg.Auth.LoginLimit.Enabled = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_AUTH_LOGIN_LIMIT_MAX_FAILURES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Auth.LoginLimit.MaxFailures = n
		}
	}
	if v := lookupEnv("CONDUCTOR_AUTH_LOGIN_LIMIT_WINDOW_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Auth.LoginLimit.WindowSeconds = n
		}
	}
	if v := lookupEnv("CONDUCTOR_AUTH_LOGIN_LIMIT_BAN_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Auth.LoginLimit.BanSeconds = n
		}
	}
	if v := lookupEnv("CONDUCTOR_CREDENTIALS_ENCRYPTION_KEY"); v != "" {
		cfg.Credentials.EncryptionKey = v
	}
	if v := lookupEnv("CONDUCTOR_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := lookupEnv("CONDUCTOR_LOGGING_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := lookupEnv("CONDUCTOR_GATEWAY_UPSTREAM_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Gateway.UpstreamTimeout = time.Duration(n) * time.Millisecond
		}
	}
	if v := lookupEnv("CONDUCTOR_GATEWAY_MAX_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Gateway.MaxConcurrency = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_ENABLED"); v != "" {
		cfg.RateLimit.Enabled = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_QPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.QPS = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.Burst = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_WINDOW_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.WindowSeconds = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_AUTO_BAN_ENABLED"); v != "" {
		cfg.RateLimit.AutoBan.Enabled = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_AUTO_BAN_WINDOW_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.AutoBan.WindowSeconds = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_AUTO_BAN_MAX_VIOLATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.AutoBan.MaxViolations = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_AUTO_BAN_BAN_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.AutoBan.BanSeconds = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_GLOBAL_QPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.GlobalQPS = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_GLOBAL_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.GlobalBurst = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_IP_QPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.IPQPS = n
		}
	}
	if v := lookupEnv("CONDUCTOR_RATELIMIT_IP_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimit.IPBurst = n
		}
	}
	if v := lookupEnv("CONDUCTOR_SECURITY_IP_BLOCKLIST"); v != "" {
		// 多个来源 IP/CIDR 以逗号分隔（作为运行期配置无存值时的种子）。
		cfg.Security.IPBlocklist = splitCSV(v)
	}
	if v := lookupEnv("CONDUCTOR_SECURITY_IP_WHITELIST"); v != "" {
		cfg.Security.IPWhitelist = splitCSV(v)
	}
	if v := lookupEnv("CONDUCTOR_OBSERVABILITY_RECORD_ARGS"); v != "" {
		cfg.Observability.RecordArgs = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_OBSERVABILITY_SAMPLE_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Observability.SampleRate = f
		}
	}
	if v := lookupEnv("CONDUCTOR_OBSERVABILITY_TREND_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Observability.TrendRetentionDays = n
		}
	}
	if v := lookupEnv("CONDUCTOR_OBSERVABILITY_TRAFFIC_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Observability.TrafficRetentionDays = n
		}
	}
}

// parseBool 将环境变量字符串解析为布尔值，无法解析时视为 false。
func parseBool(v string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	return err == nil && b
}

// splitCSV 将逗号分隔的字符串拆为去空格列表。
func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// lookupEnv 读取环境变量；缺失时返回空串。
func lookupEnv(key string) string {
	return os.Getenv(key)
}

// validate 校验配置的必填与取值范围。
func (c Config) validate() error {
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 超出合法范围: %d", c.Server.Port)
	}
	for _, p := range c.Server.TrustedProxies {
		if _, err := netip.ParseAddr(p); err != nil {
			if _, err := netip.ParsePrefix(p); err != nil {
				return fmt.Errorf("server.trusted_proxies 项 %q 须为 IP 或 CIDR", p)
			}
		}
	}
	for _, p := range c.Security.IPBlocklist {
		if _, err := netip.ParseAddr(p); err != nil {
			if _, err := netip.ParsePrefix(p); err != nil {
				return fmt.Errorf("security.ip_blocklist 项 %q 须为 IP 或 CIDR", p)
			}
		}
	}
	for _, p := range c.Security.IPWhitelist {
		if _, err := netip.ParseAddr(p); err != nil {
			if _, err := netip.ParsePrefix(p); err != nil {
				return fmt.Errorf("security.ip_whitelist 项 %q 须为 IP 或 CIDR", p)
			}
		}
	}
	if c.RateLimit.WindowSeconds < 1 || c.RateLimit.WindowSeconds > 3600 {
		return fmt.Errorf("ratelimit.window_seconds 须在 1..3600，收到: %d", c.RateLimit.WindowSeconds)
	}
	ab := c.RateLimit.AutoBan
	if ab.WindowSeconds < 0 || ab.MaxViolations < 0 || ab.BanSeconds < 0 {
		return fmt.Errorf("ratelimit.auto_ban 各参数不能为负数")
	}
	if ab.Enabled && (ab.WindowSeconds < 1 || ab.MaxViolations < 1 || ab.BanSeconds < 1) {
		return fmt.Errorf("ratelimit.auto_ban 开启时 window_seconds/max_violations/ban_seconds 均须 ≥1")
	}
	ll := c.Auth.LoginLimit
	if ll.WindowSeconds < 0 || ll.MaxFailures < 0 || ll.BanSeconds < 0 {
		return fmt.Errorf("auth.login_limit 各参数不能为负数")
	}
	if ll.Enabled && (ll.WindowSeconds < 1 || ll.MaxFailures < 1 || ll.BanSeconds < 1) {
		return fmt.Errorf("auth.login_limit 开启时 window_seconds/max_failures/ban_seconds 均须 ≥1")
	}
	switch c.Database.Driver {
	case "memory", "mysql", "":
	default:
		return fmt.Errorf("database.driver 仅支持 memory 或 mysql，收到: %q", c.Database.Driver)
	}
	if c.Database.Driver == "mysql" && strings.TrimSpace(c.Database.DSN) == "" {
		return fmt.Errorf("database.driver 为 mysql 时须提供 database.dsn")
	}
	// 鉴权开启但 operator_token/token_secret 为空时不在本层报错：应用首启会
	// 自动生成并在 stdout 打印引导管理令牌（见 app.ensureAuthSecrets）。若用户
	// 显式提供了 token_secret，则须为合法 hex（长度等校验在 auth.NewService）。
	if c.Auth.Enabled && c.Auth.TokenSecret != "" {
		if _, err := hex.DecodeString(c.Auth.TokenSecret); err != nil {
			return fmt.Errorf("auth.token_secret 须为 hex 字符串")
		}
	}
	if c.Credentials.EncryptionKey != "" {
		key, err := hex.DecodeString(c.Credentials.EncryptionKey)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("credentials.encryption_key 必须是 64 位 hex（AES-256 的 32 字节）")
		}
	}
	if c.Observability.TrendRetentionDays < 1 || c.Observability.TrendRetentionDays > 365 {
		return fmt.Errorf("observability.trend_retention_days 须在 1..365，收到: %d", c.Observability.TrendRetentionDays)
	}
	if c.Observability.TrafficRetentionDays < 0 || c.Observability.TrafficRetentionDays > 365 {
		return fmt.Errorf("observability.traffic_retention_days 须在 0(关闭)..365，收到: %d", c.Observability.TrafficRetentionDays)
	}
	return nil
}
