package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// ---- 管理面列表查询（MySQL 5.7 实现）----
//
// 面向控制台列表的分页 + 关键词/字段筛选。WHERE 由谓词片段与参数列表动态
// 组装（与 UpdateCredential 的 builder 风格一致），所有筛选值一律走参数绑定，
// 仅列名是固定常量——用户输入不进入 SQL 文本。MySQL 5.7 契约下不使用
// Window Function / CTE：分页用 LIMIT/OFFSET，总数用独立 COUNT(*)。
// 排序固定：servers/routes/credentials/access_keys 按 id、tools 按 gateway_name、
// traffic 按 id 倒序（id 单调，倒序即写入倒序）。

// serverColumns 只投影逻辑列；Endpoint/Transport 属于 server_instances，见 instances.go。
const serverColumns = `id, name, COALESCE(description,''), enabled, health_status, created_at, updated_at`

// escapeLike 转义 MySQL LIKE 元字符（\ % _），保证关键词按字面匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// likePattern 把原始关键词包装为可安全注入的 %term% 模式。
func likePattern(s string) string { return "%" + escapeLike(s) + "%" }

// paginate 仅在 PageSize>0 时追加 LIMIT/OFFSET；否则保持全量。
func paginate(sql string, args []any, p query.Paging) (string, []any) {
	if p.PageSize <= 0 {
		return sql, args
	}
	offset := (p.Page - 1) * p.PageSize
	if offset < 0 {
		offset = 0
	}
	return sql + " LIMIT ? OFFSET ?", append(args, p.PageSize, offset)
}

// inClause 为 ids 生成 "col IN (?,?,...)" 占位片段并追加参数。
func inClause(col string, ids []string, args []any) (string, []any) {
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	for _, id := range ids {
		args = append(args, id)
	}
	return col + " IN (" + ph + ")", args
}

// countRows 对给定表与 WHERE 谓词执行 COUNT(*)，返回匹配总数。
func (s *Store) countRows(ctx context.Context, table string, cond []string, args []any) (int, error) {
	sql := "SELECT COUNT(*) FROM " + table
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, sql, args...).Scan(&total); err != nil {
		return 0, err
	}
	return int(total), nil
}

// QueryServers 分页查询 Server（按 id 升序）。
func (s *Store) QueryServers(ctx context.Context, q query.ServerQuery) ([]model.Server, int, error) {
	cond := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if q.Q != "" {
		cond = append(cond, "name LIKE ?")
		args = append(args, likePattern(q.Q))
	}
	if q.Enabled != nil {
		cond = append(cond, "enabled = ?")
		args = append(args, *q.Enabled)
	}
	if q.HealthStatus != "" {
		cond = append(cond, "health_status = ?")
		args = append(args, q.HealthStatus)
	}
	total := 0
	if !q.SkipTotal {
		var err error
		total, err = s.countRows(ctx, "servers", cond, args)
		if err != nil {
			return nil, 0, fmt.Errorf("count servers: %w", err)
		}
	}

	sql := "SELECT " + serverColumns + " FROM servers"
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " ORDER BY id"
	sql, qargs := paginate(sql, args, q.Paging)

	rows, err := s.db.QueryContext(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()

	out := make([]model.Server, 0)
	for rows.Next() {
		var server model.Server
		if err := rows.Scan(&server.ID, &server.Name, &server.Description,
			&server.Enabled, &server.HealthStatus, &server.CreatedAt, &server.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan server: %w", err)
		}
		out = append(out, server)
	}
	return out, total, rows.Err()
}

// QueryTools 分页查询 Tool（按 gateway_name 升序）；ServerID 非空时限定单 Server。
func (s *Store) QueryTools(ctx context.Context, q query.ToolQuery) ([]model.Tool, int, error) {
	cond := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if q.ServerID != "" {
		cond = append(cond, "server_id = ?")
		args = append(args, q.ServerID)
	}
	if q.Q != "" {
		cond = append(cond, "(gateway_name LIKE ? OR original_name LIKE ?)")
		pat := likePattern(q.Q)
		args = append(args, pat, pat)
	}
	if q.Enabled != nil {
		cond = append(cond, "enabled = ?")
		args = append(args, *q.Enabled)
	}
	total := 0
	if !q.SkipTotal {
		var err error
		total, err = s.countRows(ctx, "tools", cond, args)
		if err != nil {
			return nil, 0, fmt.Errorf("count tools: %w", err)
		}
	}

	sql := "SELECT " + toolColumns + " FROM tools"
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " ORDER BY gateway_name"
	sql, qargs := paginate(sql, args, q.Paging)

	rows, err := s.db.QueryContext(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tools: %w", err)
	}
	defer rows.Close()

	out := make([]model.Tool, 0)
	for rows.Next() {
		tool, err := scanTool(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan tool: %w", err)
		}
		out = append(out, tool)
	}
	return out, total, rows.Err()
}

// QueryRoutes 分页查询路由（按 id 升序）。
func (s *Store) QueryRoutes(ctx context.Context, q query.RouteQuery) ([]model.Route, int, error) {
	cond := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if q.Q != "" {
		cond = append(cond, "name LIKE ?")
		args = append(args, likePattern(q.Q))
	}
	if q.ServerID != "" {
		cond = append(cond, "server_id = ?")
		args = append(args, q.ServerID)
	}
	if q.Enabled != nil {
		cond = append(cond, "enabled = ?")
		args = append(args, *q.Enabled)
	}
	total, err := s.countRows(ctx, "routes", cond, args)
	if err != nil {
		return nil, 0, fmt.Errorf("count routes: %w", err)
	}

	sql := "SELECT id, name, server_id, COALESCE(tool_names,''), enabled, created_at, updated_at FROM routes"
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " ORDER BY id"
	sql, qargs := paginate(sql, args, q.Paging)

	rows, err := s.db.QueryContext(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query routes: %w", err)
	}
	defer rows.Close()

	out := make([]model.Route, 0)
	for rows.Next() {
		var route model.Route
		var names string
		if err := rows.Scan(&route.ID, &route.Name, &route.ServerID, &names, &route.Enabled,
			&route.CreatedAt, &route.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan route: %w", err)
		}
		if err := unmarshalJSON(names, &route.ToolNames); err != nil {
			return nil, 0, fmt.Errorf("解析 route %q tool_names 失败: %w", route.ID, err)
		}
		out = append(out, route)
	}
	return out, total, rows.Err()
}

// QueryCredentials 分页查询单 Server 凭证元数据（按 id 升序）。
// 与 ListCredentialsByServer（解密供上游注入）不同：管理列表只返回元数据，
// 不解密 encrypted_value；Value 保持空串且 json:"-" 不下发。
func (s *Store) QueryCredentials(ctx context.Context, q query.CredentialQuery) ([]model.Credential, int, error) {
	cond := []string{"server_id = ?"}
	args := []any{q.ServerID}
	if q.Q != "" {
		cond = append(cond, "name LIKE ?")
		args = append(args, likePattern(q.Q))
	}
	if q.Kind != "" {
		cond = append(cond, "kind = ?")
		args = append(args, q.Kind)
	}
	if q.HasValue != nil {
		cond = append(cond, "has_value = ?")
		args = append(args, *q.HasValue)
	}
	total, err := s.countRows(ctx, "credentials", cond, args)
	if err != nil {
		return nil, 0, fmt.Errorf("count credentials: %w", err)
	}

	sql := "SELECT id, server_id, name, kind, COALESCE(header,''), has_value, created_at, updated_at FROM credentials"
	sql += " WHERE " + strings.Join(cond, " AND ") + " ORDER BY id"
	sql, qargs := paginate(sql, args, q.Paging)

	rows, err := s.db.QueryContext(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query credentials: %w", err)
	}
	defer rows.Close()

	out := make([]model.Credential, 0)
	for rows.Next() {
		var cred model.Credential
		if err := rows.Scan(&cred.ID, &cred.ServerID, &cred.Name, &cred.Kind, &cred.Header,
			&cred.HasValue, &cred.CreatedAt, &cred.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan credential: %w", err)
		}
		out = append(out, cred)
	}
	return out, total, rows.Err()
}

// QueryAccessKeys 分页查询 API Key（按 id 升序）。先分页主表，再对当页 key 的
// grants 做一次 IN 查询聚组，避免像全量 ListAccessKeys 那样载入全部 grant 行。
func (s *Store) QueryAccessKeys(ctx context.Context, q query.AccessKeyQuery) ([]model.AccessKey, int, error) {
	cond := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if q.Q != "" {
		cond = append(cond, "(name LIKE ? OR subject LIKE ?)")
		pat := likePattern(q.Q)
		args = append(args, pat, pat)
	}
	if q.Enabled != nil {
		cond = append(cond, "enabled = ?")
		args = append(args, *q.Enabled)
	}
	total, err := s.countRows(ctx, "access_keys", cond, args)
	if err != nil {
		return nil, 0, fmt.Errorf("count access keys: %w", err)
	}

	sql := "SELECT " + accessKeyColumns + " FROM access_keys"
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " ORDER BY id"
	sql, qargs := paginate(sql, args, q.Paging)

	rows, err := s.db.QueryContext(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query access keys: %w", err)
	}
	defer rows.Close()

	out := make([]model.AccessKey, 0)
	for rows.Next() {
		key, err := scanAccessKey(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan access key: %w", err)
		}
		out = append(out, key)
	}
	if rows.Err() != nil {
		return nil, 0, rows.Err()
	}
	if len(out) == 0 {
		return out, total, nil
	}
	if err := s.loadKeyGrants(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// loadKeyGrants 一次读取给定 keys 的全部 grant 行并按 key_id 聚组回填。
func (s *Store) loadKeyGrants(ctx context.Context, keys []model.AccessKey) error {
	ids := make([]string, 0, len(keys))
	index := make(map[string]int, len(keys))
	for i := range keys {
		ids = append(ids, keys[i].ID)
		index[keys[i].ID] = i
	}
	where, args := inClause("key_id", ids, nil)
	rows, err := s.db.QueryContext(ctx,
		"SELECT key_id, gateway_name, COALESCE(headers,''), COALESCE(default_args,'') FROM access_key_grants WHERE "+where+" ORDER BY key_id, gateway_name",
		args...)
	if err != nil {
		return fmt.Errorf("load key grants: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var keyID, gatewayName, headers, defaultArgs string
		if err := rows.Scan(&keyID, &gatewayName, &headers, &defaultArgs); err != nil {
			return err
		}
		i, ok := index[keyID]
		if !ok {
			continue
		}
		grant := model.ToolGrant{GatewayName: gatewayName}
		if err := unmarshalJSON(headers, &grant.Headers); err != nil {
			return fmt.Errorf("解析 grant %q headers 失败: %w", gatewayName, err)
		}
		if err := unmarshalJSON(defaultArgs, &grant.DefaultArgs); err != nil {
			return fmt.Errorf("解析 grant %q default_args 失败: %w", gatewayName, err)
		}
		keys[i].Grants = append(keys[i].Grants, grant)
	}
	return rows.Err()
}

// ---- 窗口聚合（看板/观测的服务端统计）----
//
// 设计：不把窗口内的原始行搬到应用层，而是让 MySQL 一次范围扫描完成聚合
// （走 idx_traffic_ts），返回的只有计数、均值与固定桶直方图；分组按调用量降序
// 截断。因此响应体积与 CPU 只与“窗口内行数 + 维度数”相关，与表总量无关。

// latencyCumulativeExprs 返回延迟直方图的**累积**计数表达式（ms <= bound 的行数），
// 与 model.LatencyBoundsMS 一一对应；扫描后再转换为分桶计数。
var latencyCumulativeExprs = sync.OnceValue(func() string {
	parts := make([]string, 0, len(model.LatencyBoundsMS))
	for _, bound := range model.LatencyBoundsMS {
		parts = append(parts, fmt.Sprintf("SUM(CASE WHEN latency_ms <= %d THEN 1 ELSE 0 END)", bound))
	}
	return strings.Join(parts, ", ")
})

// windowAggColumns 是窗口聚合的公共投影（计数/成功数/延迟和 + 直方图累积计数）。
func windowAggColumns() string {
	return "COUNT(*), COALESCE(SUM(status = 'success'), 0), COALESCE(SUM(latency_ms), 0), " + latencyCumulativeExprs()
}

// windowWhere 组装窗口 WHERE 谓词（ts 闭区间 + 可选 server_id）。
// 无任何条件时返回 " WHERE 1=1"，使调用方可以安全地追加 " AND ..." 片段。
func windowWhere(q query.TrafficWindowQuery) (string, []any) {
	cond := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if !q.From.IsZero() {
		cond = append(cond, "ts >= ?")
		args = append(args, fmtTimeUTC(q.From))
	}
	if !q.To.IsZero() {
		cond = append(cond, "ts <= ?")
		args = append(args, fmtTimeUTC(q.To))
	}
	if q.ServerID != "" {
		cond = append(cond, "server_id = ?")
		args = append(args, q.ServerID)
	}
	if len(cond) == 0 {
		return " WHERE 1=1", args
	}
	return " WHERE " + strings.Join(cond, " AND "), args
}

// scanWindowAgg 读取一行 (totals, success, sum_ms, 直方图累积计数...) 并转换为
// LatencyStats（累积 → 分桶）。
func scanWindowAgg(rows *sql.Rows, prefix ...any) (int64, int64, model.LatencyStats, error) {
	var totals, success, sumMS int64
	cumulative := make([]int64, len(model.LatencyBoundsMS))
	dest := make([]any, 0, len(prefix)+3+len(cumulative))
	dest = append(dest, prefix...)
	dest = append(dest, &totals, &success, &sumMS)
	for i := range cumulative {
		dest = append(dest, &cumulative[i])
	}
	if err := rows.Scan(dest...); err != nil {
		return 0, 0, model.LatencyStats{}, err
	}
	return totals, success, latencyStatsFromCumulative(totals, sumMS, cumulative), nil
}

// latencyStatsFromCumulative 把累积计数转换为分桶直方图（最后一个桶为溢出桶）。
func latencyStatsFromCumulative(total, sumMS int64, cumulative []int64) model.LatencyStats {
	stats := model.LatencyStats{Count: total, SumMS: sumMS}
	prev := int64(0)
	for i := range model.LatencyBoundsMS {
		stats.Buckets[i] = cumulative[i] - prev
		prev = cumulative[i]
	}
	stats.Buckets[len(model.LatencyBoundsMS)] = total - prev
	return stats
}

// QueryTrafficWindow 在窗口内做服务端聚合（计数/均值精确、分位由直方图近似）。
// 每个分组维度各一条 GROUP BY 查询，全部命中 idx_traffic_ts 范围扫描。
func (s *Store) QueryTrafficWindow(ctx context.Context, q query.TrafficWindowQuery) (model.TrafficWindowStats, error) {
	var out model.TrafficWindowStats
	where, args := windowWhere(q)

	// 1) 整体。
	row := s.db.QueryRowContext(ctx,
		"SELECT "+windowAggColumns()+" FROM traffic_log"+where, args...)
	var totals, success, sumMS int64
	cumulative := make([]int64, len(model.LatencyBoundsMS))
	dest := []any{&totals, &success, &sumMS}
	for i := range cumulative {
		dest = append(dest, &cumulative[i])
	}
	if err := row.Scan(dest...); err != nil {
		return model.TrafficWindowStats{}, fmt.Errorf("aggregate traffic window: %w", err)
	}
	out.Totals, out.Success = totals, success
	out.Errors = totals - success
	out.Latency = latencyStatsFromCumulative(totals, sumMS, cumulative)

	// 2) 按 Server 分组（Server 数量有限，不截断）。
	byServer, err := s.queryWindowGroups(ctx, "COALESCE(server_id,'')", where, args, 0, "")
	if err != nil {
		return model.TrafficWindowStats{}, err
	}
	out.ByServer = byServer

	// 3) 按工具分组（按调用量降序截断，避免维度爆炸）。
	byTool, err := s.queryWindowGroups(ctx, "tool", where, args, topCap(q.TopTools), " AND tool <> ''")
	if err != nil {
		return model.TrafficWindowStats{}, err
	}
	out.ByTool = byTool

	// 4) 按来源 IP 分组（看板 TopN）。
	byIP, err := s.queryWindowGroups(ctx, "client_ip", where, args, topCap(q.TopIPs), " AND COALESCE(client_ip,'') <> ''")
	if err != nil {
		return model.TrafficWindowStats{}, err
	}
	out.ByClientIP = byIP

	// 5) 按状态分组（异常分类计数）。
	statusSQL := "SELECT status, COUNT(*) FROM traffic_log" + where + " GROUP BY status ORDER BY COUNT(*) DESC"
	statusRows, err := s.db.QueryContext(ctx, statusSQL, args...)
	if err != nil {
		return model.TrafficWindowStats{}, fmt.Errorf("aggregate traffic status: %w", err)
	}
	defer statusRows.Close()
	for statusRows.Next() {
		var item model.TrafficStatusCount
		if err := statusRows.Scan(&item.Status, &item.Count); err != nil {
			return model.TrafficWindowStats{}, fmt.Errorf("scan traffic status: %w", err)
		}
		out.ByStatus = append(out.ByStatus, item)
	}
	if err := statusRows.Err(); err != nil {
		return model.TrafficWindowStats{}, err
	}

	// 6) 按分钟分组（延迟/成功率趋势；DATE_FORMAT 取 UTC 墙钟，避免会话时区影响）。
	minuteSQL := "SELECT DATE_FORMAT(ts, '%Y%m%d%H%i'), " + windowAggColumns() +
		" FROM traffic_log" + where + " GROUP BY 1 ORDER BY 1 ASC"
	minuteRows, err := s.db.QueryContext(ctx, minuteSQL, args...)
	if err != nil {
		return model.TrafficWindowStats{}, fmt.Errorf("aggregate traffic minute: %w", err)
	}
	defer minuteRows.Close()
	for minuteRows.Next() {
		var raw string
		mTotals, mSuccess, stats, err := scanWindowAgg(minuteRows, &raw)
		if err != nil {
			return model.TrafficWindowStats{}, fmt.Errorf("scan traffic minute: %w", err)
		}
		minute, err := time.ParseInLocation("200601021504", raw, time.UTC)
		if err != nil {
			return model.TrafficWindowStats{}, fmt.Errorf("解析分钟桶 %q 失败: %w", raw, err)
		}
		out.ByMinute = append(out.ByMinute, model.TrafficMinuteStats{
			Minute: minute.Unix(), Totals: mTotals, Success: mSuccess,
			Errors: mTotals - mSuccess, Latency: stats,
		})
	}
	if err := minuteRows.Err(); err != nil {
		return model.TrafficWindowStats{}, err
	}
	if q.TopMinutes > 0 && len(out.ByMinute) > q.TopMinutes {
		out.ByMinute = out.ByMinute[len(out.ByMinute)-q.TopMinutes:]
	}
	return out, nil
}

// queryWindowGroups 执行一次 GROUP BY 聚合：keyExpr 为分组键表达式（固定常量，
// 非用户输入），extraWhere 追加维度约束（如排除空值），limit>0 时按调用量降序截断。
func (s *Store) queryWindowGroups(ctx context.Context, keyExpr, where string, args []any, limit int, extraWhere string) ([]model.TrafficGroupStats, error) {
	sqlText := "SELECT " + keyExpr + ", " + windowAggColumns() + " FROM traffic_log" + where + extraWhere +
		" GROUP BY 1 ORDER BY COUNT(*) DESC"
	queryArgs := append([]any(nil), args...)
	if limit > 0 {
		sqlText += " LIMIT ?"
		queryArgs = append(queryArgs, limit)
	}
	rows, err := s.db.QueryContext(ctx, sqlText, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("aggregate traffic by %s: %w", keyExpr, err)
	}
	defer rows.Close()

	out := make([]model.TrafficGroupStats, 0)
	for rows.Next() {
		var key sql.NullString
		totals, success, stats, err := scanWindowAgg(rows, &key)
		if err != nil {
			return nil, fmt.Errorf("scan traffic group %s: %w", keyExpr, err)
		}
		out = append(out, model.TrafficGroupStats{
			Key: key.String, Totals: totals, Success: success,
			Errors: totals - success, Latency: stats,
		})
	}
	return out, rows.Err()
}

// topCap 归一化分组 TopN：<=0 用默认上限，超过上限按上限收敛。
func topCap(n int) int {
	if n <= 0 || n > trafficGroupCap {
		return trafficGroupCap
	}
	return n
}

// trafficGroupCap 是 by_tool / by_client_ip 的默认与最大返回条数。
const trafficGroupCap = 200

// QueryTraffic 分页查询调用日志（按 id 倒序）。
func (s *Store) QueryTraffic(ctx context.Context, q query.TrafficQuery) ([]model.TrafficSample, int, error) {
	cond := make([]string, 0, 7)
	args := make([]any, 0, 7)
	if q.ServerID != "" {
		cond = append(cond, "server_id = ?")
		args = append(args, q.ServerID)
	}
	if q.InstanceID != "" {
		cond = append(cond, "instance_id = ?")
		args = append(args, q.InstanceID)
	}
	if q.Status != "" {
		cond = append(cond, "status = ?")
		args = append(args, q.Status)
	}
	if q.ClientIP != "" {
		cond = append(cond, "client_ip = ?")
		args = append(args, q.ClientIP)
	}
	if q.Q != "" {
		cond = append(cond, "(tool LIKE ? OR client LIKE ? OR request_id LIKE ? OR client_ip LIKE ?)")
		pat := likePattern(q.Q)
		args = append(args, pat, pat, pat, pat)
	}
	if !q.From.IsZero() {
		cond = append(cond, "ts >= ?")
		args = append(args, fmtTimeUTC(q.From))
	}
	if !q.To.IsZero() {
		cond = append(cond, "ts <= ?")
		args = append(args, fmtTimeUTC(q.To))
	}
	total := 0
	if !q.SkipTotal {
		var err error
		total, err = s.countRows(ctx, "traffic_log", cond, args)
		if err != nil {
			return nil, 0, fmt.Errorf("count traffic: %w", err)
		}
	}

	sql := `SELECT ` + trafficListColumns + ` FROM traffic_log`
	if len(cond) > 0 {
		sql += " WHERE " + strings.Join(cond, " AND ")
	}
	sql += " ORDER BY id DESC"
	sql, qargs := paginate(sql, args, q.Paging)

	rows, err := s.db.QueryContext(ctx, sql, qargs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query traffic: %w", err)
	}
	defer rows.Close()

	out := make([]model.TrafficSample, 0)
	for rows.Next() {
		sample, err := scanTrafficList(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan traffic: %w", err)
		}
		out = append(out, sample)
	}
	return out, total, rows.Err()
}
