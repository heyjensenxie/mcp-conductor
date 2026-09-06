package ratelimit

import (
	"context"
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLimiter 基于 Redis ZSET + Lua 实现「N 秒滑动窗口」分布式限流。
//
// 每个 key 一个 Sorted Set（score=请求时刻 ms）；单条 Lua 原子地：修剪落在窗口
// 之外（<= now-W）的成员 → 窗口内计数达容量即拒绝 → 否则加入本次请求并刷新 TTL。
// 多实例共享同一 key，计数一致。Redis 不可用时上层应回退到 MemoryLimiter。
type RedisLimiter struct {
	client *redis.Client
	rate   int
	window time.Duration
	seq    atomic.Uint64 // 同毫秒去重的进程内自增
	token  uint32        // 实例随机标识，避免多实例 member 撞车
}

// keyPrefix 限定限流 key 的命名空间，避免与 Redis 中其它用途的 key 冲突。
const keyPrefix = "rl:"

// NewRedisLimiter 创建 Redis 滑动窗口限流器。
func NewRedisLimiter(client *redis.Client, rate int, window time.Duration) *RedisLimiter {
	return &RedisLimiter{
		client: client,
		rate:   rate,
		window: window,
		token:  rand.Uint32(),
	}
}

// slideScript 是滑动窗口的原子脚本：
//   - ZREMRANGEBYSCORE key '-inf' (now-win)：移除 score <= now-win 的成员
//     （'(' 为排他上限），使窗口精确为 (now-win, now]；
//   - ZCARD ≥ cap → 拒绝（返回 0，不记录本次）；
//   - 否则 ZADD now + 唯一 member，并 PEXPIRE(win+2000) 让空闲 key 自清理。
const slideScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local win = tonumber(ARGV[2])
local cap = tonumber(ARGV[3])
local member = ARGV[4]
redis.call('ZREMRANGEBYSCORE', key, '-inf', '(' .. (now - win))
local n = redis.call('ZCARD', key)
if n >= cap then
  return 0
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, win + 2000)
return 1
`

// Allow 统计窗口内请求数并裁定是否放行；qps/窗长来自 limit（0 回退构造默认）。
// Redis 故障时采取"故障放行"策略，避免 Gateway 因依赖抖动整体不可用；是否告警由
// 上层负责（建议节流/单次，避免故障期刷屏）。
func (r *RedisLimiter) Allow(ctx context.Context, key string, limit Limit) bool {
	qps := limit.QPS
	if qps <= 0 {
		qps = r.rate
	}
	if qps <= 0 {
		return true
	}
	window := r.window
	if limit.WindowSec > 0 {
		window = time.Duration(limit.WindowSec) * time.Second
	}
	if window <= 0 {
		window = time.Second
	}
	capacity := qps * int(window/time.Second)
	if capacity < 1 {
		capacity = 1
	}
	winMs := window.Milliseconds()
	nowMs := time.Now().UnixMilli()
	member := fmt.Sprintf("%d-%d-%08x", nowMs, r.seq.Add(1), r.token)

	rk := keyPrefix + key
	n, err := r.client.Eval(ctx, slideScript, []string{rk},
		nowMs, winMs, capacity, member).Int()
	if err != nil {
		return true // fail-open
	}
	return n == 1
}
