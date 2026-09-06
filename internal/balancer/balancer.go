// Package balancer 提供上游实例的负载均衡实现。
//
// 原则：Router 不与具体 Server 强耦合；单实例 Server 也走统一 Balancer 链路。
package balancer

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// ErrNoAvailable 表示候选实例中无可用（健康/启用）节点。
var ErrNoAvailable = errors.New("当前没有可用的上游实例")

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

// RoundRobin 是循环调度负载均衡器，仅选择健康的实例。
//
// 采用原子计数器，天然支持并发调用安全。未来可扩展 Random、
// LeastConnections、WeightedRoundRobin、HealthAware 等策略，接口保持不变。
type RoundRobin struct {
	counter atomic.Uint64
}

// NewRoundRobin 创建循环调度器。
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

// Pick 返回下一个健康实例；无健康实例时返回 ErrNoAvailable。
func (r *RoundRobin) Pick(_ context.Context, targets []Target) (Target, error) {
	alive := make([]Target, 0, len(targets))
	for _, t := range targets {
		if t.Healthy {
			alive = append(alive, t)
		}
	}
	if len(alive) == 0 {
		return Target{}, ErrNoAvailable
	}
	idx := int(r.counter.Add(1)-1) % len(alive)
	return alive[idx], nil
}
