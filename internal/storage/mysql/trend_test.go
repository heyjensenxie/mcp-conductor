package mysql

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// 集成测试（需 MYSQL_TEST_DSN 且已应用 0010/0011 迁移）：
//   - traffic 入参捕获往返：列表 has_args 为真且不泄入参；GetTraffic 按 id 带出入参；
//   - trend 分钟桶幂等 upsert / 窗口读取 / 保留清理。

func TestTrafficRequestArgsRoundTrip(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	marker := fmt.Sprintf("reqargs-%d", time.Now().UnixNano())
	args := map[string]any{"q": "hello", "limit": float64(3)}

	if err := store.AppendTraffic(ctx, model.TrafficSample{
		RequestID: marker, ServerID: "srv-it", Tool: "demo", Client: "it",
		Status: "success", LatencyMS: 9, Timestamp: time.Now().UTC(), RequestArgs: args,
	}); err != nil {
		t.Fatalf("AppendTraffic: %v", err)
	}

	rows, total, err := store.QueryTraffic(ctx, query.TrafficQuery{Q: marker})
	if err != nil {
		t.Fatalf("QueryTraffic: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("QueryTraffic total=%d len=%d", total, len(rows))
	}
	if !rows[0].HasArgs {
		t.Fatal("含入参的行 has_args 应为 true")
	}
	if rows[0].RequestArgs != nil {
		t.Fatal("列表不得携带 RequestArgs（隐私）")
	}

	got, err := store.GetTraffic(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("GetTraffic: %v", err)
	}
	if !reflect.DeepEqual(got.RequestArgs, args) {
		t.Fatalf("入参未往返: %+v", got.RequestArgs)
	}
	if _, err := store.GetTraffic(ctx, -1); err == nil {
		t.Fatal("GetTraffic 缺失 id 应报错")
	}
}

func TestTrendMinuteCRUD(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Minute).Unix()

	// 清理可能残留，保证断言可重放。
	if err := store.DeleteTrendBucketsBefore(ctx, base); err != nil {
		t.Fatalf("清理残留: %v", err)
	}

	in := []model.TrendMinute{
		{Scope: "tool", DimKey: "demo", Minute: base, Totals: 5, Errors: 1},
		{Scope: "server", ServerID: "srv-it", DimKey: "srv-it", Minute: base, Totals: 5},
		{Scope: "instance", ServerID: "srv-it", DimKey: "inst-it", Minute: base, Totals: 5},
	}
	if err := store.UpsertTrendBuckets(ctx, in); err != nil {
		t.Fatalf("UpsertTrendBuckets: %v", err)
	}
	// 同键重复 flush → 幂等覆盖 totals/errors。
	if err := store.UpsertTrendBuckets(ctx, []model.TrendMinute{
		{Scope: "tool", DimKey: "demo", Minute: base, Totals: 6, Errors: 2},
	}); err != nil {
		t.Fatalf("UpsertTrendBuckets: %v", err)
	}

	got, err := store.QueryTrendBuckets(ctx, query.TrendQuery{Scope: "tool", From: base, To: base})
	if err != nil {
		t.Fatalf("QueryTrendBuckets: %v", err)
	}
	if len(got) != 1 || got[0].Totals != 6 || got[0].Errors != 2 {
		t.Fatalf("tool 桶回读=%+v", got)
	}

	inst, err := store.QueryTrendBuckets(ctx, query.TrendQuery{Scope: "instance", ServerID: "srv-it", From: base, To: base})
	if err != nil {
		t.Fatalf("QueryTrendBuckets instance: %v", err)
	}
	if len(inst) != 1 || inst[0].DimKey != "inst-it" {
		t.Fatalf("instance 过滤=%+v", inst)
	}

	if err := store.DeleteTrendBucketsBefore(ctx, base+1); err != nil {
		t.Fatalf("DeleteTrendBucketsBefore: %v", err)
	}
	after, err := store.QueryTrendBuckets(ctx, query.TrendQuery{Scope: "tool", From: base, To: base})
	if err != nil {
		t.Fatalf("QueryTrendBuckets after delete: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("清理未生效: %+v", after)
	}
}
