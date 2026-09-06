// Package balancer 提供上游实例的负载均衡实现。
//
// 原则：Router 不与具体 Server 强耦合；单实例 Server 也走统一 Balancer 链路。
package balancer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// ErrNoAvailable 表示候选实例中无可用（健康/启用）节点。
var ErrNoAvailable = errors.New("当前没有可用的上游实例")

// defaultFailureCooldown 是单实例调用失败后的默认短期冷却时长：失败后该实例在
// 冷却期内不再入选，避免把随后的调用继续打向疑似故障的实例；冷却结束由健康
// 巡检（快速探活）确认后放回。
const defaultFailureCooldown = 5 * time.Second

// Target 描述一个可选的调用目标（Server 的一个实例）。
// 未来一个逻辑 Server 对应多个实例时，Target 即实例维度；MVP 下端点即实例。
type Target struct {
	ID        string
	ServerID  string
	Endpoint  string
	Transport model.Transport
	Weight    int
	Healthy   bool
}

// LoadBalancer 从候选实例中选择一个用于本次调用。
type LoadBalancer interface {
	Pick(ctx context.Context, targets []Target) (Target, error)
}

// InstanceTracker 允许 Gateway 在“某实例的一次调用失败”后回报该实例，均衡器
// 据此对该实例做短期冷却（失败反馈回路）。仅传输/实例级故障回报；工具执行
// 的业务失败（isError 结果）不回报，避免误摘健康实例。
type InstanceTracker interface {
	ReportFailure(instanceID string)
}

// Option 是 RoundRobin 的可选配置。
type Option func(*RoundRobin)

// WithFailureCooldown 覆盖单实例调用失败后的短期冷却时长（默认 5s）。
func WithFailureCooldown(d time.Duration) Option {
	return func(r *RoundRobin) { r.cooldown = d }
}

// RoundRobin 是循环调度负载均衡器，仅选择健康的实例，并对“最近调用失败的
// 实例”做短期冷却。
//
// 采用原子计数器做轮询，天然支持并发调用安全；冷却状态用互斥锁保护（实例数
// 量有限、操作微秒级）。未来可扩展 Random、LeastConnections、WeightedRoundRobin
// 等策略，接口保持不变。
type RoundRobin struct {
	counter atomic.Uint64

	mu        sync.Mutex
	coolUntil map[string]time.Time // 实例 id → 冷却截止（仅冷却中的实例有键）
	cooldown  time.Duration
	now       func() time.Time // 测试注入时钟
}

// NewRoundRobin 创建循环调度器。
func NewRoundRobin(opts ...Option) *RoundRobin {
	r := &RoundRobin{
		coolUntil: make(map[string]time.Time),
		cooldown:  defaultFailureCooldown,
		now:       time.Now,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Pick 返回下一个健康且不在冷却期的实例；无可用时返回 ErrNoAvailable。
func (r *RoundRobin) Pick(_ context.Context, targets []Target) (Target, error) {
	now := r.now()
	cooled := r.snapshotCooled(now)

	alive := make([]Target, 0, len(targets))
	for _, t := range targets {
		if !t.Healthy || cooled[t.ID] {
			continue
		}
		alive = append(alive, t)
	}
	if len(alive) == 0 {
		return Target{}, ErrNoAvailable
	}
	idx := int(r.counter.Add(1)-1) % len(alive)
	return alive[idx], nil
}

// ReportFailure 记录某实例的一次调用失败：进入短期冷却，冷却期内 Pick 不再返回它。
func (r *RoundRobin) ReportFailure(instanceID string) {
	if instanceID == "" {
		return
	}
	r.mu.Lock()
	r.coolUntil[instanceID] = r.now().Add(r.cooldown)
	r.mu.Unlock()
}

// snapshotCooled 复制当前仍处于冷却期的实例集合；顺带清理已过期的键，防止
// 长时间运行下 map 无限增长（实例本身数量有限，清理成本忽略不计）。
func (r *RoundRobin) snapshotCooled(now time.Time) map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.coolUntil) == 0 {
		return nil
	}
	out := make(map[string]bool, len(r.coolUntil))
	for id, until := range r.coolUntil {
		if until.After(now) {
			out[id] = true
		} else {
			delete(r.coolUntil, id)
		}
	}
	return out
}
