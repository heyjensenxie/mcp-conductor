package memory

import (
	"context"
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
		ClientIP: "203.0.113.9", Status: "success", LatencyMS: 12, Timestamp: time.Now().UTC(), RequestArgs: args,
	}); err != nil {
		t.Fatalf("AppendTraffic: %v", err)
	}
	if err := s.AppendTraffic(ctx, model.TrafficSample{
		RequestID: "req-2", ServerID: "srv-1", Tool: "demo", Status: "success",
		LatencyMS: 8, Timestamp: time.Now().UTC(),
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
