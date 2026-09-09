package model

import "math"

// traffic_stats.go —— 看板/观测的**有界**统计聚合类型。
//
// 背景：调用日志（traffic_log）是无限增长的流水表，把窗口内的原始行搬到应用层
// 再聚合，成本随流量线性膨胀（内存放大、GC 抖动、传输体积）。因此窗口统计改为
// “存储层聚合、有界返回”：
//   - 计数/成功率：精确（COUNT/SUM）；
//   - 平均延迟：精确（SUM(latency_ms)/COUNT）；
//   - 分位延迟：固定桶直方图近似（p 取所在桶上界），桶数固定 → 内存与 CPU 有界，
//     memory 与 mysql 两种后端语义一致；
//   - 分组：by_server / by_tool / by_client_ip 各自按调用量降序截断（TopN）。

// latencyBucketCount 是延迟直方图的桶数（含溢出桶）。
const latencyBucketCount = 26

// LatencyBoundsMS 是直方图的桶上界（毫秒，升序）：ms <= Bounds[i] 落入第 i 桶，
// 超出最后一个上界的落入溢出桶。低延迟段（1..200ms）分桶更细，兼顾看板可读性。
var LatencyBoundsMS = [latencyBucketCount - 1]int64{
	1, 2, 3, 4, 5, 7, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 300, 500, 1000, 2000, 5000,
}

// LatencyStats 是延迟的有界聚合：样本数、总延迟（求精确均值）与固定桶直方图。
type LatencyStats struct {
	Count   int64                    `json:"count"`
	SumMS   int64                    `json:"sum_ms"`
	Buckets [latencyBucketCount]int64 `json:"-"`
}

// Add 记录一个延迟样本（毫秒）。
func (s *LatencyStats) Add(ms int64) {
	if ms < 0 {
		ms = 0
	}
	s.Count++
	s.SumMS += ms
	s.Buckets[latencyBucketIndex(ms)]++
}

// Merge 合并另一份延迟聚合（用于把分组结果汇总为整体）。
func (s *LatencyStats) Merge(other LatencyStats) {
	s.Count += other.Count
	s.SumMS += other.SumMS
	for i := range s.Buckets {
		s.Buckets[i] += other.Buckets[i]
	}
}

// Avg 返回平均延迟（无样本时 0）。
func (s LatencyStats) Avg() float64 {
	if s.Count == 0 {
		return 0
	}
	return float64(s.SumMS) / float64(s.Count)
}

// Percentile 返回分位延迟（毫秒，取所在桶上界；无样本返回 0）。
// 溢出桶（> 最后一个上界）返回最后一个上界，作为可读的下界展示。
func (s LatencyStats) Percentile(p float64) float64 {
	if s.Count == 0 {
		return 0
	}
	if p <= 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	target := int64(math.Ceil(p * float64(s.Count)))
	if target < 1 {
		target = 1
	}
	var cumulative int64
	for i, c := range s.Buckets {
		cumulative += c
		if cumulative < target {
			continue
		}
		if i >= len(LatencyBoundsMS) {
			return float64(LatencyBoundsMS[len(LatencyBoundsMS)-1])
		}
		return float64(LatencyBoundsMS[i])
	}
	return float64(LatencyBoundsMS[len(LatencyBoundsMS)-1])
}

// latencyBucketIndex 返回 ms 落入的桶下标。
func latencyBucketIndex(ms int64) int {
	for i, bound := range LatencyBoundsMS {
		if ms <= bound {
			return i
		}
	}
	return len(LatencyBoundsMS)
}

// TrafficGroupStats 是某个分组维度（server / tool / client_ip）的窗口聚合。
type TrafficGroupStats struct {
	Key     string       `json:"key"`
	Totals  int64        `json:"totals"`
	Success int64        `json:"success"`
	Errors  int64        `json:"errors"`
	Latency LatencyStats `json:"latency"`
}

// SuccessRate 返回成功率（无样本时 0）。
func (g TrafficGroupStats) SuccessRate() float64 {
	if g.Totals == 0 {
		return 0
	}
	return float64(g.Success) / float64(g.Totals)
}

// TrafficStatusCount 是窗口内某个调用状态的出现次数（success 或错误码）。
type TrafficStatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// TrafficMinuteStats 是窗口内某个 UTC 分钟桶的聚合（供延迟/成功率趋势）。
type TrafficMinuteStats struct {
	Minute  int64        `json:"minute"`
	Totals  int64        `json:"totals"`
	Success int64        `json:"success"`
	Errors  int64        `json:"errors"`
	Latency LatencyStats `json:"latency"`
}

// TrafficWindowStats 是窗口统计的存储层聚合结果（不含派生字段，序列化由控制面组装）。
type TrafficWindowStats struct {
	Totals     int64                `json:"totals"`
	Success    int64                `json:"success"`
	Errors     int64                `json:"errors"`
	Latency    LatencyStats         `json:"latency"`
	ByServer   []TrafficGroupStats  `json:"by_server,omitempty"`
	ByTool     []TrafficGroupStats  `json:"by_tool,omitempty"`
	ByClientIP []TrafficGroupStats  `json:"by_client_ip,omitempty"`
	ByStatus   []TrafficStatusCount `json:"by_status,omitempty"`
	ByMinute   []TrafficMinuteStats `json:"by_minute,omitempty"`
}

// SuccessRate 返回整体成功率（无样本时 0）。
func (w TrafficWindowStats) SuccessRate() float64 {
	if w.Totals == 0 {
		return 0
	}
	return float64(w.Success) / float64(w.Totals)
}
