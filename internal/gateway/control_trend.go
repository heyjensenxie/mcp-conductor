package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

const trendMinuteSeconds = 60

// handleMetricsTrend 返回近 minutes 分钟的调用时序（分钟桶，长程已持久化）。
//
// 读侧以 trend_minute 存储为“已闭合分钟”权威源；当前 open 分钟与最近尚未落库的
// 闭合分钟由进程内热桶 HotTrend 填补（按维度×分钟判断，store 已含的不双计），
// 在 ≤1min 异步落库延迟下保持展示无缝、跨重启可回溯到保留天数。
//
// 参数：scope=tool|server|instance（缺省 tool；instance 须带 server_id，实例 id
// 按归属 Server 收敛）；minutes 默认 30、须为正整数、超过保留窗口则截断；
// dim_key 可选——非空只返回该单个维度（tool=gateway名 / server=server id /
// instance=instance id），否则聚合同 scope 的全部维度。
func (c *Control) handleMetricsTrend(w http.ResponseWriter, r *http.Request) {
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "tool"
	}
	if scope != "tool" && scope != "server" && scope != "instance" {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "scope 仅支持 tool / server / instance"))
		return
	}
	serverID := strings.TrimSpace(r.URL.Query().Get("server_id"))
	if scope == "instance" && serverID == "" {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "scope=instance 时须提供 server_id"))
		return
	}
	minutes := 30
	if s := r.URL.Query().Get("minutes"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "minutes 须为正整数"))
			return
		}
		minutes = n
	}
	if c.trendRetentionMinutes > 0 && minutes > c.trendRetentionMinutes {
		minutes = c.trendRetentionMinutes
	}
	dimKey := strings.TrimSpace(r.URL.Query().Get("dim_key"))

	nowMin := time.Now().UTC().Truncate(time.Minute).Unix()
	from := nowMin - int64(minutes-1)*trendMinuteSeconds
	storeServerID := serverID
	if scope == "tool" {
		storeServerID = "" // tool 维行 server_id 恒为空串
	}
	rows, err := c.store.QueryTrendBuckets(r.Context(), query.TrendQuery{
		Scope: scope, ServerID: storeServerID, DimKey: dimKey, From: from, To: nowMin,
	})
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	hot := c.metrics.HotTrend(scope, storeServerID, dimKey, from, nowMin)

	series := mergeTrendSeries(rows, hot, from, minutes)
	writeOK(w, RequestIDFrom(r.Context()), map[string]any{"series": series})
}

// trendRef 标识某维度在某分钟的行（覆盖判定用：同维同分钟只计一次）。
type trendRef struct {
	serverID, dimKey string
	minute           int64
}

// mergeTrendSeries 把存储行与热桶合并为连续补零的 TrendPoint 序列：
// store 为权威源；热桶仅填补 store 缺失的（维度×分钟）——避免把 open/未落库分钟
// 丢掉，也不与已落库分钟双计。
func mergeTrendSeries(rows, hot []model.TrendMinute, from int64, minutes int) []observability.TrendPoint {
	present := make(map[trendRef]bool, len(rows))
	for _, b := range rows {
		present[trendRef{b.ServerID, b.DimKey, b.Minute}] = true
	}

	acc := make(map[int64]struct{ totals, errors int64 }, len(rows)+len(hot))
	sum := func(list []model.TrendMinute, onlyIfAbsent bool) {
		for _, b := range list {
			ref := trendRef{b.ServerID, b.DimKey, b.Minute}
			if onlyIfAbsent && present[ref] {
				continue
			}
			v := acc[b.Minute]
			v.totals += b.Totals
			v.errors += b.Errors
			acc[b.Minute] = v
			present[ref] = true
		}
	}
	sum(rows, false)
	sum(hot, true)

	series := make([]observability.TrendPoint, minutes)
	for i := range series {
		ts := from + int64(i)*trendMinuteSeconds
		series[i].TS = ts
		series[i].Totals = acc[ts].totals
		series[i].Errors = acc[ts].errors
	}
	return series
}
