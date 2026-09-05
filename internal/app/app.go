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
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xmj128/mcp-conductor/internal/access"
	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/balancer"
	"github.com/xmj128/mcp-conductor/internal/config"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/gateway"
	"github.com/xmj128/mcp-conductor/internal/health"
	"github.com/xmj128/mcp-conductor/internal/mcpclient"
	"github.com/xmj128/mcp-conductor/internal/model"
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

	adapter := mcpclient.New().WithHeaderFor(credentialHeaders(store))

	registrySvc := registry.NewService(store, adapter)
	resolver := router.NewResolver(store, store)
	policyEngine := policy.NewEngine(store)
	authorizer := access.NewAuthorizer(policyEngine)
	metrics := observability.NewMetrics()
	recorder := observability.NewRecorder(store, cfg.Observability.RecordBody, cfg.Observability.SampleRate)

	mcpGateway := gateway.NewMCPGateway(
		store,
		resolver,
		balancer.NewRoundRobin(),
		adapter,
		authorizer,
		metrics,
		recorder,
		gateway.WithUpstreamTimeout(cfg.Gateway.UpstreamTimeout),
		gateway.WithMaxConcurrency(cfg.Gateway.MaxConcurrency),
	)

	authSvc, err := auth.NewService(cfg.Auth, store)
	if err != nil {
		return err
	}
	// 把 config 中的静态 API Key（auth.api_keys）作为数据面引导 key 落库：
	// 已存在的 subject 跳过，避免覆盖 Console 侧管理；seed 默认全量授权。
	// 注意：API Key 仅能访问 /mcp 数据面，控制面 /api 使用 auth.operator_token。
	if cfg.Auth.Enabled {
		if err := seedBootstrapKeys(ctx, store, cfg.Auth.APIKeys, cfg.Auth.TokenSecret); err != nil {
			return err
		}
	}
	limiter := buildLimiter(cfg)

	// 周期性健康巡检：以 initialize 握手为 Probe，启动时先全量探一遍，
	// 注册/启用 Server 时可被控制面经 ProbeNow 即时触发单点探活。
	monitor := health.NewMonitor(store, adapter, 30*time.Second, cfg.Gateway.UpstreamTimeout)
	monitor.CheckOnce(ctx)
	go monitor.Run(ctx)

	server := gateway.NewServer(cfg, gateway.Deps{
		Registry:    registrySvc,
		MCPService:  mcpGateway,
		Metrics:     metrics,
		Store:       store,
		Auth:        authSvc,
		AuthService: authSvc,
		RateLimiter: limiter,
		KeyHash: func(token string) (string, error) {
			return auth.KeyHash(cfg.Auth.TokenSecret, token)
		},
		ProbeNow: monitor.TriggerCheck,
	})

	return runWithSignal(ctx, server)
}

// seedBootstrapKeys 把 config 中的静态 API Key（subject:key）落库为数据面
// AccessKey，保留 v0.1 配置式体验：已存在按 subject 跳过，seed 默认全量授权
// （*），使引导 key 在 /mcp 上"全量可见可调"。这些 key 无法访问 /api 控制面
// （控制面使用 auth.operator_token），避免配置型凭据升级为管理权限。
func seedBootstrapKeys(ctx context.Context, store storage.AccessKeyStore, apiKeys []string, tokenSecretHex string) error {
	for _, entry := range apiKeys {
		subject, key := entry, entry
		if i := strings.IndexByte(entry, ':'); i > 0 {
			subject, key = entry[:i], entry[i+1:]
		}
		if subject == "" || key == "" {
			continue
		}
		if _, err := store.GetAccessKeyBySubject(ctx, subject); err == nil {
			continue // 已存在（Console 已管理），不覆盖
		}
		keyHash, err := auth.KeyHash(tokenSecretHex, key)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		k := &model.AccessKey{
			Name:      subject,
			Subject:   subject,
			Enabled:   true,
			Grants:    []model.ToolGrant{{GatewayName: "*"}},
			KeyHash:   keyHash,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := store.CreateAccessKey(ctx, k); err != nil {
			return errs.Wrap(errs.CodeInternal, err, "seed API Key %q 失败", subject)
		}
	}
	return nil
}

// openStore 按配置选择存储后端：默认 memory；配置为 mysql 时连接 MySQL 5.7+。
func openStore(ctx context.Context, cfg config.Config) (storage.Store, func(), error) {
	if cfg.Database.Driver != "mysql" {
		return memory.New(), func() {}, nil
	}
	opts := make([]mysqlstore.Option, 0, 1)
	if cfg.Credentials.EncryptionKey != "" {
		opts = append(opts, mysqlstore.WithCredentialKey(cfg.Credentials.EncryptionKey))
	}
	store, err := mysqlstore.Open(ctx, cfg.Database.DSN, opts...)
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

// credentialHeaders 按 Server 的已配置凭证组装上游注入 header：
// static_token → Authorization: Bearer <value>；api_key → Header: <value>。
// 值均来自存储解密，不落日志。
func credentialHeaders(store storage.CredentialStore) func(context.Context, model.Server) map[string]string {
	return func(ctx context.Context, server model.Server) map[string]string {
		creds, err := store.ListCredentialsByServer(ctx, server.ID)
		if err != nil {
			return nil
		}
		headers := make(map[string]string)
		for _, cred := range creds {
			if cred.Value == "" {
				continue
			}
			switch cred.Kind {
			case model.CredentialStaticToken:
				headers["Authorization"] = "Bearer " + cred.Value
			case model.CredentialAPIKey:
				if cred.Header != "" {
					headers[cred.Header] = cred.Value
				}
			}
		}
		return headers
	}
}

// buildLimiter 按配置组装限流器：关闭→直通；Redis 可用→分布式限流；
// Redis 不可用→回退进程内内存限流（避免静默全放行），否则默认内存限流。
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
		// 连接自检：Redis 挂了限流回退单机实现，不因依赖抖动全局放行。
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			slog.Warn("Redis 不可用，限流回退到进程内内存实现", "addr", cfg.Redis.Addr, "error", err)
			return ratelimit.NewMemoryLimiter(cfg.RateLimit.QPS, cfg.RateLimit.Burst)
		}
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

// setupLogger 按配置设置结构化日志级别与格式（text 默认；json 供容器/集中采集）。
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
	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(handler))
}
