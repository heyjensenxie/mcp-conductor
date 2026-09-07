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
		IPBlocklist:   []string{"203.0.113.9", "10.0.0.0/8"},
		IPWhitelist:   []string{"198.51.100.7", "172.16.0.0/12"},
		Observability: &model.RuntimeObservability{RecordArgs: true},
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
	if got.Observability == nil || !got.Observability.RecordArgs {
		t.Fatalf("observability.record_args 往返不一致: %+v", got.Observability)
	}

	// 读副本隔离：改动返回副本不应影响存储内数据。
	got.IPBlocklist[0] = "mutated"
	got.Observability.RecordArgs = false
	again, _, _ := s.GetRuntimeConfig(ctx)
	if again.IPBlocklist[0] != "203.0.113.9" {
		t.Fatalf("Get 应返回独立副本, 得到 %v", again.IPBlocklist)
	}
	if again.Observability == nil || !again.Observability.RecordArgs {
		t.Fatalf("Observability 应返回独立副本, 得到 %+v", again.Observability)
	}
}

// TestPurgeTrafficArgs 验证清除入参只清 request_args，行与元数据保留。
func TestPurgeTrafficArgs(t *testing.T) {
	s := New()
	ctx := context.Background()
	now := model.Now()
	for i, withArgs := range []bool{true, false, true} {
		sample := model.TrafficSample{
			ServerID:  "srv-1",
			Tool:      "mock.search",
			Status:    "success",
			LatencyMS: 3,
			Timestamp: now,
		}
		if withArgs {
			sample.RequestArgs = map[string]any{"q": "hello"}
		}
		if err := s.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic[%d]: %v", i, err)
		}
	}

	n, err := s.PurgeTrafficArgs(ctx)
	if err != nil {
		t.Fatalf("PurgeTrafficArgs: %v", err)
	}
	if n != 2 {
		t.Fatalf("应清除 2 行入参, 得到 %d", n)
	}

	// 二次清除幂等：无入参可清，返回 0。
	if n2, _ := s.PurgeTrafficArgs(ctx); n2 != 0 {
		t.Fatalf("重复清除应返回 0, 得到 %d", n2)
	}
	// 行仍在，且入参已清空（回放不可用）。
	list, err := s.RecentTraffic(ctx, 10)
	if err != nil || len(list) != 3 {
		t.Fatalf("清除后行应保留: len=%d err=%v", len(list), err)
	}
	for _, sample := range list {
		if sample.HasArgs {
			t.Fatalf("清除后应 has_args=false: %+v", sample)
		}
	}
}
