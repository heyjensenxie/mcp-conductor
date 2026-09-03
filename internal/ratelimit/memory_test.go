package ratelimit

import (
	"context"
	"testing"
	"time"
)

// fixedClock 由测试注入的静态时钟。
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

func TestMemoryLimiter_BurstThenRejectAndRefill(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(1, 3) // 每秒 1 个，突发 3
	limiter.now = clock.now

	ctx := context.Background()
	// 突发容量内全部放行。
	for i := 0; i < 3; i++ {
		if !limiter.Allow(ctx, "k") {
			t.Fatalf("突发第 %d 次应放行", i+1)
		}
	}
	if limiter.Allow(ctx, "k") {
		t.Fatal("超出突发容量应拒绝")
	}

	// 时间前进 1 秒，应恢复 1 个令牌。
	clock.t = clock.t.Add(time.Second)
	if !limiter.Allow(ctx, "k") {
		t.Fatal("经过一个周期后应补充令牌放行")
	}
	if limiter.Allow(ctx, "k") {
		t.Fatal("再次请求应拒绝（仅补充 1 个）")
	}
}

func TestMemoryLimiter_ZeroRateAllowsAll(t *testing.T) {
	limiter := NewMemoryLimiter(0, 0) // 关闭限流
	if !limiter.Allow(context.Background(), "k") {
		t.Fatal("rate<=0 时应始终放行")
	}
}

func TestMemoryLimiter_KeysAreIndependent(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(1, 1)
	limiter.now = clock.now

	ctx := context.Background()
	if !limiter.Allow(ctx, "a") {
		t.Fatal("key a 应放行")
	}
	if limiter.Allow(ctx, "a") {
		t.Fatal("key a 的桶已空，应拒绝")
	}
	if !limiter.Allow(ctx, "b") {
		t.Fatal("key b 独立计数，应放行")
	}
}
