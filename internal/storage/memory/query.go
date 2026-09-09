package memory

import (
	"context"
	"sort"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// ---- 管理面列表查询（memory 实现）----
//
// 与 mysql 实现语义对齐：关键词模糊匹配采用大小写不敏感包含（对齐 utf8mb4
// LIKE 的不区分大小写），筛选在分页与全量（PageSize<=0）两种模式下都生效；
// 排序固定（servers/routes/keys/credentials 按 id，tools 按 gateway_name，
// traffic 按写入倒序）。返回行均为 map 中的值副本，安全返回调用方。

// containsFold 判断 s 是否包含子串 keyword（ASCII 大小写不敏感）。
func containsFold(s, keyword string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(keyword))
}

// triStateMatch 应用三态布尔筛选：filter 为 nil 表示不过滤。
func triStateMatch(actual bool, filter *bool) bool {
	return filter == nil || actual == *filter
}

// applyPage 按 Paging 切页；PageSize<=0 时返回全部（不分页）。
func applyPage[T any](rows []T, p query.Paging) []T {
	if p.PageSize <= 0 {
		return rows
	}
	start := (p.Page - 1) * p.PageSize
	if start < 0 {
		start = 0
	}
	if start >= len(rows) {
		return nil
	}
	end := start + p.PageSize
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

// QueryServers 分页查询 Server（按 id 升序）。
func (s *Store) QueryServers(_ context.Context, q query.ServerQuery) ([]model.Server, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]model.Server, 0)
	for _, id := range sortedKeys(s.servers) {
		server := s.servers[id]
		if q.Q != "" && !containsFold(server.Name, q.Q) {
			continue
		}
		if !triStateMatch(server.Enabled, q.Enabled) {
			continue
		}
		if q.HealthStatus != "" && string(server.HealthStatus) != q.HealthStatus {
			continue
		}
		rows = append(rows, server)
	}
	if q.SkipTotal {
		return applyPage(rows, q.Paging), 0, nil
	}
	return applyPage(rows, q.Paging), len(rows), nil
}

// QueryTools 分页查询 Tool（按 gateway_name 升序）；ServerID 非空时限定单 Server。
func (s *Store) QueryTools(_ context.Context, q query.ToolQuery) ([]model.Tool, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]model.Tool, 0)
	for _, tool := range s.tools {
		if q.ServerID != "" && tool.ServerID != q.ServerID {
			continue
		}
		if !triStateMatch(tool.Enabled, q.Enabled) {
			continue
		}
		if q.Q != "" && !containsFold(tool.GatewayName, q.Q) && !containsFold(tool.OriginalName, q.Q) {
			continue
		}
		rows = append(rows, tool)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].GatewayName < rows[j].GatewayName })
	if q.SkipTotal {
		return applyPage(rows, q.Paging), 0, nil
	}
	return applyPage(rows, q.Paging), len(rows), nil
}

// QueryRoutes 分页查询路由（按 id 升序）。
func (s *Store) QueryRoutes(_ context.Context, q query.RouteQuery) ([]model.Route, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]model.Route, 0)
	for _, id := range sortedKeys(s.routes) {
		route := s.routes[id]
		if q.Q != "" && !containsFold(route.Name, q.Q) {
			continue
		}
		if q.ServerID != "" && route.ServerID != q.ServerID {
			continue
		}
		if !triStateMatch(route.Enabled, q.Enabled) {
			continue
		}
		rows = append(rows, route)
	}
	return applyPage(rows, q.Paging), len(rows), nil
}

// QueryCredentials 分页查询单 Server 凭证元数据（按 id 升序）。
// 管理列表语义与 mysql 一致：只返回元数据，不带明文/内部 Value。
func (s *Store) QueryCredentials(_ context.Context, q query.CredentialQuery) ([]model.Credential, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]model.Credential, 0)
	for _, id := range sortedKeys(s.credentials) {
		cred := s.credentials[id]
		if cred.ServerID != q.ServerID {
			continue
		}
		if q.Q != "" && !containsFold(cred.Name, q.Q) {
			continue
		}
		if q.Kind != "" && string(cred.Kind) != q.Kind {
			continue
		}
		if !triStateMatch(cred.HasValue, q.HasValue) {
			continue
		}
		cred.Value = "" // 列表不下发内部凭证值
		rows = append(rows, cred)
	}
	return applyPage(rows, q.Paging), len(rows), nil
}

// QueryAccessKeys 分页查询 API Key（按 id 升序）。grants 已在存储副本内，无需二次查询。
func (s *Store) QueryAccessKeys(_ context.Context, q query.AccessKeyQuery) ([]model.AccessKey, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]model.AccessKey, 0)
	for _, id := range sortedKeys(s.keys) {
		key := s.keys[id]
		if !triStateMatch(key.Enabled, q.Enabled) {
			continue
		}
		if q.Q != "" && !containsFold(key.Name, q.Q) && !containsFold(key.Subject, q.Q) {
			continue
		}
		rows = append(rows, key)
	}
	return applyPage(rows, q.Paging), len(rows), nil
}

// QueryTraffic 分页查询调用日志（按写入倒序，与 RecentTraffic 一致）。
//
// 内存占用只与**本页行数**相关：从尾部倒序扫描，只物化落在请求页内的行；仅在
// 调用方需要 total（SkipTotal=false）时继续扫描计数，但不复制行。此前实现会把
// 全部匹配行复制一份再切页，50k 行规模下单次请求即产生 ~47MB 垃圾（见基准）。
func (s *Store) QueryTraffic(_ context.Context, q query.TrafficQuery) ([]model.TrafficSample, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pageSize := q.Paging.PageSize
	start := 0
	if pageSize > 0 {
		start = (q.Paging.Page - 1) * pageSize
		if start < 0 {
			start = 0
		}
	}
	// 全量模式（pageSize<=0）仍需要承载全部匹配行，容量按当前总量预估避免反复扩容。
	capacity := pageSize
	if capacity <= 0 {
		capacity = len(s.traffic)
	}

	out := make([]model.TrafficSample, 0, capacity)
	matched := 0
	for i := len(s.traffic) - 1; i >= 0; i-- {
		sample := s.traffic[i]
		if q.ServerID != "" && sample.ServerID != q.ServerID {
			continue
		}
		if q.InstanceID != "" && sample.InstanceID != q.InstanceID {
			continue
		}
		if q.Status != "" && sample.Status != q.Status {
			continue
		}
		if q.ClientIP != "" && sample.ClientIP != q.ClientIP {
			continue
		}
		if q.Q != "" &&
			!containsFold(sample.Tool, q.Q) &&
			!containsFold(sample.Client, q.Q) &&
			!containsFold(sample.RequestID, q.Q) &&
			!containsFold(sample.ClientIP, q.Q) {
			continue
		}
		if !q.From.IsZero() && sample.Timestamp.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && sample.Timestamp.After(q.To) {
			continue
		}
		// 命中：仅在请求页范围内物化。
		if pageSize <= 0 || (matched >= start && matched < start+pageSize) {
			out = append(out, stripArgs(sample))
		}
		matched++
		if q.SkipTotal && pageSize > 0 && matched >= start+pageSize {
			break // 不需要 total 且本页已满：无需继续扫描
		}
	}
	if q.SkipTotal {
		return out, 0, nil
	}
	return out, matched, nil
}

// trafficGroupCap 是窗口聚合中 by_tool / by_client_ip 的硬上限（未显式指定 TopN 时），
// 防止维度爆炸（如每请求一个 client_ip）把响应体撑大。
const trafficGroupCap = 200

// QueryTrafficWindow 在窗口内做一次聚合：整体 + by_server/by_tool/by_client_ip/
// by_status/by_minute，全部在一次遍历中完成（无逐行复制，内存与窗口内维度数相关）。
func (s *Store) QueryTrafficWindow(_ context.Context, q query.TrafficWindowQuery) (model.TrafficWindowStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out model.TrafficWindowStats
	byServer := make(map[string]*model.TrafficGroupStats)
	byTool := make(map[string]*model.TrafficGroupStats)
	byIP := make(map[string]*model.TrafficGroupStats)
	byStatus := make(map[string]int64)
	byMinute := make(map[int64]*model.TrafficMinuteStats)

	for i := range s.traffic {
		sample := &s.traffic[i]
		if q.ServerID != "" && sample.ServerID != q.ServerID {
			continue
		}
		if !q.From.IsZero() && sample.Timestamp.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && sample.Timestamp.After(q.To) {
			continue
		}
		ok := sample.Status == "success"

		out.Totals++
		if ok {
			out.Success++
		} else {
			out.Errors++
		}
		out.Latency.Add(sample.LatencyMS)

		if sample.ServerID != "" {
			groupAdd(byServer, sample.ServerID, sample.LatencyMS, ok)
		}
		if sample.Tool != "" {
			groupAdd(byTool, sample.Tool, sample.LatencyMS, ok)
		}
		if sample.ClientIP != "" {
			groupAdd(byIP, sample.ClientIP, sample.LatencyMS, ok)
		}
		byStatus[sample.Status]++

		minute := sample.Timestamp.Unix() - sample.Timestamp.Unix()%60
		ms, exists := byMinute[minute]
		if !exists {
			ms = &model.TrafficMinuteStats{Minute: minute}
			byMinute[minute] = ms
		}
		ms.Totals++
		if ok {
			ms.Success++
		} else {
			ms.Errors++
		}
		ms.Latency.Add(sample.LatencyMS)
	}

	out.ByServer = topGroups(byServer, 0)
	out.ByTool = topGroups(byTool, topCap(q.TopTools))
	out.ByClientIP = topGroups(byIP, topCap(q.TopIPs))
	for status, count := range byStatus {
		out.ByStatus = append(out.ByStatus, model.TrafficStatusCount{Status: status, Count: count})
	}
	sort.Slice(out.ByStatus, func(i, j int) bool {
		if out.ByStatus[i].Count != out.ByStatus[j].Count {
			return out.ByStatus[i].Count > out.ByStatus[j].Count
		}
		return out.ByStatus[i].Status < out.ByStatus[j].Status
	})
	for _, ms := range byMinute {
		out.ByMinute = append(out.ByMinute, *ms)
	}
	sort.Slice(out.ByMinute, func(i, j int) bool { return out.ByMinute[i].Minute < out.ByMinute[j].Minute })
	if q.TopMinutes > 0 && len(out.ByMinute) > q.TopMinutes {
		out.ByMinute = out.ByMinute[len(out.ByMinute)-q.TopMinutes:]
	}
	return out, nil
}

// groupAdd 把一次调用累加到分组聚合。
func groupAdd(groups map[string]*model.TrafficGroupStats, key string, latencyMS int64, ok bool) {
	group, exists := groups[key]
	if !exists {
		group = &model.TrafficGroupStats{Key: key}
		groups[key] = group
	}
	group.Totals++
	if ok {
		group.Success++
	} else {
		group.Errors++
	}
	group.Latency.Add(latencyMS)
}

// topCap 归一化 TopN：<=0 用默认硬上限，超过硬上限按硬上限收敛。
func topCap(n int) int {
	if n <= 0 || n > trafficGroupCap {
		return trafficGroupCap
	}
	return n
}

// topGroups 把分组 map 按调用量降序（同量按 key 升序）输出，最多 limit 条。
func topGroups(groups map[string]*model.TrafficGroupStats, limit int) []model.TrafficGroupStats {
	out := make([]model.TrafficGroupStats, 0, len(groups))
	for _, group := range groups {
		out = append(out, *group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Totals != out[j].Totals {
			return out[i].Totals > out[j].Totals
		}
		return out[i].Key < out[j].Key
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
