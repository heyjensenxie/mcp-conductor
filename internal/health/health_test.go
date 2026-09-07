package health

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// countingChecker 返回固定健康结论并累计调用次数（按实例维度探测）。
type countingChecker struct {
	mu     sync.Mutex
	count  int
	status model.ServerStatus
}

func (c *countingChecker) Check(_ context.Context, _ model.Server, _ model.Instance) (Status, error) {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return c.status, nil
}

func (c *countingChecker) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// seedServer 写入一个带 seed 实例的测试 Server 并返回存储。
func seedServer(id string, enabled bool, status model.ServerStatus) *memory.Store {
	store := memory.New()
	ctx := context.Background()
	now := model.Now()
	_ = store.CreateServer(ctx, &model.Server{
		ID: id, Name: "mock", Enabled: enabled,
		HealthStatus: status, CreatedAt: now, UpdatedAt: now,
	})
	_ = store.CreateInstance(ctx, &model.Instance{
		ID: "inst-1", ServerID: id, Endpoint: "http://localhost:9000/mcp",
		Transport: model.TransportStreamableHTTP, Enabled: enabled,
		HealthStatus: model.ServerStatusUnknown, CreatedAt: now, UpdatedAt: now,
	})
	return store
}

// TestCheckServer_probesAndPersists 验证对单个 Server 的即时巡检会探测其启用实例
// 并写回实例健康 + Server 聚合健康。
func TestCheckServer_probesAndPersists(t *testing.T) {
	ctx := context.Background()
	store := seedServer("srv-1", true, model.ServerStatusUnknown)
	checker := &countingChecker{status: model.ServerStatusHealthy}
	m := NewMonitor(store, checker, time.Hour, time.Second)

	m.checkServer(ctx, "srv-1")

	if checker.calls() != 1 {
		t.Fatalf("应探测 1 次，实际 %d", checker.calls())
	}
	inst, err := store.GetInstance(ctx, "inst-1")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if inst.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("实例健康状态应为 healthy，得到 %s", inst.HealthStatus)
	}
	got, err := store.GetServer(ctx, "srv-1")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("Server 聚合健康应为 healthy，得到 %s", got.HealthStatus)
	}
}

// TestCheckServer_skipsDisabled 验证禁用/Disabled 状态的 Server 触发巡检会被跳过。
func TestCheckServer_skipsDisabled(t *testing.T) {
	ctx := context.Background()
	store := seedServer("srv-1", false, model.ServerStatusDisabled)
	checker := &countingChecker{status: model.ServerStatusHealthy}
	m := NewMonitor(store, checker, time.Hour, time.Second)

	m.checkServer(ctx, "srv-1")

	if checker.calls() != 0 {
		t.Fatalf("禁用 Server 不应被探测，实际 %d 次", checker.calls())
	}
}

// TestCheckServer_skipsDisabledInstance 验证启用的 Server 下被摘除（enabled=false）
// 的实例不会被探测。
func TestCheckServer_skipsDisabledInstance(t *testing.T) {
	ctx := context.Background()
	store := seedServer("srv-1", true, model.ServerStatusUnknown)
	inst, err := store.GetInstance(ctx, "inst-1")
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	inst.Enabled = false
	if err := store.UpdateInstance(ctx, inst); err != nil {
		t.Fatalf("摘除实例失败: %v", err)
	}
	checker := &countingChecker{status: model.ServerStatusHealthy}
	m := NewMonitor(store, checker, time.Hour, time.Second)

	m.checkServer(ctx, "srv-1")

	if checker.calls() != 0 {
		t.Fatalf("禁用实例不应被探测，实际 %d 次", checker.calls())
	}
}

// TestRun_consumesTriggerCheck 验证 TriggerCheck 的即时巡检请求会被 Run 消费并探活。
func TestRun_consumesTriggerCheck(t *testing.T) {
	store := seedServer("srv-1", true, model.ServerStatusUnknown)
	checker := &countingChecker{status: model.ServerStatusHealthy}
	m := NewMonitor(store, checker, time.Hour, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)
	m.TriggerCheck("srv-1")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.GetServer(context.Background(), "srv-1")
		if err == nil && got.HealthStatus == model.ServerStatusHealthy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("TriggerCheck 未被 Run 消费，健康状态未更新为 healthy")
}
