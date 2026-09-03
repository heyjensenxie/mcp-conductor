package ratelimit

import (
	"context"
	"sync"
	"time"
)

// MemoryLimiter 是基于进程内存的令牌桶限流器。
//
// 每个 key 独立维护令牌桶，按速率补充令牌；不足时拒绝请求。
// 开发模式默认实现，多个实例间不共享状态。
type MemoryLimiter struct {
	mu      sync.Mutex
	rate    float64 // 每秒补充令牌数；<=0 视为不限流
	burst   int
	buckets map[string]*memoryBucket
	now     func() time.Time // 便于测试注入时钟
}

type memoryBucket struct {
	tokens float64
	last   time.Time
}

// NewMemoryLimiter 创建内存限流器。
func NewMemoryLimiter(rate int, burst int) *MemoryLimiter {
	return &MemoryLimiter{
		rate:    float64(rate),
		burst:   burst,
		buckets: make(map[string]*memoryBucket),
		now:     time.Now,
	}
}

// Allow 判断 key 是否放行：不限流直接放行；否则补充令牌后按桶余额裁定。
func (m *MemoryLimiter) Allow(_ context.Context, key string) bool {
	if m.rate <= 0 {
		return true
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.buckets[key]
	if !ok {
		b = &memoryBucket{tokens: float64(m.burst), last: now}
		m.buckets[key] = b
	}
	// 先按时间间隔补充令牌。
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * m.rate
	if b.tokens > float64(m.burst) {
		b.tokens = float64(m.burst)
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
