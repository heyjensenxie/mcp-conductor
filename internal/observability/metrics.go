// Package observability 汇集调用观测：指标聚合与调用日志（Audit）。
//
// 默认不记录完整 Tool 参数/返回值（可能含隐私），由配置的表体记录开关与
// 采样率控制；未来在此扩展 Masking / Sampling 的精细配置。
package observability

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// metricsCap 每个维度保留的最大延迟样本数，防止无限增长。
const metricsCap = 4096

// trendKeepMinutes 指标趋势保留的分钟窗口（分钟桶，重启即清）。
const trendKeepMinutes = 120

// minuteSec 一分钟的秒数。
const minuteSec = 60

// ServerDimPrefix 是 metrics 中按 Server 聚合的维度前缀（供 Servers 列表
// 展示每个 Server 的 Requests / P95）。默认 /metrics 不返回此类行，避免
// 污染工具维语义；需 ?scope=server 时才单独返回。
const ServerDimPrefix = "server:"

// InstanceDimPrefix 是 metrics 中按「Server 的某个具体实例」聚合的维度前缀，
// 键格式 instance:<serverID>:<instanceID>。与 server: 维同一次调用并存：server:
// 承载逻辑 Server 聚合（eval runtime 等精确等值消费者依赖），instance: 用于把
// 观测下沉到多实例中的单个实例。默认 /metrics 的 tool 语义不返回此类行，
// 需 ?scope=instance（可配 &server_id=）时才单独返回。
const InstanceDimPrefix = "instance:"

// trendBucket 是单个分钟桶的调用计数。
type trendBucket struct {
	total int64
	err   int64
}

// metricRow 是单个维度（工具/Server 等）的聚合指标。
type metricRow struct {
	Totals  int64
	Success int64
	Errors  int64
	latency []float64 // 单位 ms，达到上限后仅保留最新

	// buckets 是按分钟（UTC Unix）计数的调用时序，供趋势端点读取。
	buckets map[int64]*trendBucket
}

// Metrics 是并发安全的进程内指标聚合器。
//
// 覆盖 PRD 要求的请求量、成功率、错误率、P50/P95/P99 等指标；
// 数据保留在内存，可被控制面 /metrics 读取。
type Metrics struct {
	mu   sync.Mutex
	rows map[string]*metricRow
}

// NewMetrics 创建空指标聚合器。
func NewMetrics() *Metrics {
	return &Metrics{rows: make(map[string]*metricRow)}
}

// Record 记录一次调用结果到指定维度。
func (m *Metrics) Record(key string, ok bool, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, exists := m.rows[key]
	if !exists {
		row = &metricRow{}
		m.rows[key] = row
	}
	row.Totals++
	if ok {
		row.Success++
	} else {
		row.Errors++
	}
	row.latency = append(row.latency, float64(latency.Milliseconds()))
	if len(row.latency) > metricsCap {
		row.latency = row.latency[len(row.latency)-metricsCap:]
	}

	// 分钟级时序桶（UTC），并修剪超出保留窗口的旧桶。
	now := time.Now().UTC()
	minute := now.Truncate(time.Minute).Unix()
	if row.buckets == nil {
		row.buckets = make(map[int64]*trendBucket)
	}
	b := row.buckets[minute]
	if b == nil {
		b = &trendBucket{}
		row.buckets[minute] = b
	}
	b.total++
	if !ok {
		b.err++
	}
	cutoff := minute - (trendKeepMinutes - 1)
	for t := range row.buckets {
		if t < cutoff {
			delete(row.buckets, t)
		}
	}
}

// TrendPoint 是某个分钟窗口的趋势采样（真时序，按分钟聚合调用量与失败数）。
type TrendPoint struct {
	TS     int64 `json:"ts"`     // 分钟级 UTC Unix
	Totals int64 `json:"totals"` // 该分钟调用量
	Errors int64 `json:"errors"` // 该分钟失败数
}

// TrendTool 返回工具维度（非 server:/instance: 前缀）近 minutes 分钟的聚合时序。
func (m *Metrics) TrendTool(minutes int) []TrendPoint {
	return m.trendScope(time.Now().UTC(), minutes, func(key string) bool {
		return !strings.HasPrefix(key, ServerDimPrefix) && !strings.HasPrefix(key, InstanceDimPrefix)
	})
}

// TrendServer 返回按 Server 聚合（server: 前缀）近 minutes 分钟的时序。
func (m *Metrics) TrendServer(minutes int) []TrendPoint {
	return m.trendScope(time.Now().UTC(), minutes, func(key string) bool {
		return strings.HasPrefix(key, ServerDimPrefix)
	})
}

// trendScope 汇总命中维度的分钟桶为连续时序（缺数据补 0），窗口按 now 对齐。
func (m *Metrics) trendScope(now time.Time, minutes int, match func(key string) bool) []TrendPoint {
	m.mu.Lock()
	defer m.mu.Unlock()

	if minutes <= 0 {
		minutes = trendKeepMinutes
	}
	if minutes > trendKeepMinutes {
		minutes = trendKeepMinutes
	}
	start := now.Truncate(time.Minute).Unix() - int64(minutes-1)*minuteSec
	points := make([]TrendPoint, minutes)
	for i := range points {
		points[i].TS = start + int64(i)*minuteSec
	}
	for key, row := range m.rows {
		if !match(key) {
			continue
		}
		for i := range points {
			if b, ok := row.buckets[points[i].TS]; ok {
				points[i].Totals += b.total
				points[i].Errors += b.err
			}
		}
	}
	return points
}

// Snapshot 是某个维度当前指标的稳定快照。
type Snapshot struct {
	Key         string  `json:"key"`
	Totals      int64   `json:"totals"`
	Success     int64   `json:"success"`
	Errors      int64   `json:"errors"`
	SuccessRate float64 `json:"success_rate"`
	P50         float64 `json:"p50"`
	P95         float64 `json:"p95"`
	P99         float64 `json:"p99"`
}

// SnapshotAll 返回全部维度的指标快照，供控制面展示。
func (m *Metrics) SnapshotAll() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]string, 0, len(m.rows))
	for k := range m.rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]Snapshot, 0, len(keys))
	for _, k := range keys {
		row := m.rows[k]
		out = append(out, snapshotOf(k, row))
	}
	return out
}

// snapshotOf 计算单个维度的汇总与分位延迟。
func snapshotOf(key string, row *metricRow) Snapshot {
	s := Snapshot{Key: key, Totals: row.Totals, Success: row.Success, Errors: row.Errors}
	if row.Totals > 0 {
		s.SuccessRate = float64(row.Success) / float64(row.Totals)
	}
	if len(row.latency) > 0 {
		sorted := append([]float64(nil), row.latency...)
		sort.Float64s(sorted)
		s.P50 = percentile(sorted, 0.50)
		s.P95 = percentile(sorted, 0.95)
		s.P99 = percentile(sorted, 0.99)
	}
	return s
}

// percentile 计算有序样本的指定分位数（线性插值）。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
