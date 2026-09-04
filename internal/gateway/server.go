package gateway

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/config"
	"github.com/xmj128/mcp-conductor/internal/mcp"
	"github.com/xmj128/mcp-conductor/internal/observability"
	"github.com/xmj128/mcp-conductor/internal/ratelimit"
	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/storage"
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
	control := NewControl(deps.Registry, deps.Store, deps.Metrics)

	mux.HandleFunc("GET /api/servers", control.handleListServers)
	mux.HandleFunc("POST /api/servers", control.handleCreateServer)
	mux.HandleFunc("GET /api/servers/{id}", control.handleGetServer)
	mux.HandleFunc("PATCH /api/servers/{id}/toggle", control.handleToggleServer)
	mux.HandleFunc("DELETE /api/servers/{id}", control.handleDeleteServer)
	mux.HandleFunc("POST /api/servers/{id}/test", control.handleTestServer)
	mux.HandleFunc("GET /api/servers/{id}/tools", control.handleListServerTools)
	mux.HandleFunc("POST /api/servers/{id}/credentials", control.handleCreateCredential)
	mux.HandleFunc("GET /api/servers/{id}/credentials", control.handleListCredentials)

	mux.HandleFunc("GET /api/tools", control.handleListTools)
	mux.HandleFunc("GET /api/routes", control.handleListRoutes)
	mux.HandleFunc("POST /api/routes", control.handleCreateRoute)
	mux.HandleFunc("GET /api/policies", control.handleListPolicies)
	mux.HandleFunc("POST /api/policies", control.handleCreatePolicy)

	mux.HandleFunc("GET /api/metrics", control.handleMetrics)
	mux.HandleFunc("GET /api/logs", control.handleLogs)
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
