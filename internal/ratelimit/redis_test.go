package ratelimit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestRedisLimiter_SlidingWindow 是 Redis 滑动窗口的集成测试：
// 需真实 Redis（设 CONDUCTOR_REDIS_TEST_ADDR，如 127.0.0.1:6379），否则跳过。
// 验证：窗口容量放行、超限拒绝、key 独立、window_seconds 覆盖。
func TestRedisLimiter_SlidingWindow(t *testing.T) {
	addr := os.Getenv("CONDUCTOR_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("未设置 CONDUCTOR_REDIS_TEST_ADDR，跳过 Redis 集成测试")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis 不可达，跳过: %v", err)
	}
	defer rdb.Close()
	_ = rdb.FlushDB(ctx) // 测试隔离

	lim := NewRedisLimiter(rdb, 2, time.Second) // 容量 = 2×1s = 2
	if !lim.Allow(ctx, "it:k", Limit{}) {
		t.Fatal("第 1 次应放行")
	}
	if !lim.Allow(ctx, "it:k", Limit{}) {
		t.Fatal("第 2 次应放行")
	}
	if lim.Allow(ctx, "it:k", Limit{}) {
		t.Fatal("容量 2 时第 3 次应拒绝")
	}
	if !lim.Allow(ctx, "it:other", Limit{}) {
		t.Fatal("key 应独立计数")
	}

	// window_seconds 覆盖：QPS=1/窗长 2s → 容量 2。
	if !lim.Allow(ctx, "it:w", Limit{QPS: 1, WindowSec: 2}) {
		t.Fatal("QPS=1/WindowSec=2 第 1 次应放行")
	}
	if !lim.Allow(ctx, "it:w", Limit{QPS: 1, WindowSec: 2}) {
		t.Fatal("QPS=1/WindowSec=2 第 2 次应放行")
	}
	if lim.Allow(ctx, "it:w", Limit{QPS: 1, WindowSec: 2}) {
		t.Fatal("QPS=1/WindowSec=2 第 3 次应拒绝")
	}
}
