package balancer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

func healthyTarget(id string) Target {
	return Target{ID: id, ServerID: id, Endpoint: "http://" + id, Transport: model.TransportStreamableHTTP, Weight: 1, Healthy: true}
}

func TestRoundRobin_RotatesHealthTargets(t *testing.T) {
	lb := NewRoundRobin()
	targets := []Target{healthyTarget("a"), healthyTarget("b")}

	seen := make(map[string]int)
	for range 6 {
		got, err := lb.Pick(context.Background(), targets)
		if err != nil {
			t.Fatalf("Pick 不应失败: %v", err)
		}
		seen[got.ID]++
	}
	if seen["a"] != 3 || seen["b"] != 3 {
		t.Fatalf("RoundRobin 应均匀轮转，得到 %v", seen)
	}
}

func TestRoundRobin_SkipsUnhealthy(t *testing.T) {
	lb := NewRoundRobin()
	targets := []Target{healthyTarget("a"), {ID: "b", ServerID: "b", Healthy: false}}

	for range 3 {
		got, err := lb.Pick(context.Background(), targets)
		if err != nil {
			t.Fatalf("Pick 不应失败: %v", err)
		}
		if got.ID != "a" {
			t.Fatalf("应始终选健康实例 a，得到 %q", got.ID)
		}
	}
}

func TestRoundRobin_NoHealthyTargets(t *testing.T) {
	lb := NewRoundRobin()
	_, err := lb.Pick(context.Background(), []Target{{ID: "a", Healthy: false}})
	if !errors.Is(err, ErrNoAvailable) {
		t.Fatalf("全部不健康时应返回 ErrNoAvailable，得到 %v", err)
	}
}

// TestRoundRobin_FailureCoolingExcludes 验证 ReportFailure 后该实例在冷却期内
// 不再入选，流量转到其它健康实例（失败反馈回路的核心）。
func TestRoundRobin_FailureCoolingExcludes(t *testing.T) {
	lb := NewRoundRobin()
	targets := []Target{healthyTarget("a"), healthyTarget("b")}
	lb.ReportFailure("a")

	for range 4 {
		got, err := lb.Pick(context.Background(), targets)
		if err != nil {
			t.Fatalf("Pick 不应失败: %v", err)
		}
		if got.ID != "b" {
			t.Fatalf("冷却期内的实例 a 不应入选，得到 %q", got.ID)
		}
	}
}

// TestRoundRobin_AllCooledIsNoAvailable 验证全部候选都在冷却期时返回
// ErrNoAvailable（与全不健康语义一致）。
func TestRoundRobin_AllCooledIsNoAvailable(t *testing.T) {
	lb := NewRoundRobin()
	lb.ReportFailure("a")
	lb.ReportFailure("b")
	_, err := lb.Pick(context.Background(), []Target{healthyTarget("a"), healthyTarget("b")})
	if !errors.Is(err, ErrNoAvailable) {
		t.Fatalf("全部冷却时应返回 ErrNoAvailable，得到 %v", err)
	}
}

// TestRoundRobin_FailureCoolingExpires 验证冷却期结束（时钟前进）后实例恢复入选，
// 可由健康探活确认后放回。
func TestRoundRobin_FailureCoolingExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	lb := NewRoundRobin(WithFailureCooldown(5 * time.Second))
	lb.now = func() time.Time { return now }

	lb.ReportFailure("a")
	if got, err := lb.Pick(context.Background(), []Target{healthyTarget("a")}); err == nil {
		t.Fatalf("冷却期内不应返回 a，得到 %+v", got)
	}

	// 冷却期满：实例恢复候选。
	now = now.Add(6 * time.Second)
	got, err := lb.Pick(context.Background(), []Target{healthyTarget("a")})
	if err != nil {
		t.Fatalf("冷却期满后应恢复入选: %v", err)
	}
	if got.ID != "a" {
		t.Fatalf("应恢复到实例 a，得到 %q", got.ID)
	}
}

// TestRoundRobin_UnhealthyAndCooledNeverChosen 验证 disabled/unhealthy 与冷却
// 两类排除叠加：仅冷却期外的健康实例可选。
func TestRoundRobin_UnhealthyAndCooledNeverChosen(t *testing.T) {
	lb := NewRoundRobin()
	lb.ReportFailure("b")
	targets := []Target{
		{ID: "a", ServerID: "a", Healthy: false}, // 不健康
		healthyTarget("b"),                       // 健康但冷却中
		healthyTarget("c"),                       // 健康且未冷却
	}
	for range 3 {
		got, err := lb.Pick(context.Background(), targets)
		if err != nil {
			t.Fatalf("Pick 不应失败: %v", err)
		}
		if got.ID != "c" {
			t.Fatalf("应只选健康且未冷却的 c，得到 %q", got.ID)
		}
	}
}
