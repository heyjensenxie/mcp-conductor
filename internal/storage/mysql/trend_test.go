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

// 集成测试（需 MYSQL_TEST_DSN 且已应用 database/schema.sql）：
//   - traffic 入参捕获往返：列表 has_args 为真且不泄入参；GetTraffic 按 id 带出入参；
//   - trend 分钟桶幂等 upsert / 窗口读取 / 保留清理。

func TestTrafficRequestArgsRoundTrip(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	marker := fmt.Sprintf("reqargs-%d", time.Now().UnixNano())
	args := map[string]any{"q": "hello", "limit": float64(3)}

	if err := store.AppendTraffic(ctx, model.TrafficSample{
		RequestID: marker, ServerID: "srv-it", Tool: "demo", Client: "it",
		ClientIP: "203.0.113.9", Status: "success", LatencyMS: 9, Timestamp: model.Now(), RequestArgs: args,
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
	if rows[0].ClientIP != "203.0.113.9" {
		t.Fatalf("列表应保留 client_ip, 得到 %q", rows[0].ClientIP)
	}

	got, err := store.GetTraffic(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("GetTraffic: %v", err)
	}
	if !reflect.DeepEqual(got.RequestArgs, args) {
		t.Fatalf("入参未往返: %+v", got.RequestArgs)
	}
	if got.ClientIP != "203.0.113.9" {
		t.Fatalf("详情应保留 client_ip, 得到 %q", got.ClientIP)
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

// TestDeleteTrafficBefore 覆盖调用日志保留清理：分块 limit 语义与最近行保留
// （MySQL 端 DELETE ... LIMIT 收敛）。traffic 为纯流水，测试启动先清残村保证可重放。
func TestDeleteTrafficBefore(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	// 清理可能残留，保证断言可重放（traffic 为纯流水表）。
	if _, err := store.DeleteTrafficBefore(ctx, time.Now().UTC(), 0); err != nil {
		t.Fatalf("清理残留: %v", err)
	}

	old := time.Now().UTC().Add(-48 * time.Hour)
	oldReq := fmt.Sprintf("old-%d", time.Now().UnixNano())
	recentReq := fmt.Sprintf("recent-%d", time.Now().UnixNano())
	for _, r := range []struct {
		req string
		ts  time.Time
	}{
		{oldReq + "-1", old.Add(time.Hour)},
		{oldReq + "-2", old},
		{recentReq, time.Now().UTC()},
	} {
		if err := store.AppendTraffic(ctx, model.TrafficSample{
			RequestID: r.req, Tool: "it-retention", Status: "success", LatencyMS: 1, Timestamp: model.T(r.ts),
		}); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	cutoff := time.Now().UTC().Add(-24 * time.Hour) // 只应清除两条 old 行

	// 分块（limit=1）分两轮收敛。
	if n, err := store.DeleteTrafficBefore(ctx, cutoff, 1); err != nil || n != 1 {
		t.Fatalf("第1次应删 1 条，n=%d err=%v", n, err)
	}
	if n, err := store.DeleteTrafficBefore(ctx, cutoff, 1); err != nil || n != 1 {
		t.Fatalf("第2次应删 1 条，n=%d err=%v", n, err)
	}

	if _, total, err := store.QueryTraffic(ctx, query.TrafficQuery{Q: oldReq}); err != nil || total != 0 {
		t.Fatalf("旧行应被清空，total=%d err=%v", total, err)
	}
	if _, total, err := store.QueryTraffic(ctx, query.TrafficQuery{Q: recentReq}); err != nil || total != 1 {
		t.Fatalf("最近行应保留，total=%d err=%v", total, err)
	}

	// 收尾收敛：清掉仍早于 now 的测试行（测试库为专用库，宽清理无害）。
	if _, err := store.DeleteTrafficBefore(ctx, time.Now().UTC(), 0); err != nil {
		t.Fatalf("测试收尾清理: %v", err)
	}
}
