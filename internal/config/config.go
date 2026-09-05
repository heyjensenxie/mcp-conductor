// Package config 负责统一加载 MCP Conductor 的分组配置。
//
// 配置来源：config.yaml（可选）+ 环境变量。
// 环境变量优先级高于 config.yaml，命名规则为 CONDUCTOR_<GROUP>_<FIELD>，
// 例如 CONDUCTOR_SERVER_PORT、CONDUCTOR_DATABASE_DSN。避免散落全局环境变量解析。
package config

import (
	"encoding/hex"
	"fmt"
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
	Auth          AuthConfig          `yaml:"auth"`
	Credentials   CredentialsConfig   `yaml:"credentials"`
	Logging       LoggingConfig       `yaml:"logging"`
	Observability ObservabilityConfig `yaml:"observability"`
}

// ServerConfig 控制 HTTP 服务监听地址。
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
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

// RateLimitConfig 控制限流；默认关闭（允许所有）。
type RateLimitConfig struct {
	Enabled bool `yaml:"enabled"`
	// QPS 是每个限流维度每秒放行的请求数。
	QPS int `yaml:"qps"`
	// Burst 是令牌桶容量（允许的瞬时突发）。
	Burst int `yaml:"burst"`
}

// CredentialsConfig 控制 Gateway→Upstream 凭证的加密存储。
type CredentialsConfig struct {
	// EncryptionKey 是 AES-256-GCM 的 32 字节密钥（64 位 hex）。
	// 为空时 MySQL 存储拒绝落库明文（仅 memory 模式可在进程内承载）；生产必须配置。
	EncryptionKey string `yaml:"encryption_key"`
}

// AuthConfig 控制 Gateway 与 Control Plane 的认证。
type AuthConfig struct {
	// Enabled 开启后，控制面 /api 仅接受管理令牌（operator_token 或会话），
	// 数据面 /mcp 接受管理令牌与数据面 API Key。
	Enabled bool `yaml:"enabled"`
	// APIKeys 是本实例数据面引导凭据（格式 subject:key，全量授权、仅 /mcp）。
	APIKeys []string `yaml:"api_keys"`
	// OperatorToken 是控制面/Console 的管理凭据；auth.enabled 时必须配置。
	OperatorToken string `yaml:"operator_token"`
	// TokenSecret 用于签发会话令牌的 HMAC 密钥；auth.enabled 时必须配置。
	TokenSecret string `yaml:"token_secret"`
	// SessionTTL 登录会话有效期（默认 12h）。
	SessionTTL time.Duration `yaml:"session_ttl"`
}

// LoggingConfig 控制结构化日志输出级别。
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // text | json
}

// ObservabilityConfig 控制调用观测记录。
type ObservabilityConfig struct {
	// RecordBody 为 true 时记录工具调用参数与返回值（含脱敏配置时同时生效）。
	RecordBody bool `yaml:"record_body"`
	// SampleRate 为 0-1 之间的采样率，控制调用日志采样。
	SampleRate float64 `yaml:"sample_rate"`
}

// Default 返回适合本地开发的最小配置，保证无外部依赖也可启动。
func Default() Config {
	return Config{
		Server: ServerConfig{Host: "0.0.0.0", Port: 8080},
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
			Enabled: false,
			QPS:     0,
			Burst:   1,
		},
		Auth: AuthConfig{
			Enabled:       false,
			APIKeys:       []string{},
			OperatorToken: "",
			TokenSecret:   "",
			SessionTTL:    12 * time.Hour,
		},
		Credentials: CredentialsConfig{
			EncryptionKey: "",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Observability: ObservabilityConfig{
			RecordBody: false,
			SampleRate: 1.0,
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
	if v := lookupEnv("CONDUCTOR_AUTH_SESSION_TTL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Auth.SessionTTL = time.Duration(n) * time.Minute
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
	if v := lookupEnv("CONDUCTOR_OBSERVABILITY_RECORD_BODY"); v != "" {
		cfg.Observability.RecordBody = parseBool(v)
	}
	if v := lookupEnv("CONDUCTOR_OBSERVABILITY_SAMPLE_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Observability.SampleRate = f
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
	switch c.Database.Driver {
	case "memory", "mysql", "":
	default:
		return fmt.Errorf("database.driver 仅支持 memory 或 mysql，收到: %q", c.Database.Driver)
	}
	if c.Database.Driver == "mysql" && strings.TrimSpace(c.Database.DSN) == "" {
		return fmt.Errorf("database.driver 为 mysql 时须提供 database.dsn")
	}
	if c.Auth.Enabled && strings.TrimSpace(c.Auth.OperatorToken) == "" {
		return fmt.Errorf("auth.enabled 时须配置 auth.operator_token（控制面管理凭据）")
	}
	if c.Auth.Enabled && strings.TrimSpace(c.Auth.TokenSecret) == "" {
		return fmt.Errorf("auth.enabled 时须配置 auth.token_secret（签发登录令牌用）")
	}
	if c.Credentials.EncryptionKey != "" {
		key, err := hex.DecodeString(c.Credentials.EncryptionKey)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("credentials.encryption_key 必须是 64 位 hex（AES-256 的 32 字节）")
		}
	}
	return nil
}
