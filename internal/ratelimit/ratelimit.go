// Package ratelimit 提供流量治理的限流能力。
//
// Limiter 是统一接口，提供 memory 与 redis 两种实现；Redis 不可用时
// 应用以 memory 模式运行，保证无外部依赖也能启动。
//
// 限流维度按 key（调用主体/IP）隔离，且允许调用方按 key 传入各自的
// Limit（例如 AccessKey 的独立配额）；Limit 为零值时实现回退到构造时
// 的全局默认值。
package ratelimit

import "context"

// Limit 描述单个限流维度（key）的放行定额。
// QPS<=0 表示不限流；Burst 是令牌桶容量（内存实现使用，redis 固定窗口忽略）。
type Limit struct {
	QPS   int
	Burst int
}

// Limiter 按维度（key）裁定请求是否放行。
type Limiter interface {
	// Allow 返回是否为该 key 放行本次请求；limit 为该 key 的配额，
	// 零值时使用实现自身的全局默认。
	Allow(ctx context.Context, key string, limit Limit) bool
}

// AllowAll 是永远放行的直通实现，用于限流关闭时的默认值。
type AllowAll struct{}

// Allow 总是放行。
func (AllowAll) Allow(_ context.Context, _ string, _ Limit) bool { return true }
