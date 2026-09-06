package memory

import (
	"context"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

func TestRuntimeConfigRoundTrip(t *testing.T) {
	s := New()
	ctx := context.Background()

	// 初始无保存值。
	if _, exists, err := s.GetRuntimeConfig(ctx); err != nil || exists {
		t.Fatalf("初始应无保存值: exists=%v err=%v", exists, err)
	}

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
	if err := s.PutRuntimeConfig(ctx, cfg); err != nil {
		t.Fatalf("PutRuntimeConfig: %v", err)
	}

	got, exists, err := s.GetRuntimeConfig(ctx)
	if err != nil || !exists {
		t.Fatalf("GetRuntimeConfig: exists=%v err=%v", exists, err)
	}
	if got.RateLimit != cfg.RateLimit {
		t.Fatalf("ratelimit 往返不一致: %+v", got.RateLimit)
	}
	if got.AutoBan != cfg.AutoBan {
		t.Fatalf("auto_ban 往返不一致: %+v", got.AutoBan)
	}
	if len(got.IPBlocklist) != 2 || got.IPBlocklist[1] != "10.0.0.0/8" {
		t.Fatalf("blocklist 往返不一致: %v", got.IPBlocklist)
	}
	if len(got.IPWhitelist) != 2 || got.IPWhitelist[0] != "198.51.100.7" {
		t.Fatalf("whitelist 往返不一致: %v", got.IPWhitelist)
	}

	// 读副本隔离：改动返回副本不应影响存储内数据。
	got.IPBlocklist[0] = "mutated"
	again, _, _ := s.GetRuntimeConfig(ctx)
	if again.IPBlocklist[0] != "203.0.113.9" {
		t.Fatalf("Get 应返回独立副本, 得到 %v", again.IPBlocklist)
	}
}
