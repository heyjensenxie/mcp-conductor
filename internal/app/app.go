// Package app 负责组装并启动 MCP Conductor 的全部运行时组件。
//
// 装配原则：模块全部面向 interface，依赖通过构造函数显式注入；
// 后端默认 memory 存储，MySQL 5.7+/Redis 为可选扩展（对应配置开启）。
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/heyjensenxie/mcp-conductor/internal/access"
	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/balancer"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/eval"
	"github.com/heyjensenxie/mcp-conductor/internal/gateway"
	"github.com/heyjensenxie/mcp-conductor/internal/health"
	"github.com/heyjensenxie/mcp-conductor/internal/mcpclient"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/ratelimit"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/router"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
	mysqlstore "github.com/heyjensenxie/mcp-conductor/internal/storage/mysql"
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
	if err := ensureAuthSecrets(&cfg); err != nil {
		return err
	}

	store, closeStore, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeStore()

	adapter := mcpclient.New().WithHeaderFor(credentialHeaders(store))

	registrySvc := registry.NewService(store, adapter).WithProber(adapter)
	resolver := router.NewResolver(store, store).WithRoutes(store).WithInstances(store)
	authorizer := access.NewAuthorizer()
	metrics := observability.NewMetrics()
	recorder := observability.NewRecorder(store, cfg.Observability.SampleRate)

	// MCP 评测：现场拨测（复用带凭据注入的 adapter）+ 进程内运行时指标。
	evalSvc := eval.NewService(store, adapter, adapter, func(serverID string) (eval.RuntimeStats, bool) {
		for _, s := range metrics.SnapshotAll() {
			if s.Key == observability.ServerDimPrefix+serverID {
				return eval.RuntimeStats{
					Available:   s.Totals > 0,
					Totals:      s.Totals,
					SuccessRate: s.SuccessRate,
					P95:         s.P95,
					Errors:      s.Errors,
				}, true
			}
		}
		return eval.RuntimeStats{}, false
	})

	// 周期性健康巡检：以 initialize 握手为 Probe，启动时先全量探一遍，
	// 注册/启用 Server 时经 ProbeNow 即时触发单点探活；实例调用失败时由网关
	// 经 WithInstanceProbe 触发快速探活（见 mcpGateway 装配）。
	monitor := health.NewMonitor(store, adapter, 30*time.Second, cfg.Gateway.UpstreamTimeout)

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
		gateway.WithInstanceProbe(monitor.TriggerCheck),
		// 入参捕获静态种子：数据面实际是否捕获由运行期配置（/mcp 守卫解析进 ctx）
		// 决定，未携带时回退到 config.yaml 的 record_args。
		gateway.WithRecordArgsSeed(cfg.Observability.RecordArgs),
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

	// 启动巡检协程 + 启动时先全量探一遍（Monitor 已在上方随网关装配）。
	monitor.CheckOnce(ctx)
	go monitor.Run(ctx)

	// 分钟桶趋势持久化：已闭合分钟每 60s 幂等落库 + 每小时按保留天数清理。
	replaySvc := gateway.NewReplayService(store, adapter, cfg.Gateway.UpstreamTimeout)
	go runTrendPersist(ctx, store, metrics, cfg.Observability.TrendRetentionDays)
	// 调用日志保留清理：每小时按保留天数分块收敛 traffic_log（默认 90 天，0 关闭）。
	go runTrafficRetention(ctx, store, cfg.Observability.TrafficRetentionDays)

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
		ProbeNow:              monitor.TriggerCheck,
		Eval:                  evalSvc,
		KeyCall:               mcpGateway,
		Replay:                replaySvc,
		TrendRetentionMinutes: cfg.Observability.TrendRetentionDays * 24 * 60,
	})

	return runWithSignal(ctx, server)
}

// ensureAuthSecrets 在鉴权开启且未显式配置时补齐运行凭据：
//   - token_secret / operator_token：内部所需，缺则生成（operator 作为程序化
//     管理凭据，不打印，避免与登录账号混淆）；
//   - admin_password：Console 登录的初始管理员密码，缺则生成并打印到 stdout
//     一次（引导管理员账号），供首次登录使用。
func ensureAuthSecrets(cfg *config.Config) error {
	if !cfg.Auth.Enabled {
		return nil
	}
	if cfg.Auth.OperatorToken == "" {
		token, err := randomHex(16)
		if err != nil {
			return fmt.Errorf("生成 operator_token 失败: %w", err)
		}
		cfg.Auth.OperatorToken = token
	}
	if cfg.Auth.TokenSecret == "" {
		secret, err := randomHex(32)
		if err != nil {
			return fmt.Errorf("生成会话签名密钥失败: %w", err)
		}
		cfg.Auth.TokenSecret = secret
	}
	if cfg.Auth.AdminUsername == "" {
		cfg.Auth.AdminUsername = "admin"
	}
	if cfg.Auth.AdminPassword == "" {
		pass, err := randomHex(12) // 24 位 hex，比常见口令更不易被猜中
		if err != nil {
			return fmt.Errorf("生成管理员密码失败: %w", err)
		}
		cfg.Auth.AdminPassword = pass
		fmt.Fprintf(os.Stdout,
			"\n================================================================\n"+
				"  MCP Conductor 引导管理员账号（Console 登录）\n\n"+
				"    用户名：%s\n    密码：  %s\n\n"+
				"  请立即保存。推荐固化配置：\n"+
				"    CONDUCTOR_AUTH_ADMIN_PASSWORD=%s\n"+
				"  （或 config.yaml 的 auth.admin_password）\n"+
				"  未固化时每次重启会重新生成，旧密码随即失效。\n"+
				"================================================================\n",
			cfg.Auth.AdminUsername, pass, pass)
	}
	return nil
}

// randomHex 生成 n 字节随机数并编码为 hex 字符串。
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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
		now := model.Now()
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
			return ratelimit.NewMemoryLimiter(cfg.RateLimit.QPS, windowFor(cfg))
		}
		return ratelimit.NewRedisLimiter(rdb, cfg.RateLimit.QPS, windowFor(cfg))
	}
	return ratelimit.NewMemoryLimiter(cfg.RateLimit.QPS, windowFor(cfg))
}

// windowFor 把 ratelimit.window_seconds 换算为滑动窗口时长（默认 1 分钟）。
func windowFor(cfg config.Config) time.Duration {
	w := cfg.RateLimit.WindowSeconds
	if w <= 0 {
		w = 60
	}
	return time.Duration(w) * time.Second
}

// runTrendPersist 把指标聚合器的“已闭合分钟桶”每 60s 幂等落库到 trend_minute，
// 并每小时按保留天数（默认 7）清理旧桶。落库/清理失败仅告警：趋势读侧由进程内
// 热桶兜底最近 2h，不影响数据面计数。
func runTrendPersist(ctx context.Context, store storage.TrendStore, metrics *observability.Metrics, retentionDays int) {
	persist := func(now time.Time) {
		if buckets := metrics.PersistedBuckets(now); len(buckets) > 0 {
			if err := store.UpsertTrendBuckets(ctx, buckets); err != nil {
				slog.Warn("趋势落库失败", "error", err, "buckets", len(buckets))
			}
		}
	}
	persist(time.Now()) // 启动即先落一次历史已闭合分钟
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	purge := time.NewTicker(time.Hour)
	defer purge.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			persist(now)
		case <-purge.C:
			if retentionDays <= 0 {
				retentionDays = 7
			}
			cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).
				Truncate(time.Minute).Unix()
			if err := store.DeleteTrendBucketsBefore(ctx, cutoff); err != nil {
				slog.Warn("趋势清理失败", "error", err, "cutoff_minute", cutoff)
			}
		}
	}
}

// runTrafficRetention 每小时按保留天数清理超过保留期的调用日志（traffic_log），
// 遏制观察流水无限膨胀。retentionDays<=0 表示关闭自动清理（默认 90）。
// traffic_log 是最大流水表，单条大 DELETE 在 MySQL 上持锁过久，故分块清理；
// 启动先清一轮，让新配置立即生效。
func runTrafficRetention(ctx context.Context, store storage.TrafficStore, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	purge := func(now time.Time) {
		cutoff := now.UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		purgeTrafficBefore(ctx, store, cutoff, trafficPurgeChunk, trafficPurgeMaxPerRun)
	}
	purge(time.Now()) // 启动先清一轮
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			purge(now)
		}
	}
}

// trafficPurgeChunk / trafficPurgeMaxPerRun 约束单轮调用日志清理的语句粒度：
// 每批 DELETE 最多删 trafficPurgeChunk 行（MySQL 5.7 单表 DELETE 支持 LIMIT），
// 单轮累计上限 trafficPurgeMaxPerRun，控制整点/启动清理的持锁范围与耗时，即使
// 存量庞大也在多小时中渐进收敛。
const (
	trafficPurgeChunk     int64 = 5000
	trafficPurgeMaxPerRun int64 = 200_000
)

// purgeTrafficBefore 分块删除 ts < cutoff 的旧调用日志，返回本轮实际删除行数。
// 每批最多 chunk 行；累计达 maxPerRun 或剩余不足一批即停；失败写告警日志并中止
// （观测流水清理失败不阻断数据面）。
func purgeTrafficBefore(ctx context.Context, store storage.TrafficStore, cutoff time.Time, chunk, maxPerRun int64) int64 {
	var total int64
	for total < maxPerRun {
		if ctx.Err() != nil {
			return total
		}
		deleted, err := store.DeleteTrafficBefore(ctx, cutoff, chunk)
		if err != nil {
			slog.Warn("调用日志清理失败", "error", err, "cutoff", cutoff, "deleted", total)
			return total
		}
		total += deleted
		if deleted < chunk {
			return total
		}
	}
	return total
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
