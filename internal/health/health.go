// Package health 提供 MCP Server 健康检查抽象与周期巡检。
//
// Checker 只关心"探测一次并给出状态结论"，具体探测方式（MCP initialize、
// 简单 TCP 连通性等）由实现注入，本包不绑定具体协议。
package health

import (
	"context"
	"log/slog"
	"time"

	"github.com/xmj128/mcp-conductor/internal/model"
)

// Status 是探测结论：沿用领域模型的状态枚举。
type Status = model.ServerStatus

// Checker 对单个 Server 执行一次健康探测。
type Checker interface {
	Check(ctx context.Context, server model.Server) (Status, error)
}

// Monitor 周期巡检全部启用的 Server，并把结果回写存储；同时支持对单个
// Server 的非阻塞即时探活（注册/启用 Server 时由控制面触发，避免等待周期）。
type Monitor struct {
	store    ServerStatusWriter
	checker  Checker
	interval time.Duration
	timeout  time.Duration
	req      chan string // 即时巡检请求队列（serverID），满则丢弃
}

// ServerStatusWriter 是 Monitor 回写健康状态所需的最小存储能力。
type ServerStatusWriter interface {
	ListServers(ctx context.Context) ([]model.Server, error)
	GetServer(ctx context.Context, id string) (*model.Server, error)
	UpdateServer(ctx context.Context, server *model.Server) error
}

// NewMonitor 创建周期巡检器。
func NewMonitor(store ServerStatusWriter, checker Checker, interval, timeout time.Duration) *Monitor {
	return &Monitor{store: store, checker: checker, interval: interval, timeout: timeout, req: make(chan string, 16)}
}

// Run 阻塞执行周期巡检，收到 ctx 取消信号后退出。
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-m.req:
			m.checkServer(ctx, id)
		case <-ticker.C:
			m.checkAll(ctx)
		}
	}
}

// CheckOnce 立即对全部启用 Server 执行一次巡检（启动时调用）。
func (m *Monitor) CheckOnce(ctx context.Context) {
	m.checkAll(ctx)
}

// TriggerCheck 非阻塞请求对指定 Server 立即巡检一次；队列满则丢弃
//（周期巡检仍会兜底，避免拖慢写路径）。
func (m *Monitor) TriggerCheck(serverID string) {
	if serverID == "" {
		return
	}
	select {
	case m.req <- serverID:
	default:
	}
}

// checkServer 对单个 Server 执行一次巡检；不存在或未启用则跳过。
func (m *Monitor) checkServer(ctx context.Context, id string) {
	server, err := m.store.GetServer(ctx, id)
	if err != nil {
		slog.Warn("读取 Server 失败，跳过即时巡检", "server", id, "error", err)
		return
	}
	if !server.Enabled || server.HealthStatus == model.ServerStatusDisabled {
		return
	}
	m.check(ctx, *server)
}

// checkAll 巡检全部启用 Server，失败逐个记录而不中断。
func (m *Monitor) checkAll(ctx context.Context) {
	servers, err := m.store.ListServers(ctx)
	if err != nil {
		slog.Warn("读取 Server 列表失败，跳过本轮巡检", "error", err)
		return
	}
	for _, server := range servers {
		if !server.Enabled || server.HealthStatus == model.ServerStatusDisabled {
			continue
		}
		m.check(ctx, server)
	}
}

// check 对单个 Server 执行带超时探测并回写状态。
func (m *Monitor) check(ctx context.Context, server model.Server) {
	probeCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	status, err := m.checker.Check(probeCtx, server)
	if err != nil {
		status = model.ServerStatusUnhealthy
		slog.Warn("Server 健康检查失败", "server", server.ID, "error", err)
	}
	if status == server.HealthStatus {
		return
	}
	server.HealthStatus = status
	server.UpdatedAt = time.Now()
	if err := m.store.UpdateServer(ctx, &server); err != nil {
		slog.Warn("回写 Server 健康状态失败", "server", server.ID, "error", err)
	}
}
