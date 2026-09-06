package balancer

import (
	"context"
	"errors"
	"testing"

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
