package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// TestRuntimeConfigCRUD 验证 runtime_config 单行 upsert/get 往返（需 MYSQL_TEST_DSN
// 且已应用 database/schema.sql）。blocklist 经 JSON 文本往返保持顺序。
func TestRuntimeConfigCRUD(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	cfg := &model.RuntimeConfig{
		RateLimit: model.RuntimeRateLimit{
			QPS: 10, Burst: 5, WindowSeconds: 2, IPQPS: 20, IPBurst: 10, GlobalQPS: 500, GlobalBurst: 100,
		},
		AutoBan: model.RuntimeAutoBan{
			Enabled: true, WindowSeconds: 30, MaxViolations: 3, BanSeconds: 120,
		},
		IPBlocklist:   []string{"203.0.113.9", "10.0.0.0/8"},
		IPWhitelist:   []string{"198.51.100.7", "172.16.0.0/12"},
		Observability: &model.RuntimeObservability{RecordArgs: true},
	}
	if err := store.PutRuntimeConfig(ctx, cfg); err != nil {
		t.Fatalf("PutRuntimeConfig: %v", err)
	}

	got, exists, err := store.GetRuntimeConfig(ctx)
	if err != nil || !exists {
		t.Fatalf("GetRuntimeConfig: exists=%v err=%v", exists, err)
	}
	if got.RateLimit != cfg.RateLimit {
		t.Fatalf("ratelimit 往返不一致: %+v", got.RateLimit)
	}
	if got.AutoBan != cfg.AutoBan {
		t.Fatalf("auto_ban 往返不一致: %+v", got.AutoBan)
	}
	if len(got.IPBlocklist) != 2 || got.IPBlocklist[0] != "203.0.113.9" || got.IPBlocklist[1] != "10.0.0.0/8" {
		t.Fatalf("blocklist 往返不一致: %v", got.IPBlocklist)
	}
	if len(got.IPWhitelist) != 2 || got.IPWhitelist[0] != "198.51.100.7" {
		t.Fatalf("whitelist 往返不一致: %v", got.IPWhitelist)
	}
	if got.Observability == nil || !got.Observability.RecordArgs {
		t.Fatalf("observability.record_args 往返不一致: %+v", got.Observability)
	}

	// 覆盖写（last-writer-wins）应更新为最新快照。
	if err := store.PutRuntimeConfig(ctx, &model.RuntimeConfig{
		RateLimit: model.RuntimeRateLimit{GlobalQPS: 999, GlobalBurst: 99},
	}); err != nil {
		t.Fatalf("PutRuntimeConfig 覆盖: %v", err)
	}
	after, _, err := store.GetRuntimeConfig(ctx)
	if err != nil {
		t.Fatalf("GetRuntimeConfig: %v", err)
	}
	if after.RateLimit.GlobalQPS != 999 || after.RateLimit.QPS != 0 || len(after.IPBlocklist) != 0 {
		t.Fatalf("覆盖后应反映最新快照: %+v", after)
	}
}

// TestPurgeTrafficArgs 验证清除入参只清 request_args，行与元数据保留。
func TestPurgeTrafficArgs(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, withArgs := range []bool{true, false, true} {
		sample := model.TrafficSample{
			ServerID:  "srv-1",
			Tool:      "mock.search",
			Status:    "success",
			LatencyMS: 3,
			Timestamp: model.T(now),
		}
		if withArgs {
			sample.RequestArgs = map[string]any{"q": "hello"}
		}
		if err := store.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	// 从列表取真实流水主键（AppendTraffic 不回填自增 id）。
	list, err := store.RecentTraffic(ctx, 10)
	if err != nil || len(list) != 3 {
		t.Fatalf("RecentTraffic: len=%d err=%v", len(list), err)
	}
	withArgsIDs := []int64{}
	for _, sample := range list {
		if sample.HasArgs {
			withArgsIDs = append(withArgsIDs, sample.ID)
		}
	}
	if len(withArgsIDs) != 2 {
		t.Fatalf("应有 2 行带参: %v", withArgsIDs)
	}

	if n, err := store.PurgeTrafficArgs(ctx); err != nil || n != 2 {
		t.Fatalf("应清除 2 行入参: n=%d err=%v", n, err)
	}
	if n2, _ := store.PurgeTrafficArgs(ctx); n2 != 0 {
		t.Fatalf("重复清除应返回 0, 得到 %d", n2)
	}

	// 行仍在、元数据保留；原带参行入参已清空。
	for _, id := range withArgsIDs {
		detail, err := store.GetTraffic(ctx, id)
		if err != nil {
			t.Fatalf("GetTraffic(%d) 应仍存在: %v", id, err)
		}
		if detail.RequestArgs != nil {
			t.Fatalf("清除后 GetTraffic(%d) 不应有入参: %+v", id, detail.RequestArgs)
		}
	}
}
