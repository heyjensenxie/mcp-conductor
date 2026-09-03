// Package ratelimit 提供流量治理的限流能力。
//
// Limiter 是统一接口，提供 memory 与 redis 两种实现；Redis 不可用时
// 应用以 memory 模式运行，保证无外部依赖也能启动。
package ratelimit

import "context"

// Limiter 按维度（key）裁定请求是否放行。
type Limiter interface {
	// Allow 返回是否为该 key 放行本次请求。
	Allow(ctx context.Context, key string) bool
}

// AllowAll 是永不放行的直通实现，用于限流关闭时的默认值。
type AllowAll struct{}

// Allow 总是放行。
func (AllowAll) Allow(_ context.Context, _ string) bool { return true }
