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
}

// NewServer 组装 HTTP 服务器：统一 MCP 端点 + 控制面 REST API + 健康探针，
// 外面包一层网关中间件链（请求标识 → 访问日志 → 认证 → 限流）。
func NewServer(cfg config.Config, deps Deps) *http.Server {
	mux := http.NewServeMux()

	// 统一 MCP 端点（streamable HTTP 无状态模式）。
	mcpHandler := mcp.NewHandler(deps.MCPService)
	mux.Handle(mcpPath, mcpHandler)
	mux.Handle(mcpPath+"/", mcpHandler)

	// 认证：登录签发 / 状态探测（免认证路径）。
	mux.HandleFunc("POST /api/auth/login", handleLogin(deps.AuthService))
	mux.HandleFunc("GET /api/auth/status", handleAuthStatus(deps.AuthService))

	registerControlRoutes(mux, deps)
	registerHealthRoutes(mux)
	// 内嵌前端 SPA：未匹配 /api 与 /mcp 的路径由 Console 兜底。
	mux.Handle("/", spaHandler())

	handler := chain(mux,
		requestIDMiddleware,
		loggingMiddleware,
		authMiddleware(deps.Auth),
		rateLimitMiddleware(deps.RateLimiter),
	)

	addr := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// registerControlRoutes 注册控制面 REST 路由（供 Vue3 Console 使用）。
func registerControlRoutes(mux *http.ServeMux, deps Deps) {
	control := NewControl(deps.Registry, deps.Store, deps.Metrics, deps.KeyHash)
	// 注册/启用 Server 后即时触发健康巡检（nil 安全，测试可不注入）。
	control.probeNow = deps.ProbeNow
	control.eval = deps.Eval

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

	mux.HandleFunc("GET /api/metrics", control.handleMetrics)
	mux.HandleFunc("GET /api/metrics/trend", control.handleMetricsTrend)
	mux.HandleFunc("GET /api/logs", control.handleLogs)

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
