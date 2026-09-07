package memory

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// 覆盖本轮新增：traffic 主键 id / 入参捕获往返（列表剥离、详情带出）、
// trend 分钟桶幂等 upsert + 读取 + 清理。

func TestAppendTrafficAssignsIDAndKeepsArgs(t *testing.T) {
	s := New()
	ctx := context.Background()
	args := map[string]any{"q": "hello", "limit": float64(3)}

	if err := s.AppendTraffic(ctx, model.TrafficSample{
		RequestID: "req-1", ServerID: "srv-1", InstanceID: "inst-1", Tool: "demo",
		ClientIP: "203.0.113.9", Status: "success", LatencyMS: 12, Timestamp: model.Now(), RequestArgs: args,
	}); err != nil {
		t.Fatalf("AppendTraffic: %v", err)
	}
	if err := s.AppendTraffic(ctx, model.TrafficSample{
		RequestID: "req-2", ServerID: "srv-1", Tool: "demo", Status: "success",
		LatencyMS: 8, Timestamp: model.Now(),
	}); err != nil {
		t.Fatalf("AppendTraffic: %v", err)
	}

	// 详情按 id 读回：ID 单调自增、入参完整往返、ClientIP 保留。
	got, err := s.GetTraffic(ctx, 1)
	if err != nil {
		t.Fatalf("GetTraffic: %v", err)
	}
	if got.ID != 1 || !reflect.DeepEqual(got.RequestArgs, args) || got.ClientIP != "203.0.113.9" {
		t.Fatalf("详情回读不一致: %+v", got)
	}

	// 缺失 id 报错。
	if _, err := s.GetTraffic(ctx, 99); err == nil {
		t.Fatal("GetTraffic 缺失 id 应报错")
	}

	// 列表（QueryTraffic）最新优先、置 HasArgs、但不得泄露 RequestArgs 大列。
	rows, total, err := s.QueryTraffic(ctx, query.TrafficQuery{})
	if err != nil {
		t.Fatalf("QueryTraffic: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("QueryTraffic total=%d len=%d", total, len(rows))
	}
	if rows[0].ID != 2 || rows[0].HasArgs || rows[0].ClientIP != "" {
		t.Fatalf("最新行 id=%d HasArgs=%v ClientIP=%q", rows[0].ID, rows[0].HasArgs, rows[0].ClientIP)
	}
	if !rows[1].HasArgs {
		t.Fatal("含入参的行 HasArgs 应为 true")
	}
	if rows[1].RequestArgs != nil {
		t.Fatal("列表不得携带 RequestArgs（隐私）")
	}
	if rows[1].ClientIP != "203.0.113.9" {
		t.Fatalf("列表应保留 ClientIP, 得到 %q", rows[1].ClientIP)
	}
}

func TestTrendUpsertQueryDelete(t *testing.T) {
	s := New()
	ctx := context.Background()

	in := []model.TrendMinute{
		{Scope: "tool", DimKey: "demo", Minute: 100, Totals: 5, Errors: 1},
		{Scope: "tool", DimKey: "demo", Minute: 101, Totals: 2},
		{Scope: "server", ServerID: "srv-1", DimKey: "srv-1", Minute: 101, Totals: 2},
		{Scope: "instance", ServerID: "srv-1", DimKey: "inst-1", Minute: 101, Totals: 2},
	}
	if err := s.UpsertTrendBuckets(ctx, in); err != nil {
		t.Fatalf("UpsertTrendBuckets: %v", err)
	}
	// 同键重复 flush 幂等覆盖（minute=100 改 totals=6/errors=2）。
	if err := s.UpsertTrendBuckets(ctx, []model.TrendMinute{
		{Scope: "tool", DimKey: "demo", Minute: 100, Totals: 6, Errors: 2},
	}); err != nil {
		t.Fatalf("UpsertTrendBuckets: %v", err)
	}

	// 窗口读取 + 升序。
	got, err := s.QueryTrendBuckets(ctx, query.TrendQuery{Scope: "tool", From: 0, To: 500})
	if err != nil {
		t.Fatalf("QueryTrendBuckets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tool 行数=%d, want 2", len(got))
	}
	if got[0].Minute != 100 || got[0].Totals != 6 || got[0].Errors != 2 {
		t.Fatalf("幂等覆盖未生效: %+v", got[0])
	}

	// scope + server 过滤（instance 维按归属 Server 收敛）。
	inst, err := s.QueryTrendBuckets(ctx, query.TrendQuery{Scope: "instance", ServerID: "srv-1", From: 0, To: 500})
	if err != nil {
		t.Fatalf("QueryTrendBuckets: %v", err)
	}
	if len(inst) != 1 || inst[0].DimKey != "inst-1" {
		t.Fatalf("instance 过滤结果=%+v", inst)
	}

	// 清理 minute < 101 → 仅保留 minute=101。
	if err := s.DeleteTrendBucketsBefore(ctx, 101); err != nil {
		t.Fatalf("DeleteTrendBucketsBefore: %v", err)
	}
	after, err := s.QueryTrendBuckets(ctx, query.TrendQuery{Scope: "tool", From: 0, To: 500})
	if err != nil {
		t.Fatalf("QueryTrendBuckets: %v", err)
	}
	if len(after) != 1 || after[0].Minute != 101 {
		t.Fatalf("清理后残留=%+v", after)
	}
}

// TestDeleteTrafficBefore 覆盖调用日志保留清理：分块 limit 语义、limit<=0 全删、计数。
func TestDeleteTrafficBefore(t *testing.T) {
	s := New()
	ctx := context.Background()
	base := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Millisecond)
	stamps := []time.Time{base, base.Add(time.Hour), base.Add(2 * time.Hour)}
	for i, ts := range stamps {
		if err := s.AppendTraffic(ctx, model.TrafficSample{
			RequestID: fmt.Sprintf("req-%d", i+1), Tool: "demo", Status: "success",
			LatencyMS: int64(i + 1), Timestamp: model.T(ts),
		}); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	// 保留最近 1.5h：会清除 base 与 base+1h，保留 base+2h。
	cutoff := base.Add(90 * time.Minute)

	// 分块删除（limit=1）：三条旧行分两轮删，第三轮无可删应返回 0。
	for i := 0; i < 2; i++ {
		n, err := s.DeleteTrafficBefore(ctx, cutoff, 1)
		if err != nil {
			t.Fatalf("DeleteTrafficBefore(limit=1) 第%d次: %v", i+1, err)
		}
		if n != 1 {
			t.Fatalf("第%d次应删 1 条，得到 %d", i+1, n)
		}
	}
	if n, err := s.DeleteTrafficBefore(ctx, cutoff, 1); err != nil || n != 0 {
		t.Fatalf("无可删时应返回 0，got n=%d err=%v", n, err)
	}

	rows, total, err := s.QueryTraffic(ctx, query.TrafficQuery{})
	if err != nil {
		t.Fatalf("QueryTraffic: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].RequestID != "req-3" {
		t.Fatalf("分块清理后应仅剩 req-3，total=%d got=%+v", total, rows)
	}

	// limit<=0：删除全部匹配（不设上限）。
	if err := s.AppendTraffic(ctx, model.TrafficSample{
		RequestID: "req-4", Tool: "demo", Status: "success", Timestamp: model.T(base),
	}); err != nil {
		t.Fatalf("AppendTraffic: %v", err)
	}
	if err := s.AppendTraffic(ctx, model.TrafficSample{
		RequestID: "req-5", Tool: "demo", Status: "success", Timestamp: model.Now(),
	}); err != nil {
		t.Fatalf("AppendTraffic: %v", err)
	}
	if n, err := s.DeleteTrafficBefore(ctx, base.Add(time.Hour), 0); err != nil || n != 1 {
		t.Fatalf("limit=0 应删全部匹配(1条)，n=%d err=%v", n, err)
	}
	if n, err := s.DeleteTrafficBefore(ctx, base.Add(time.Hour), 0); err != nil || n != 0 {
		t.Fatalf("再次删除应为 0，n=%d err=%v", n, err)
	}
}
