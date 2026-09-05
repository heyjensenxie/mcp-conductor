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
	if cfg.Server.Port != 8080 || cfg.Redis.Enabled || cfg.RateLimit.Enabled {
		t.Fatalf("默认值异常: %+v", cfg)
	}
	if !cfg.Auth.Enabled {
		t.Fatal("默认应开启鉴权（控制台登录 / 控制面保护）")
	}
	// 鉴权开启但未配置凭据不应报错：由应用首启自动生成引导令牌。
	if cfg.Auth.OperatorToken != "" || cfg.Auth.TokenSecret != "" {
		t.Fatalf("默认不应携带凭据: %+v", cfg.Auth)
	}
}

// TestLoadEnvOverridesNewKeys 验证 config.yaml 有但此前缺 env 覆盖的项现在生效。
func TestLoadEnvOverridesNewKeys(t *testing.T) {
	t.Setenv("CONDUCTOR_REDIS_DB", "3")
	t.Setenv("CONDUCTOR_RATELIMIT_BURST", "7")
	t.Setenv("CONDUCTOR_GATEWAY_MAX_CONCURRENCY", "11")
	t.Setenv("CONDUCTOR_LOGGING_FORMAT", "json")
	t.Setenv("CONDUCTOR_RATELIMIT_ENABLED", "true")

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
