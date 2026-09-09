package gateway

import (
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
)

// trend_bucket.go —— 长窗口趋势的**服务端降采样**。
//
// 3 天/7 天窗口按分钟返回会是 4320/10080 个点：响应体大、图表渲染重，而且绝大多数
// 分钟是空的。这里把分钟点按“整齐”的桶宽合并（1/5/10/…/1440 分钟），保证点数不超过
// maxTrendPoints，同时保留计数与延迟直方图的语义（桶内求和、分位重新由合并后的直方图
// 计算）。桶按绝对分钟对齐，跨请求稳定。

// maxTrendPoints 是趋势/分钟桶响应的目标点位数上限。
const maxTrendPoints = 500

// trendBucketSteps 是可选的桶宽（分钟），从细到粗。
var trendBucketSteps = []int{1, 5, 10, 15, 30, 60, 120, 180, 360, 720, 1440}

// trendBucketMinutes 返回把 minutes 个分钟点降到 <= maxTrendPoints 的最小整齐桶宽。
func trendBucketMinutes(minutes int) int {
	if minutes <= 0 {
		return 1
	}
	for _, step := range trendBucketSteps {
		if (minutes+step-1)/step <= maxTrendPoints {
			return step
		}
	}
	return trendBucketSteps[len(trendBucketSteps)-1]
}

// alignMinute 把分钟起点对齐到 bucket 分钟桶。
func alignMinute(minute int64, bucketMinutes int) int64 {
	step := int64(bucketMinutes) * 60
	if step <= 0 {
		return minute
	}
	return minute - minute%step
}

// bucketTrendSeries 把连续的趋势序列按 bucketMinutes 合并（求和），时间戳取桶起点。
func bucketTrendSeries(series []observability.TrendPoint, bucketMinutes int) []observability.TrendPoint {
	if bucketMinutes <= 1 || len(series) == 0 {
		return series
	}
	out := make([]observability.TrendPoint, 0, len(series)/bucketMinutes+1)
	index := make(map[int64]int, len(out))
	for _, p := range series {
		key := alignMinute(p.TS, bucketMinutes)
		i, ok := index[key]
		if !ok {
			out = append(out, observability.TrendPoint{TS: key})
			i = len(out) - 1
			index[key] = i
		}
		out[i].Totals += p.Totals
		out[i].Errors += p.Errors
	}
	return out
}

// fillWindowMinutes 把窗口内的分钟桶按 bucketMinutes 合并为**连续**序列（缺桶补零），
// 覆盖 [from, to] 所在的桶区间。延迟分位由桶内合并后的直方图重新计算。
func fillWindowMinutes(stats []model.TrafficMinuteStats, from, to time.Time, bucketMinutes int) []metricsWindowMinute {
	if bucketMinutes <= 0 {
		bucketMinutes = 1
	}
	step := int64(bucketMinutes) * 60
	first := alignMinute(from.Unix(), bucketMinutes)
	last := alignMinute(to.Unix(), bucketMinutes)

	acc := make(map[int64]*model.TrafficMinuteStats, len(stats))
	for i := range stats {
		m := stats[i]
		key := alignMinute(m.Minute, bucketMinutes)
		bucket, ok := acc[key]
		if !ok {
			bucket = &model.TrafficMinuteStats{Minute: key}
			acc[key] = bucket
		}
		bucket.Totals += m.Totals
		bucket.Success += m.Success
		bucket.Errors += m.Errors
		bucket.Latency.Merge(m.Latency)
	}

	if last < first {
		return nil
	}
	out := make([]metricsWindowMinute, 0, (last-first)/step+1)
	for ts := first; ts <= last; ts += step {
		bucket := acc[ts]
		point := metricsWindowMinute{Minute: ts}
		if bucket != nil {
			point.Totals = bucket.Totals
			point.Success = bucket.Success
			point.Errors = bucket.Errors
			point.Avg = round1(bucket.Latency.Avg())
			point.P95 = bucket.Latency.Percentile(0.95)
		}
		out = append(out, point)
	}
	return out
}
