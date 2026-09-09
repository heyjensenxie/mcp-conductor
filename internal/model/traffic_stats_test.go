package model

import "testing"

// TestLatencyStats_AddAvgPercentile 验证有界延迟聚合的语义：均值精确、分位取
// 所在桶上界、溢出桶取最后一个上界。
func TestLatencyStats_AddAvgPercentile(t *testing.T) {
	var stats LatencyStats
	for _, ms := range []int64{10, 20, 30, 40} {
		stats.Add(ms)
	}
	if stats.Count != 4 || stats.SumMS != 100 {
		t.Fatalf("计数/总和错误: %+v", stats)
	}
	if got := stats.Avg(); got != 25 {
		t.Fatalf("均值应精确为 25，得到 %v", got)
	}
	// p50 目标样本为第 2 个（20ms）→ 落在 (13,20] 桶，返回上界 20。
	if got := stats.Percentile(0.50); got != 20 {
		t.Fatalf("p50 应为桶上界 20，得到 %v", got)
	}
	// p99 目标样本为第 4 个（40ms）→ 落在 (30,40] 桶，返回上界 40。
	if got := stats.Percentile(0.99); got != 40 {
		t.Fatalf("p99 应为桶上界 40，得到 %v", got)
	}
}

// TestLatencyStats_OverflowAndEmpty 验证溢出桶与空样本的取值。
func TestLatencyStats_OverflowAndEmpty(t *testing.T) {
	var empty LatencyStats
	if empty.Avg() != 0 || empty.Percentile(0.95) != 0 {
		t.Fatalf("空样本应返回 0: avg=%v p95=%v", empty.Avg(), empty.Percentile(0.95))
	}

	var stats LatencyStats
	stats.Add(9000) // 超出最后一个上界（5000）
	if stats.Buckets[len(LatencyBoundsMS)] != 1 {
		t.Fatalf("超界样本应落入溢出桶: %+v", stats.Buckets)
	}
	if got := stats.Percentile(0.95); got != float64(LatencyBoundsMS[len(LatencyBoundsMS)-1]) {
		t.Fatalf("溢出桶分位应取最后一个上界，得到 %v", got)
	}
	// 负数样本归一为 0（防御性，避免脏数据把直方图撑坏）。
	var negative LatencyStats
	negative.Add(-5)
	if negative.SumMS != 0 || negative.Buckets[0] != 1 {
		t.Fatalf("负数样本应归一到 0: %+v", negative)
	}
}

// TestLatencyStats_Merge 验证合并（分组汇总为整体）后计数与分位一致。
func TestLatencyStats_Merge(t *testing.T) {
	var a, b LatencyStats
	a.Add(5)
	a.Add(15)
	b.Add(100)
	a.Merge(b)
	if a.Count != 3 || a.SumMS != 120 {
		t.Fatalf("合并后计数/总和错误: %+v", a)
	}
	if got := a.Avg(); got != 40 {
		t.Fatalf("合并后均值应为 40，得到 %v", got)
	}
}

// TestTrafficWindowStats_SuccessRate 验证成功率计算（空窗口为 0）。
func TestTrafficWindowStats_SuccessRate(t *testing.T) {
	if got := (TrafficWindowStats{}).SuccessRate(); got != 0 {
		t.Fatalf("空窗口成功率应为 0，得到 %v", got)
	}
	stats := TrafficWindowStats{Totals: 4, Success: 3}
	if got := stats.SuccessRate(); got != 0.75 {
		t.Fatalf("成功率应为 0.75，得到 %v", got)
	}
}
