package mysql

import (
	"context"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// TestRuntimeConfigCRUD 验证 runtime_config 单行 upsert/get 往返（需 MYSQL_TEST_DSN
// 且已应用 0013 迁移）。blocklist 经 JSON 文本往返保持顺序。
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
		IPBlocklist: []string{"203.0.113.9", "10.0.0.0/8"},
		IPWhitelist: []string{"198.51.100.7", "172.16.0.0/12"},
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
