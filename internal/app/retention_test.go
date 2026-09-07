package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// stubTrafficStore 供清理逻辑测试：记录每次 DeleteTrafficBefore 收到的 cutoff；
// give=-1 表示“始终有可删行”（返回 min(limit, 模拟大存量)）；0<give 自定义批次。
type stubTrafficStore struct {
	mu    sync.Mutex
	calls []time.Time
	give  int64
	err   error
}

func (s *stubTrafficStore) DeleteTrafficBefore(_ context.Context, before time.Time, limit int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, before)
	if s.err != nil {
		return 0, s.err
	}
	n := s.give
	if n == -1 || n > limit {
		n = limit
	}
	return n, nil
}

func (s *stubTrafficStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (*stubTrafficStore) AppendTraffic(context.Context, model.TrafficSample) error { return nil }
func (*stubTrafficStore) GetTraffic(context.Context, int64) (*model.TrafficSample, error) {
	return nil, errors.New("stub")
}
func (*stubTrafficStore) RecentTraffic(context.Context, int) ([]model.TrafficSample, error) {
	return nil, nil
}
func (*stubTrafficStore) RecentTrafficByServer(context.Context, string, int) ([]model.TrafficSample, error) {
	return nil, nil
}

var _ storage.TrafficStore = (*stubTrafficStore)(nil)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("条件未在超时内满足")
}

// TestRunTrafficRetentionDisabled 验证 retentionDays<=0 关闭自动清理：不发起任何删除。
func TestRunTrafficRetentionDisabled(t *testing.T) {
	s := &stubTrafficStore{}
	runTrafficRetention(context.Background(), s, 0)
	if s.count() != 0 {
		t.Fatalf("关闭时不应有清理调用，calls=%d", s.count())
	}
}

// TestRunTrafficRetentionStartupPurge 验证启动先清一轮且 cutoff 按保留天数计算，
// 分块返回不足一批时立即收敛（give<chunk 一轮即停），ctx 取消后调度循环退出。
func TestRunTrafficRetentionStartupPurge(t *testing.T) {
	s := &stubTrafficStore{give: 10}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runTrafficRetention(ctx, s, 7)
		close(done)
	}()

	waitFor(t, func() bool { return s.count() >= 1 })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 runTrafficRetention 未退出")
	}

	if s.count() != 1 {
		t.Fatalf("启动清理应仅一轮（give<chunk），calls=%d", s.count())
	}
	want := time.Now().Add(-7 * 24 * time.Hour)
	got := s.calls[0]
	if d := got.Sub(want); d < -2*time.Minute || d > 2*time.Minute {
		t.Fatalf("cutoff 偏差过大: got=%v want≈%v (diff=%v)", got, want, d)
	}
}

// TestPurgeStopsWhenFewerThanChunk 验证剩余不足一批时停止（一刀收敛正常场景）。
func TestPurgeStopsWhenFewerThanChunk(t *testing.T) {
	s := &stubTrafficStore{give: 10}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	if n := purgeTrafficBefore(context.Background(), s, cutoff, 100, 10000); n != 10 {
		t.Fatalf("应删 10 行即停，得到 %d", n)
	}
	if s.count() != 1 || !s.calls[0].Equal(cutoff) {
		t.Fatalf("应仅一次调用且 cutoff 透传，calls=%d first=%v", s.count(), s.calls[0])
	}
}

// TestPurgeBoundedByMaxPerRun 验证存量庞大时分块受单轮上限约束（防止一次清空拖垮整点）。
func TestPurgeBoundedByMaxPerRun(t *testing.T) {
	s := &stubTrafficStore{give: -1} // 始终有可删行
	if n := purgeTrafficBefore(context.Background(), s, time.Now(), 100, 350); n != 400 {
		t.Fatalf("应删满 350 上限（整批凑 400），得到 %d", n)
	}
	if s.count() != 4 {
		t.Fatalf("分块应 4 批（100*4=400≥350），calls=%d", s.count())
	}
}

// TestPurgeStopsOnError 验证删除失败写告警并中止（观测流水清理失败不阻断数据面）。
func TestPurgeStopsOnError(t *testing.T) {
	s := &stubTrafficStore{err: errors.New("db down")}
	if n := purgeTrafficBefore(context.Background(), s, time.Now(), 100, 10000); n != 0 {
		t.Fatalf("失败应中止且返回 0，得到 %d", n)
	}
	if s.count() != 1 {
		t.Fatalf("失败后不应继续分块，calls=%d", s.count())
	}
}

// TestPurgeStopsOnCancel 验证 ctx 取消立即中止，不发起删除。
func TestPurgeStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &stubTrafficStore{give: -1}
	if n := purgeTrafficBefore(ctx, s, time.Now(), 100, 10000); n != 0 {
		t.Fatalf("ctx 取消应立即中止，得到 %d", n)
	}
	if s.count() != 0 {
		t.Fatalf("ctx 取消后不应有删除调用，calls=%d", s.count())
	}
}
