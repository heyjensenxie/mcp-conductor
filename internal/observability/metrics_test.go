package observability

import (
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
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

// TestMetricsTrendScopeIsolation 验证 Tool/Server/Instance 三个 scope 互不串。
func TestMetricsTrendScopeIsolation(t *testing.T) {
	m := NewMetrics()
	m.Record("mock.search", true, time.Millisecond) // 工具维
	m.Record(ServerDimPrefix+"srv-1", true, time.Millisecond)
	m.Record(InstanceDimPrefix+"srv-1:inst-1", false, time.Millisecond) // 实例维

	if got := m.TrendTool(1)[0].Totals; got != 1 {
		t.Fatalf("TrendTool 应只含工具维（排除 instance:），得到 %d", got)
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
		old:        {total: 5, err: 0},
		now.Unix(): {total: 1, err: 0},
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

// seededRows 构造多维度多分钟的已知桶（M 视为“当前分钟”）。
func seededRows() *Metrics {
	m := NewMetrics()
	m.mu.Lock()
	defer m.mu.Unlock()
	M := time.Now().UTC().Truncate(time.Minute).Unix()
	m.rows = map[string]*metricRow{
		"demo":                             {buckets: map[int64]*trendBucket{M - 1: {total: 4, err: 1}, M: {total: 1}}},
		ServerDimPrefix + "srv-1":          {buckets: map[int64]*trendBucket{M - 1: {total: 4}}},
		InstanceDimPrefix + "srv-1:inst-1": {buckets: map[int64]*trendBucket{M - 1: {total: 4, err: 1}}},
		InstanceDimPrefix + "srv-1:inst-2": {buckets: map[int64]*trendBucket{M: {total: 1}}},
	}
	return m
}

// TestPersistedBucketsSkipsOpenMinute 验证只导出“已闭合”分钟（minute < now），
// 且把 key 正确解析为 scope/server_id/dim_key 三元组。
func TestPersistedBucketsSkipsOpenMinute(t *testing.T) {
	m := seededRows()
	M := time.Now().UTC().Truncate(time.Minute).Unix()
	got := m.PersistedBuckets(time.Unix(M, 0).UTC())

	byKey := make(map[string]model.TrendMinute, len(got))
	for _, b := range got {
		byKey[b.Scope+":"+b.DimKey] = b
	}
	if len(got) != 3 {
		t.Fatalf("应只含 3 个已闭合桶（不含 open 分钟），得到 %d: %+v", len(got), got)
	}
	if b := byKey["tool:demo"]; b.Minute != M-1 || b.Totals != 4 || b.Errors != 1 || b.ServerID != "" {
		t.Fatalf("tool 维桶异常: %+v", b)
	}
	if b := byKey["server:srv-1"]; b.Minute != M-1 || b.ServerID != "srv-1" {
		t.Fatalf("server 维桶异常: %+v", b)
	}
	if b := byKey["instance:inst-1"]; b.Minute != M-1 || b.ServerID != "srv-1" {
		t.Fatalf("instance 维桶异常: %+v", b)
	}
	if _, ok := byKey["instance:inst-2"]; ok {
		t.Fatal("open 分钟（inst-2）不应被持久化")
	}
}

// TestHotTrendScopeFilter 验证热窗读取的 scope/server_id/dim_key 过滤与窗口。
func TestHotTrendScopeFilter(t *testing.T) {
	m := seededRows()
	M := time.Now().UTC().Truncate(time.Minute).Unix()

	// tool 维含两个分钟（含当前 open 分钟）。
	tool := m.HotTrend("tool", "", "", M-1, M)
	if len(tool) != 2 {
		t.Fatalf("tool 热桶应含 2 分钟，得到 %d", len(tool))
	}
	// instance scope + 归属 Server + 单实例。
	inst := m.HotTrend("instance", "srv-1", "inst-1", M-1, M)
	if len(inst) != 1 || inst[0].DimKey != "inst-1" || inst[0].ServerID != "srv-1" {
		t.Fatalf("instance 单实例热桶异常: %+v", inst)
	}
	// instance scope 无归属过滤则含两个实例。
	all := m.HotTrend("instance", "", "", M-1, M)
	if len(all) != 2 {
		t.Fatalf("instance 全实例热桶应 2，得到 %d", len(all))
	}
	// server scope 排除 instance 与 tool。
	server := m.HotTrend("server", "", "", M-1, M)
	if len(server) != 1 || server[0].DimKey != "srv-1" {
		t.Fatalf("server 热桶异常: %+v", server)
	}
	// 越界窗口返回空。
	if got := m.HotTrend("tool", "", "", M+2, M+3); len(got) != 0 {
		t.Fatalf("窗口外应返回空: %+v", got)
	}
}
