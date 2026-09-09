package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// metricsWindow.go —— 看板/观测的**服务端窗口聚合**端点。
//
// 为什么要它：调用日志是无限增长的流水表，控制台此前把窗口内的原始行（截断到
// 200 条）拉到浏览器再聚合——既随流量线性膨胀，又因为截断而口径失真（窗口内超过
// 200 次调用时，总量/成功率/分位延迟/工具榜单都只是样本值）。本端点把统计下沉到
// 存储层：一次范围扫描（ts 索引）返回计数、均值与固定桶直方图，分组按调用量
// 截断，响应体积与窗口维度数相关、与表总量无关。

const (
	// metricsWindowDefaultMinutes 缺省窗口（与看板默认档位一致）。
	metricsWindowDefaultMinutes = 30
	// metricsWindowMaxMinutes 窗口上限（30 天）：约束单次范围扫描的行数上限。
	metricsWindowMaxMinutes = 30 * 24 * 60
	// metricsWindowTopTools / metricsWindowTopIPs 是分组返回条数的默认上限。
	metricsWindowTopTools = 200
	metricsWindowTopIPs   = 50
)

// metricsWindowGroup 是分组维度的窗口统计（分位延迟由固定桶直方图近似）。
type metricsWindowGroup struct {
	Key         string  `json:"key"`
	Totals      int64   `json:"totals"`
	Success     int64   `json:"success"`
	Errors      int64   `json:"errors"`
	SuccessRate float64 `json:"success_rate"`
	Avg         float64 `json:"avg"`
	P50         float64 `json:"p50"`
	P95         float64 `json:"p95"`
	P99         float64 `json:"p99"`
}

// metricsWindowMinute 是窗口内单个分钟桶（供延迟/成功率趋势）。
type metricsWindowMinute struct {
	Minute  int64   `json:"minute"`
	Totals  int64   `json:"totals"`
	Success int64   `json:"success"`
	Errors  int64   `json:"errors"`
	Avg     float64 `json:"avg"`
	P95     float64 `json:"p95"`
}

// metricsWindowResponse 是 GET /api/metrics/window 的响应体。
type metricsWindowResponse struct {
	From        model.Time                 `json:"from"`
	To          model.Time                 `json:"to"`
	Minutes     int                        `json:"minutes"`
	// BucketMinutes 是 by_minute 的桶宽（分钟）：长窗口自动降采样，点数不超过 500。
	BucketMinutes int                      `json:"bucket_minutes"`
	Totals      int64                      `json:"totals"`
	Success     int64                      `json:"success"`
	Errors      int64                      `json:"errors"`
	SuccessRate float64                    `json:"success_rate"`
	Avg         float64                    `json:"avg"`
	P50         float64                    `json:"p50"`
	P95         float64                    `json:"p95"`
	P99         float64                    `json:"p99"`
	ByServer    []metricsWindowGroup       `json:"by_server,omitempty"`
	ByTool      []metricsWindowGroup       `json:"by_tool,omitempty"`
	ByClientIP  []metricsWindowGroup       `json:"by_client_ip,omitempty"`
	ByStatus    []model.TrafficStatusCount `json:"by_status,omitempty"`
	ByMinute    []metricsWindowMinute      `json:"by_minute,omitempty"`
}

// handleMetricsWindow 返回窗口内的调用统计聚合（整体 + by_server/by_tool/
// by_client_ip/by_status/by_minute）。
//
// 参数：minutes 窗口分钟数（默认 30，1..43200，支持 3 天/7 天）；server_id 可选
// （只看某 Server）；top_tools / top_ips 可选（分组返回条数上限，<=0 用默认值）；
// bucket_minutes 可选（by_minute 的桶宽，缺省按窗口自动降采样以保证点数 <= 500）。
func (c *Control) handleMetricsWindow(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	minutes := metricsWindowDefaultMinutes
	if raw := strings.TrimSpace(v.Get("minutes")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "minutes 须为正整数"))
			return
		}
		if n > metricsWindowMaxMinutes {
			n = metricsWindowMaxMinutes
		}
		minutes = n
	}
	topTools, err := optionalPositiveInt(v.Get("top_tools"), metricsWindowTopTools, "top_tools")
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	topIPs, err := optionalPositiveInt(v.Get("top_ips"), metricsWindowTopIPs, "top_ips")
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	// 桶宽：缺省按窗口自动降采样（长窗口返回的 by_minute 点数不超过 maxTrendPoints）。
	bucketMinutes := trendBucketMinutes(minutes)
	if raw := strings.TrimSpace(v.Get("bucket_minutes")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 1440 {
			writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "bucket_minutes 须为 1..1440 的整数"))
			return
		}
		bucketMinutes = n
	}

	now := time.Now().UTC()
	from := now.Add(-time.Duration(minutes) * time.Minute)
	stats, err := c.store.QueryTrafficWindow(r.Context(), query.TrafficWindowQuery{
		From:     from,
		To:       now,
		ServerID: strings.TrimSpace(v.Get("server_id")),
		TopTools: topTools,
		TopIPs:   topIPs,
	})
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}

	resp := metricsWindowResponse{
		From:          model.T(from),
		To:            model.T(now),
		Minutes:       minutes,
		BucketMinutes: bucketMinutes,
		Totals:        stats.Totals,
		Success:       stats.Success,
		Errors:        stats.Errors,
		SuccessRate:   stats.SuccessRate(),
		Avg:           round1(stats.Latency.Avg()),
		P50:           stats.Latency.Percentile(0.50),
		P95:           stats.Latency.Percentile(0.95),
		P99:           stats.Latency.Percentile(0.99),
		ByStatus:      stats.ByStatus,
	}
	resp.ByServer = windowGroups(stats.ByServer)
	resp.ByTool = windowGroups(stats.ByTool)
	resp.ByClientIP = windowGroups(stats.ByClientIP)
	resp.ByMinute = fillWindowMinutes(stats.ByMinute, from, now, bucketMinutes)
	writeOK(w, RequestIDFrom(r.Context()), resp)
}

// windowGroups 把存储层分组统计转换为响应结构（补齐成功率与分位）。
func windowGroups(in []model.TrafficGroupStats) []metricsWindowGroup {
	if len(in) == 0 {
		return nil
	}
	out := make([]metricsWindowGroup, 0, len(in))
	for _, g := range in {
		out = append(out, metricsWindowGroup{
			Key:         g.Key,
			Totals:      g.Totals,
			Success:     g.Success,
			Errors:      g.Errors,
			SuccessRate: g.SuccessRate(),
			Avg:         round1(g.Latency.Avg()),
			P50:         g.Latency.Percentile(0.50),
			P95:         g.Latency.Percentile(0.95),
			P99:         g.Latency.Percentile(0.99),
		})
	}
	return out
}

// optionalPositiveInt 解析可选的正整数参数；缺省返回 fallback，非法返回参数错误。
func optionalPositiveInt(raw string, fallback int, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, errs.New(errs.CodeInvalidArgument, "%s 须为正整数", name)
	}
	return n, nil
}

// round1 保留一位小数（看板展示用，避免无意义的浮点噪声）。
func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
