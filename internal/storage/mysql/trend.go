package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// ---- TrendStore ----
//
// trend_minute 存“已闭合分钟桶”：进程内指标聚合器每 60s 把闭合分钟幂等 upsert
// 到此（PK (scope,dim_key,minute) 防重），读侧作为长程权威源。表结构见 database/schema.sql。

// UpsertTrendBuckets 幂等写入已闭合分钟桶（同键存在则覆盖计数，重复 flush 无害）。
func (s *Store) UpsertTrendBuckets(ctx context.Context, buckets []model.TrendMinute) error {
	if len(buckets) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`INSERT INTO trend_minute (scope, server_id, dim_key, minute, totals, errors) VALUES `)
	args := make([]any, 0, len(buckets)*6)
	for i, bk := range buckets {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`(?,?,?,?,?,?)`)
		args = append(args, bk.Scope, bk.ServerID, bk.DimKey, bk.Minute, bk.Totals, bk.Errors)
	}
	b.WriteString(` ON DUPLICATE KEY UPDATE totals = VALUES(totals), errors = VALUES(errors)`)
	if _, err := s.db.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("upsert trend_minute: %w", err)
	}
	return nil
}

// QueryTrendBuckets 按 query.TrendQuery 读取窗口内的分钟桶（minute 升序）。
// scope=server/instance 时 server_id 可作归属过滤；dim_key 非空则只读单维。
func (s *Store) QueryTrendBuckets(ctx context.Context, q query.TrendQuery) ([]model.TrendMinute, error) {
	cond := []string{"scope = ?"}
	args := []any{q.Scope}
	if q.ServerID != "" {
		cond = append(cond, "server_id = ?")
		args = append(args, q.ServerID)
	}
	if q.DimKey != "" {
		cond = append(cond, "dim_key = ?")
		args = append(args, q.DimKey)
	}
	cond = append(cond, "minute BETWEEN ? AND ?")
	args = append(args, q.From, q.To)

	sql := `SELECT scope, server_id, dim_key, minute, totals, errors FROM trend_minute
	        WHERE ` + strings.Join(cond, " AND ") + ` ORDER BY minute ASC`
	rows, err := s.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query trend_minute: %w", err)
	}
	defer rows.Close()

	out := make([]model.TrendMinute, 0)
	for rows.Next() {
		var bucket model.TrendMinute
		if err := rows.Scan(&bucket.Scope, &bucket.ServerID, &bucket.DimKey,
			&bucket.Minute, &bucket.Totals, &bucket.Errors); err != nil {
			return nil, fmt.Errorf("scan trend_minute: %w", err)
		}
		out = append(out, bucket)
	}
	return out, rows.Err()
}

// DeleteTrendBucketsBefore 清理 minute < before 的旧桶（保留天数收敛）。
func (s *Store) DeleteTrendBucketsBefore(ctx context.Context, beforeMinute int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM trend_minute WHERE minute < ?`, beforeMinute); err != nil {
		return fmt.Errorf("delete trend_minute: %w", err)
	}
	return nil
}
