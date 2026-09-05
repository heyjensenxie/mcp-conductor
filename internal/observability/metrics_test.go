package observability

import (
	"testing"
	"time"
)

// TestMetricsTrendToolAggregates 验证工具维度按分钟聚合 totals/errors。
func TestMetricsTrendToolAggregates(t *testing.T) {
	m := NewMetrics()
	now := time.Now().UTC().Truncate(time.Minute)
	m.Record("mock.search", true, time.Millisecond)
	m.Record("mock.search", false, 2*time.Millisecond)
	m.Record("mock.detail", true, time.Millisecond)

	points := m.TrendTool(1)
	if len(points) != 1 {
		t.Fatalf("TrendTool(1) 应返回 1 个点，得到 %d", len(points))
	}
	if points[0].Totals != 3 || points[0].Errors != 1 {
		t.Fatalf("工具维聚合错误: %+v", points[0])
	}
	if points[0].TS != now.Unix() {
		t.Fatalf("时间戳应对齐分钟: %d != %d", points[0].TS, now.Unix())
	}
}

// TestMetricsTrendScopeIsolation 验证 Tool/Server 两个 scope 互不串。
func TestMetricsTrendScopeIsolation(t *testing.T) {
	m := NewMetrics()
	m.Record("mock.search", true, time.Millisecond) // 工具维
	m.Record(ServerDimPrefix+"srv-1", true, time.Millisecond)

	if got := m.TrendTool(1)[0].Totals; got != 1 {
		t.Fatalf("TrendTool 应只含工具维，得到 %d", got)
	}
	if got := m.TrendServer(1)[0].Totals; got != 1 {
		t.Fatalf("TrendServer 应只含 server: 前缀，得到 %d", got)
	}
}

// TestMetricsTrendWindowLength 验证窗口长度与补零。
func TestMetricsTrendWindowLength(t *testing.T) {
	m := NewMetrics()
	m.Record("mock.search", true, time.Millisecond)
	points := m.TrendTool(5)
	if len(points) != 5 {
		t.Fatalf("TrendTool(5) 长度应为 5，得到 %d", len(points))
	}
	for i := 1; i < len(points); i++ {
		if points[i].TS-points[i-1].TS != minuteSec {
			t.Fatalf("点应逐分钟递增: %+v", points)
		}
	}
	total := int64(0)
	for _, p := range points {
		total += p.Totals
	}
	if total != 1 {
		t.Fatalf("5 分钟内应聚合 1 次调用，得到 %d", total)
	}
}

// TestMetricsTrendPrunesOldBuckets 验证 Record 会修剪超出保留窗口的旧分钟桶。
func TestMetricsTrendPrunesOldBuckets(t *testing.T) {
	m := NewMetrics()
	now := time.Now().UTC().Truncate(time.Minute)
	// 手工塞入 130 分钟前的旧桶（超出 120 窗口）。
	old := now.Unix() - (trendKeepMinutes+10)*minuteSec
	m.mu.Lock()
	m.rows["mock.search"] = &metricRow{buckets: map[int64]*trendBucket{
		old:             {total: 5, err: 0},
		now.Unix():      {total: 1, err: 0},
	}}
	m.mu.Unlock()

	// 触发修剪。
	m.Record("mock.search", true, time.Millisecond)

	m.mu.Lock()
	row := m.rows["mock.search"]
	if _, ok := row.buckets[old]; ok {
		m.mu.Unlock()
		t.Fatal("旧分钟桶应被修剪")
	}
	m.mu.Unlock()

	if got := m.TrendTool(1)[0].Totals; got != 2 {
		t.Fatalf("当前分钟应累计为 2（初始 1 + 新 1），得到 %d", got)
	}
}
