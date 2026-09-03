package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter 基于 Redis 固定窗口计数实现分布式限流。
//
// 用于多实例共享限流状态；Redis 不可用时上层应回退到 MemoryLimiter。
// 设计保留：后续可替换为滑动窗口/令牌桶的 Lua 脚本，无需改动调用方。
type RedisLimiter struct {
	client *redis.Client
	rate   int
	window time.Duration
}

// NewRedisLimiter 创建 Redis 限流器。
func NewRedisLimiter(client *redis.Client, rate int, window time.Duration) *RedisLimiter {
	return &RedisLimiter{client: client, rate: rate, window: window}
}

// Allow 统计窗口内请求数并裁定是否放行。
// Redis 故障时采取"故障放行"策略，避免 Gateway 因依赖抖动整体不可用；
// 是否告警由上层日志/观测负责。
func (r *RedisLimiter) Allow(ctx context.Context, key string) bool {
	if r.rate <= 0 {
		return true
	}
	n, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if n == 1 {
		r.client.Expire(ctx, key, r.window)
	}
	return n <= int64(r.rate)
}
