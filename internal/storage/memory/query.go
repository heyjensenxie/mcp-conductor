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
func (s *Store) QueryTraffic(_ context.Context, q query.TrafficQuery) ([]model.TrafficSample, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]model.TrafficSample, 0)
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
		if q.Q != "" &&
			!containsFold(sample.Tool, q.Q) &&
			!containsFold(sample.Client, q.Q) &&
			!containsFold(sample.RequestID, q.Q) {
			continue
		}
		if !q.From.IsZero() && sample.Timestamp.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && sample.Timestamp.After(q.To) {
			continue
		}
		rows = append(rows, stripArgs(sample))
	}
	return applyPage(rows, q.Paging), len(rows), nil
}
