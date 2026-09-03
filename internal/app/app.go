// Package app 负责组装并启动 MCP Conductor 的全部运行时组件。
//
// 装配原则：模块全部面向 interface，依赖通过构造函数显式注入；
// 后端默认 memory 存储，MySQL 5.7+/Redis 为可选扩展（对应配置开启）。
package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/balancer"
	"github.com/xmj128/mcp-conductor/internal/config"
	"github.com/xmj128/mcp-conductor/internal/gateway"
	"github.com/xmj128/mcp-conductor/internal/health"
	"github.com/xmj128/mcp-conductor/internal/mcpclient"
	"github.com/xmj128/mcp-conductor/internal/observability"
	"github.com/xmj128/mcp-conductor/internal/policy"
	"github.com/xmj128/mcp-conductor/internal/ratelimit"
	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/router"
	"github.com/xmj128/mcp-conductor/internal/storage"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
	mysqlstore "github.com/xmj128/mcp-conductor/internal/storage/mysql"
)

// Run 启动后端并阻塞直到退出信号或服务错误。
func Run(ctx context.Context) error {
	cfgPath := os.Getenv("CONDUCTOR_CONFIG")
	if cfgPath == "" {
		cfgPath = "config.yaml" // 缺失时 config.Load 回退到默认值
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	setupLogger(cfg.Logging)

	store, closeStore, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeStore()

	adapter := mcpclient.New()

	registrySvc := registry.NewService(store, adapter)
	resolver := router.NewResolver(store, store)
	policyEngine := policy.NewEngine(store)
	metrics := observability.NewMetrics()
	recorder := observability.NewRecorder(store, cfg.Observability.RecordBody, cfg.Observability.SampleRate)

	mcpGateway := gateway.NewMCPGateway(
		store,
		resolver,
		balancer.NewRoundRobin(),
		adapter,
		policyEngine,
		metrics,
		recorder,
		gateway.WithUpstreamTimeout(cfg.Gateway.UpstreamTimeout),
		gateway.WithMaxConcurrency(cfg.Gateway.MaxConcurrency),
	)

	authenticator := auth.NewStaticKeys(cfg.Auth.Enabled, cfg.Auth.APIKeys)
	limiter := buildLimiter(cfg)

	server := gateway.NewServer(cfg, gateway.Deps{
		Registry:    registrySvc,
		MCPService:  mcpGateway,
		Metrics:     metrics,
		Store:       store,
		Auth:        authenticator,
		RateLimiter: limiter,
	})

	// 周期性健康巡检：以 initialize 握手为 Probe，首次立即执行一次。
	monitor := health.NewMonitor(store, adapter, 30*time.Second, cfg.Gateway.UpstreamTimeout)
	monitor.CheckOnce(ctx)
	go monitor.Run(ctx)

	return runWithSignal(ctx, server)
}

// openStore 按配置选择存储后端：默认 memory；配置为 mysql 时连接 MySQL 5.7+。
func openStore(ctx context.Context, cfg config.Config) (storage.Store, func(), error) {
	if cfg.Database.Driver != "mysql" {
		return memory.New(), func() {}, nil
	}
	store, err := mysqlstore.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

// buildLimiter 按配置组装限流器：关闭→直通；Redis 可用→分布式限流；
// 否则退化为进程内内存限流。
func buildLimiter(cfg config.Config) ratelimit.Limiter {
	if !cfg.RateLimit.Enabled {
		return ratelimit.AllowAll{}
	}
	if cfg.Redis.Enabled {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		return ratelimit.NewRedisLimiter(rdb, cfg.RateLimit.QPS, time.Second)
	}
	return ratelimit.NewMemoryLimiter(cfg.RateLimit.QPS, cfg.RateLimit.Burst)
}

// runWithSignal 启动 HTTP 服务，并在收到退出信号时优雅关闭。
func runWithSignal(ctx context.Context, server *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- gateway.ListenAndServe(server)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

// setupLogger 按配置设置结构化日志级别与格式。
func setupLogger(cfg config.LoggingConfig) {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
