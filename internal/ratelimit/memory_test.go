package ratelimit

import (
	"context"
	"testing"
	"time"
)

// fixedClock 由测试注入的静态时钟。
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

// TestMemoryLimiter_WindowCapacity 验证滑窗容量：容量=qps×窗长内放行、达到即拒、
// 恰好经过一个窗长后旧记录过期恢复（边界为 (now-W, now]）。
func TestMemoryLimiter_WindowCapacity(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(1, time.Second) // 容量 = 1×1s = 1
	limiter.now = clock.now

	ctx := context.Background()
	if !limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("窗口内第 1 次应放行")
	}
	if limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("窗口容量 1 时第 2 次应拒绝")
	}
	// 未满一个窗长（999ms）仍应拒绝。
	clock.t = clock.t.Add(999 * time.Millisecond)
	if limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("未满窗长应继续拒绝")
	}
	// 恰好满一个窗长（+1ms 至 1000ms）：旧记录正好落在 (now-W, now] 之外，恢复放行。
	clock.t = clock.t.Add(time.Millisecond)
	if !limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("经过一个窗长后应恢复放行")
	}
}

// TestMemoryLimiter_WindowOverride 验证 limit.WindowSec 覆盖构造默认窗长。
func TestMemoryLimiter_WindowOverride(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(1, time.Second) // 构造默认容量 1
	limiter.now = clock.now

	ctx := context.Background()
	if !limiter.Allow(ctx, "k", Limit{QPS: 1, WindowSec: 2}) {
		t.Fatal("QPS=1/WindowSec=2 容量应为 2, 第 1 次应放行")
	}
	if !limiter.Allow(ctx, "k", Limit{QPS: 1, WindowSec: 2}) {
		t.Fatal("容量 2 第 2 次应放行")
	}
	if limiter.Allow(ctx, "k", Limit{QPS: 1, WindowSec: 2}) {
		t.Fatal("容量 2 第 3 次应拒绝")
	}
}

// TestMemoryLimiter_SlidingBoundary 验证滑动性：窗口内按时间推进，仅过期最旧记录
// 逐个释放（而非整窗清零）。
func TestMemoryLimiter_SlidingBoundary(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(2, time.Second) // 容量 2
	limiter.now = clock.now

	ctx := context.Background()
	if !limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("第 1 次应放行")
	}
	clock.t = clock.t.Add(500 * time.Millisecond)
	if !limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("第 2 次应放行")
	}
	if limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("窗口内 2 条应满, 应拒绝")
	}

	// 推进到最早一条（t0）恰好过期：仅释放 1 个名额，仍应有 1 条在窗内 → 可再放 1 条。
	clock.t = clock.t.Add(600 * time.Millisecond) // 现在 t0+1100ms
	if !limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("最早记录过期后应恢复 1 个名额")
	}
	if limiter.Allow(ctx, "k", Limit{}) {
		t.Fatal("仍剩 1 条在窗内 + 新 1 条 = 满, 应拒绝")
	}
}

func TestMemoryLimiter_ZeroRateAllowsAll(t *testing.T) {
	limiter := NewMemoryLimiter(0, time.Second) // 关闭限流
	if !limiter.Allow(context.Background(), "k", Limit{}) {
		t.Fatal("qps<=0 时应始终放行")
	}
}

func TestMemoryLimiter_KeysAreIndependent(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(1, time.Second) // 容量 1
	limiter.now = clock.now

	ctx := context.Background()
	if !limiter.Allow(ctx, "a", Limit{}) {
		t.Fatal("key a 应放行")
	}
	if limiter.Allow(ctx, "a", Limit{}) {
		t.Fatal("key a 的窗口已满, 应拒绝")
	}
	if !limiter.Allow(ctx, "b", Limit{}) {
		t.Fatal("key b 独立计数, 应放行")
	}
}

// TestMemoryLimiter_PerKeyLimit 验证按 key 传入的配额独立生效。
func TestMemoryLimiter_PerKeyLimit(t *testing.T) {
	clock := &fixedClock{t: time.Unix(0, 0)}
	limiter := NewMemoryLimiter(10, time.Second) // 全局默认宽松，per-key 收紧
	limiter.now = clock.now

	ctx := context.Background()
	// key a 独立配额 QPS=2/WindowSec=1：两次放行后拒绝。
	if !limiter.Allow(ctx, "a", Limit{QPS: 2, WindowSec: 1}) {
		t.Fatal("per-key 第 1 次应放行")
	}
	if !limiter.Allow(ctx, "a", Limit{QPS: 2, WindowSec: 1}) {
		t.Fatal("per-key 第 2 次应放行")
	}
	if limiter.Allow(ctx, "a", Limit{QPS: 2, WindowSec: 1}) {
		t.Fatal("per-key 第 3 次应拒绝")
	}
	// key b 无配额 → 回退全局默认（QPS=10），应放行。
	if !limiter.Allow(ctx, "b", Limit{}) {
		t.Fatal("无配额 key 应回退全局默认放行")
	}
}
