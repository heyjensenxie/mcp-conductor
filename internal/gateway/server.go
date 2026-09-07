package gateway

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/eval"
	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/ratelimit"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// Deps 是构成 HTTP 服务器所需的全部服务依赖。
type Deps struct {
	Registry    *registry.Service
	MCPService  mcp.ToolService
	Metrics     *observability.Metrics
	Store       storage.Store
	Auth        auth.Authenticator
	AuthService *auth.Service
	RateLimiter ratelimit.Limiter
	// KeyHash 把 API Key 明文映射为落库哈希（源自 auth.token_secret）。
	KeyHash func(token string) (string, error)
	// ProbeNow 非阻塞触发对指定 Server 的即时健康巡检（注册/启用 Server 时
	// 调用，nil 表示不触发，交由周期巡检兜底）。
	ProbeNow func(serverID string)
	// Eval 是 MCP 评测服务（质量分 + 回归用例；nil 表示评测未启用）。
	Eval *eval.Service
	// KeyCall 是控制面「按 Key 试调用」能力（*MCPGateway 满足；nil 表示未装配）。
	KeyCall KeyCallService
	// Replay 是控制面「回放捕获调用」能力（*replayService 满足；nil 表示未装配）。
	Replay ReplayService
	// TrendRetentionMinutes 长程分钟桶趋势保留窗口（分钟），<=0 用 NewControl 兜底。
	TrendRetentionMinutes int
}

// NewServer 组装 HTTP 服务器：统一 MCP 端点 + 控制面 REST API + 健康探针，
// 外面包一层网关中间件链（客户端 IP → 请求标识 → 访问日志 → /mcp 运行期守卫 →
// 认证 → 限流）。
func NewServer(cfg config.Config, deps Deps) *http.Server {
	mux := http.NewServeMux()

	// 统一 MCP 端点（streamable HTTP 无状态模式）。
	mcpHandler := mcp.NewHandler(deps.MCPService)
	mux.Handle(mcpPath, mcpHandler)
	mux.Handle(mcpPath+"/", mcpHandler)

	// 认证：登录签发 / 状态探测（免认证路径）。
	// Console 登录防爆破：按来源 IP 统计登录失败，窗口内达上限即临时封禁该 IP。
	mux.Handle("POST /api/auth/login",
		loginGuardMiddleware(newLoginGuard(cfg.Auth.LoginLimit))(handleLogin(deps.AuthService)))
	mux.HandleFunc("GET /api/auth/status", handleAuthStatus(deps.AuthService))

	// 运行期治理配置缓存：封禁名单 + 三级限流阈值，存值优先、config.yaml 作种子。
	runtimeCache := newRuntimeConfigCache(deps.Store, fallbackRuntimeConfig(cfg))
	// 自动封禁管理器（进程内临时封禁状态，随请求惰性过期）。
	autoBan := newAutoBanManager()

	registerControlRoutes(mux, deps, runtimeCache, cfg.RateLimit.Enabled)
	registerHealthRoutes(mux)
	// 内嵌前端 SPA：未匹配 /api 与 /mcp 的路径由 Console 兜底。
	mux.Handle("/", spaHandler())

	// 中间件链：数据面 /mcp 先经运行期守卫（封禁 + 注入生效配置），认证失败计入
	// 自动封禁违规（刷无效凭据达阈值即拉黑 IP），认证成功后再限流。
	handler := chain(mux,
		captureClientIPMiddleware(cfg.Server.TrustedProxies),
		requestIDMiddleware,
		loggingMiddleware,
		runtimeGuardMiddleware(runtimeCache, autoBan),
		authFailGuardMiddleware(autoBan),
		authMiddleware(deps.Auth),
		rateLimitMiddleware(deps.RateLimiter, newRateLimitPolicy(cfg.RateLimit), autoBan),
	)

	addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// registerControlRoutes 注册控制面 REST 路由（供 Vue3 Console 使用）。
// runtimeCache 注入运行期配置缓存（含 config.yaml 种子），供 /api/runtime-config
// 读取/保存后失效。
func registerControlRoutes(mux *http.ServeMux, deps Deps, runtimeCache *runtimeConfigCache, rateLimitEnabled bool) {
	control := NewControl(deps.Registry, deps.Store, deps.Metrics, deps.KeyHash)
	// 注册/启用 Server 后即时触发健康巡检（nil 安全，测试可不注入）。
	control.probeNow = deps.ProbeNow
	control.eval = deps.Eval
	control.keyCall = deps.KeyCall
	control.replay = deps.Replay
	control.runtimeCache = runtimeCache
	control.rateLimitEnabled = rateLimitEnabled
	if deps.TrendRetentionMinutes > 0 {
		control.trendRetentionMinutes = deps.TrendRetentionMinutes
	}

	mux.HandleFunc("GET /api/servers", control.handleListServers)
	mux.HandleFunc("POST /api/servers", control.handleCreateServer)
	mux.HandleFunc("GET /api/servers/{id}", control.handleGetServer)
	mux.HandleFunc("PATCH /api/servers/{id}", control.handleUpdateServer)
	mux.HandleFunc("PATCH /api/servers/{id}/toggle", control.handleToggleServer)
	mux.HandleFunc("DELETE /api/servers/{id}", control.handleDeleteServer)
	mux.HandleFunc("POST /api/servers/{id}/test", control.handleTestServer)
	mux.HandleFunc("POST /api/servers/{id}/rediscover/plan", control.handleServerRediscoverPlan)
	mux.HandleFunc("GET /api/servers/{id}/tools", control.handleListServerTools)
	mux.HandleFunc("POST /api/servers/{id}/credentials", control.handleCreateCredential)
	mux.HandleFunc("GET /api/servers/{id}/credentials", control.handleListCredentials)
	mux.HandleFunc("PATCH /api/servers/{id}/credentials/{credId}", control.handleUpdateCredential)
	mux.HandleFunc("DELETE /api/servers/{id}/credentials/{credId}", control.handleDeleteCredential)
	mux.HandleFunc("GET /api/servers/{id}/instances", control.handleListInstances)
	mux.HandleFunc("POST /api/servers/{id}/instances", control.handleCreateInstance)
	mux.HandleFunc("PATCH /api/servers/{id}/instances/{iid}", control.handleUpdateInstance)
	mux.HandleFunc("PATCH /api/servers/{id}/instances/{iid}/toggle", control.handleToggleInstance)
	mux.HandleFunc("POST /api/servers/{id}/instances/{iid}/test", control.handleTestInstance)
	mux.HandleFunc("DELETE /api/servers/{id}/instances/{iid}", control.handleDeleteInstance)

	mux.HandleFunc("GET /api/tools", control.handleListTools)
	mux.HandleFunc("GET /api/tools/{id}", control.handleGetTool)
	mux.HandleFunc("PATCH /api/tools/{id}", control.handleUpdateTool)
	mux.HandleFunc("PATCH /api/tools/{id}/toggle", control.handleToggleTool)
	mux.HandleFunc("GET /api/routes", control.handleListRoutes)
	mux.HandleFunc("POST /api/routes", control.handleCreateRoute)
	mux.HandleFunc("PATCH /api/routes/{id}", control.handleUpdateRoute)
	mux.HandleFunc("PATCH /api/routes/{id}/toggle", control.handleToggleRoute)
	mux.HandleFunc("DELETE /api/routes/{id}", control.handleDeleteRoute)

	mux.HandleFunc("GET /api/keys", control.handleListKeys)
	mux.HandleFunc("POST /api/keys", control.handleCreateKey)
	mux.HandleFunc("GET /api/keys/{id}", control.handleGetKey)
	mux.HandleFunc("PATCH /api/keys/{id}", control.handleUpdateKey)
	mux.HandleFunc("DELETE /api/keys/{id}", control.handleDeleteKey)
	mux.HandleFunc("POST /api/keys/{id}/rotate", control.handleRotateKeySecret)
	mux.HandleFunc("POST /api/keys/{id}/invoke", control.handleKeyInvoke)

	mux.HandleFunc("GET /api/metrics", control.handleMetrics)
	mux.HandleFunc("GET /api/metrics/trend", control.handleMetricsTrend)
	mux.HandleFunc("GET /api/logs", control.handleLogs)
	mux.HandleFunc("GET /api/logs/{id}", control.handleGetLogDetail)
	mux.HandleFunc("POST /api/logs/{id}/replay", control.handleReplayLog)
	mux.HandleFunc("POST /api/logs/purge-args", control.handlePurgeTrafficArgs)

	mux.HandleFunc("GET /api/runtime-config", control.handleGetRuntimeConfig)
	mux.HandleFunc("PUT /api/runtime-config", control.handlePutRuntimeConfig)

	mux.HandleFunc("GET /api/evaluations/servers/{id}", control.handleEvalMeta)
	mux.HandleFunc("POST /api/evaluations/servers/{id}/quality", control.handleEvalQuality)
	mux.HandleFunc("POST /api/evaluations/servers/{id}/suite", control.handleEvalSuite)
}

// registerHealthRoutes 注册存活/就绪探针，供容器编排使用。
func registerHealthRoutes(mux *http.ServeMux) {
	liveness := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, RequestIDFrom(r.Context()), map[string]string{"status": "ok"})
	})
	mux.Handle("GET /healthz", liveness)
	mux.Handle("GET /readyz", liveness)
}

// ListenAndServe 启动 HTTP 服务器；返回退出错误供上层处理。
func ListenAndServe(server *http.Server) error {
	slog.Info("MCP Conductor 监听中", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP 服务退出: %w", err)
	}
	return nil
}
