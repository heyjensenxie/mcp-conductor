// Package health 提供 MCP Server 健康检查抽象与周期巡检。
//
// 探测以"实例"为单位：一个逻辑 Server 下多个实例各自维护健康状态，巡检按
// Server 级联其全部启用实例，再把实例健康聚合成 Server 级状态写回（供列表
// 筛选/展示）。Checker 只关心"探测一次并给出状态结论"，具体探测方式（MCP
// initialize、简单 TCP 连通性等）由实现注入，本包不绑定具体协议。
package health

import (
	"context"
	"log/slog"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// Status 是探测结论：沿用领域模型的状态枚举。
type Status = model.ServerStatus

// Checker 对逻辑 Server 的某个实例执行一次健康探测。
type Checker interface {
	Check(ctx context.Context, server model.Server, instance model.Instance) (Status, error)
}

// Monitor 周期巡检全部启用 Server 的启用实例，并把实例状态与 Server 聚合状态
// 回写存储；同时支持对单个 Server 的非阻塞即时探活（注册/启用时触发，避免等待周期）。
type Monitor struct {
	store    MonitorStore
	checker  Checker
	interval time.Duration
	timeout  time.Duration
	req      chan string // 即时巡检请求队列（serverID），满则丢弃
}

// MonitorStore 是 Monitor 读写健康状态所需的最小存储能力（Server + Instance）。
type MonitorStore interface {
	ListServers(ctx context.Context) ([]model.Server, error)
	GetServer(ctx context.Context, id string) (*model.Server, error)
	UpdateServer(ctx context.Context, server *model.Server) error
	ListInstancesByServer(ctx context.Context, serverID string) ([]model.Instance, error)
	UpdateInstance(ctx context.Context, instance *model.Instance) error
}

// NewMonitor 创建周期巡检器。
func NewMonitor(store MonitorStore, checker Checker, interval, timeout time.Duration) *Monitor {
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
// （周期巡检仍会兜底，避免拖慢写路径）。
func (m *Monitor) TriggerCheck(serverID string) {
	if serverID == "" {
		return
	}
	select {
	case m.req <- serverID:
	default:
	}
}

// checkServer 对单个 Server 的启用实例执行巡检；不存在或未启用则跳过。
func (m *Monitor) checkServer(ctx context.Context, id string) {
	server, err := m.store.GetServer(ctx, id)
	if err != nil {
		slog.Warn("读取 Server 失败，跳过即时巡检", "server", id, "error", err)
		return
	}
	if !server.Enabled || server.HealthStatus == model.ServerStatusDisabled {
		return
	}
	m.probeServer(ctx, *server)
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
		m.probeServer(ctx, server)
	}
}

// probeServer 探测一个 Server 的全部启用实例并回写：逐实例带超时探测（失败
// 标 unhealthy），随后用实例集合重算聚合状态，变化才写回 Server 行。
func (m *Monitor) probeServer(ctx context.Context, server model.Server) {
	instances, err := m.store.ListInstancesByServer(ctx, server.ID)
	if err != nil {
		slog.Warn("读取 Server 实例列表失败，跳过本轮巡检", "server", server.ID, "error", err)
		return
	}
	for i := range instances {
		inst := &instances[i]
		if !inst.Enabled {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, m.timeout)
		status, checkErr := m.checker.Check(probeCtx, server, *inst)
		cancel()
		if checkErr != nil {
			status = model.ServerStatusUnhealthy
			slog.Warn("实例健康检查失败", "server", server.ID, "instance", inst.ID, "endpoint", inst.Endpoint, "error", checkErr)
		}
		if status == inst.HealthStatus {
			continue
		}
		inst.HealthStatus = status
		inst.UpdatedAt = model.Now()
		if err := m.store.UpdateInstance(ctx, inst); err != nil {
			slog.Warn("回写实例健康状态失败", "instance", inst.ID, "error", err)
		}
	}

	agg := model.AggregateServerHealth(server.Enabled, instances)
	if agg == server.HealthStatus {
		return
	}
	server.HealthStatus = agg
	server.UpdatedAt = model.Now()
	if err := m.store.UpdateServer(ctx, &server); err != nil {
		slog.Warn("回写 Server 聚合健康状态失败", "server", server.ID, "error", err)
	}
}
