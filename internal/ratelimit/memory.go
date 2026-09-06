package ratelimit

import (
	"context"
	"sync"
	"time"
)

// MemoryLimiter 是基于进程内存的「N 秒滑动窗口」限流器。
//
// 每个 key 维护最近放行请求的毫秒时间戳 FIFO（长度 ≤ 容量 = qps×window 秒）。
// Allow 时先修剪落在窗口 (now-W, now] 之外的旧记录；若窗口内计数已达容量则
// 拒绝，否则记录本次时间戳放行。多实例间不共享状态，开发模式默认实现。
// 构造参数作为全局默认；Allow 可传入按 key 的 Limit 覆盖（含 WindowSec）。
type MemoryLimiter struct {
	mu     sync.Mutex
	rate   int // 每秒放行数（默认，<=0 视为不限流）
	window time.Duration
	keys   map[string][]int64 // key → 窗口内放行时间戳（ms，升序，有界）
	now    func() time.Time   // 便于测试注入时钟
}

// NewMemoryLimiter 创建内存滑动窗口限流器。
// window 为默认窗口长度（秒级，内部取整秒）。
func NewMemoryLimiter(rate int, window time.Duration) *MemoryLimiter {
	return &MemoryLimiter{
		rate:   rate,
		window: window,
		keys:   make(map[string][]int64),
		now:    time.Now,
	}
}

// effective 解析某次 Allow 的有效速率与窗口：limit 提供配额，零值回退构造默认。
func (m *MemoryLimiter) effective(limit Limit) (qps int, window time.Duration) {
	qps = limit.QPS
	if qps <= 0 {
		qps = m.rate
	}
	window = m.window
	if limit.WindowSec > 0 {
		window = time.Duration(limit.WindowSec) * time.Second
	}
	if window <= 0 {
		window = time.Second
	}
	return qps, window
}

// Allow 判断 key 是否放行：修剪过期记录后，窗口内计数达容量即拒绝。
func (m *MemoryLimiter) Allow(_ context.Context, key string, limit Limit) bool {
	qps, window := m.effective(limit)
	if qps <= 0 {
		return true
	}
	capacity := qps * int(window/time.Second)
	if capacity < 1 {
		capacity = 1
	}

	nowMs := m.now().UnixMilli()
	cutoff := nowMs - window.Milliseconds() // 窗口 = (now-W, now]：修剪 <= cutoff

	m.mu.Lock()
	defer m.mu.Unlock()

	arr := m.keys[key]
	start := 0
	for start < len(arr) && arr[start] <= cutoff {
		start++
	}
	arr = arr[start:]
	if len(arr) >= capacity {
		m.keys[key] = arr // 已修剪，避免下次重复扫描
		return false
	}
	arr = append(arr, nowMs)
	m.keys[key] = arr
	return true
}
