// Package observability 汇集调用观测：指标聚合与调用日志（Audit）。
//
// 默认不记录完整 Tool 参数/返回值（可能含隐私），由配置的表体记录开关与
// 采样率控制；未来在此扩展 Masking / Sampling 的精细配置。
package observability

import (
	"math"
	"sort"
	"sync"
	"time"
)

// metricsCap 每个维度保留的最大延迟样本数，防止无限增长。
const metricsCap = 4096

// ServerDimPrefix 是 metrics 中按 Server 聚合的维度前缀（供 Servers 列表
// 展示每个 Server 的 Requests / P95）。默认 /metrics 不返回此类行，避免
// 污染工具维语义；需 ?scope=server 时才单独返回。
const ServerDimPrefix = "server:"

// metricRow 是单个维度（工具/Server 等）的聚合指标。
type metricRow struct {
	Totals  int64
	Success int64
	Errors  int64
	latency []float64 // 单位 ms，达到上限后仅保留最新
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
