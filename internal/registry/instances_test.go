package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeProber 模拟实例健康探针：按预设结论返回并支持注入探测错误。
type fakeProber struct {
	status model.ServerStatus
	err    error
}

func (f *fakeProber) Check(_ context.Context, _ model.Server, _ model.Instance) (model.ServerStatus, error) {
	return f.status, f.err
}

// newInstanceTestService 构造一个装配好 prober 的 Service（discoverer 返回空工具）。
func newInstanceTestService(prober *fakeProber) (*memory.Store, *Service) {
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})
	if prober != nil {
		svc = svc.WithProber(prober)
	}
	return store, svc
}

// TestAddInstanceAndListOrdering 验证新增实例后列表按 (created_at, id) 升序，
// 首条即 CreateServer 创建的 seed（主实例）。
func TestAddInstanceAndListOrdering(t *testing.T) {
	ctx := context.Background()
	_, svc := newInstanceTestService(nil)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	second, err := svc.AddInstance(ctx, created.ID, "http://second:9000/mcp", model.TransportStreamableHTTP)
	if err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	instances, err := svc.ListInstances(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("应有两个实例，得到 %d", len(instances))
	}
	// 主实例 = seed（升序首条）；新增实例排后。
	if instances[0].Endpoint != "http://seed:9000/mcp" {
		t.Fatalf("主实例应为 seed，得到 %+v", instances[0])
	}
	if instances[1].ID != second.ID {
		t.Fatalf("次实例应为新增的 %q，得到 %+v", second.ID, instances[1])
	}
}

// TestToggleInstanceTogglesAndUpdatesAggregate 验证实例启停落库并触发 Server 聚合
// 健康重算。
func TestToggleInstanceTogglesAndUpdatesAggregate(t *testing.T) {
	ctx := context.Background()
	store, svc := newInstanceTestService(nil)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	// CreateServer 内空发现成功 → seed 实例 healthy，Server 聚合 healthy。
	if got, _ := store.GetServer(ctx, created.ID); got.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("CreateServer 后聚合应 healthy，得到 %s", got.HealthStatus)
	}

	second, err := svc.AddInstance(ctx, created.ID, "http://second:9000/mcp", model.TransportStreamableHTTP)
	if err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	// 摘除主实例 → 只剩新增的 unknown 实例 → 聚合 unknown。
	if _, err := svc.ToggleInstance(ctx, created.ID, instancesByEndpoint(t, store, created.ID, "http://seed:9000/mcp"), false); err != nil {
		t.Fatalf("ToggleInstance(false): %v", err)
	}
	if got, _ := store.GetServer(ctx, created.ID); got.HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("摘除唯一 healthy 实例后聚合应 unknown，得到 %s", got.HealthStatus)
	}

	// 重新启用主实例 → 聚合恢复 healthy。
	if _, err := svc.ToggleInstance(ctx, created.ID, instancesByEndpoint(t, store, created.ID, "http://seed:9000/mcp"), true); err != nil {
		t.Fatalf("ToggleInstance(true): %v", err)
	}
	if got, _ := store.GetInstance(ctx, second.ID); !got.Enabled {
		t.Fatalf("新实例应保持启用: %+v", got)
	}
	if got, _ := store.GetServer(ctx, created.ID); got.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("恢复后聚合应 healthy，得到 %s", got.HealthStatus)
	}
}

// TestDeleteInstanceLastInstanceGuard 验证删除最后一个实例被拒绝（CodeInvalidArgument）
// 且 Server 与实例保持完整。
func TestDeleteInstanceLastInstanceGuard(t *testing.T) {
	ctx := context.Background()
	store, svc := newInstanceTestService(nil)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	seedID := instancesByEndpoint(t, store, created.ID, "http://seed:9000/mcp")

	// 只有一个实例 → 拒绝删除。
	if err := svc.DeleteInstance(ctx, created.ID, seedID); !errs.Is(err, errs.CodeInvalidArgument) {
		t.Fatalf("删除最后实例应返回 invalid_argument，得到 %v", err)
	}
	if insts, _ := store.ListInstancesByServer(ctx, created.ID); len(insts) != 1 {
		t.Fatalf("拒绝后 Server 仍应保留 1 个实例，得到 %d", len(insts))
	}

	// 新增第二个实例后可删除其中一个。
	if _, err := svc.AddInstance(ctx, created.ID, "http://second:9000/mcp", model.TransportStreamableHTTP); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}
	if err := svc.DeleteInstance(ctx, created.ID, seedID); err != nil {
		t.Fatalf("删除非最后实例应成功: %v", err)
	}
	if insts, _ := store.ListInstancesByServer(ctx, created.ID); len(insts) != 1 {
		t.Fatalf("删除后应剩 1 个实例，得到 %d", len(insts))
	}
	if _, err := store.GetServer(ctx, created.ID); err != nil {
		t.Fatalf("Server 应保持存在: %v", err)
	}
}

// TestUpdateInstanceResetsHealth 验证更新 endpoint/transport 后该实例健康复位
// 为 unknown 并重算聚合。
func TestUpdateInstanceResetsHealth(t *testing.T) {
	ctx := context.Background()
	store, svc := newInstanceTestService(nil)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	seedID := instancesByEndpoint(t, store, created.ID, "http://seed:9000/mcp")
	if got, _ := store.GetInstance(ctx, seedID); got.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("CreateServer 空发现成功后 seed 应为 healthy，得到 %s", got.HealthStatus)
	}

	endpoint := "http://moved:9000/mcp"
	upd, err := svc.UpdateInstance(ctx, created.ID, seedID, UpdateInstancePatch{Endpoint: &endpoint})
	if err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}
	if upd.HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("端点变更后实例健康应复位 unknown，得到 %s", upd.HealthStatus)
	}
	if got, _ := store.GetInstance(ctx, seedID); got.Endpoint != endpoint || got.HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("实例落库不一致: %+v", got)
	}
	// 唯一实例复位为 unknown → Server 聚合也应 unknown。
	if got, _ := store.GetServer(ctx, created.ID); got.HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("聚合应随实例复位为 unknown，得到 %s", got.HealthStatus)
	}
}

// TestTestInstanceWithProber 验证实例测试：healthy prober 回写健康并更新聚合。
func TestTestInstanceWithProber(t *testing.T) {
	ctx := context.Background()
	prober := &fakeProber{status: model.ServerStatusHealthy}
	store, svc := newInstanceTestService(prober)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	second, err := svc.AddInstance(ctx, created.ID, "http://second:9000/mcp", model.TransportStreamableHTTP)
	if err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	got, err := svc.TestInstance(ctx, created.ID, second.ID)
	if err != nil {
		t.Fatalf("TestInstance(healthy): %v", err)
	}
	if got.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("探测后实例应为 healthy，得到 %s", got.HealthStatus)
	}
	if stored, _ := store.GetInstance(ctx, second.ID); stored.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("探测结果应落库，得到 %s", stored.HealthStatus)
	}
	if srv, _ := store.GetServer(ctx, created.ID); srv.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("聚合应随探测结果更新为 healthy，得到 %s", srv.HealthStatus)
	}
}

// TestTestInstanceUnhealthyProberPersists 验证探测失败时实例被标 unhealthy 并回传上游错误。
func TestTestInstanceUnhealthyProberPersists(t *testing.T) {
	ctx := context.Background()
	prober := &fakeProber{err: errors.New("握手失败")}
	store, svc := newInstanceTestService(prober)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	seedID := instancesByEndpoint(t, store, created.ID, "http://seed:9000/mcp")

	_, err = svc.TestInstance(ctx, created.ID, seedID)
	if !errs.Is(err, errs.CodeUpstream) {
		t.Fatalf("探测失败应返回 upstream_error，得到 %v", err)
	}
	if got, _ := store.GetInstance(ctx, seedID); got.HealthStatus != model.ServerStatusUnhealthy {
		t.Fatalf("探测失败实例应标 unhealthy，得到 %s", got.HealthStatus)
	}
	if srv, _ := store.GetServer(ctx, created.ID); srv.HealthStatus != model.ServerStatusUnhealthy {
		t.Fatalf("聚合应更新为 unhealthy，得到 %s", srv.HealthStatus)
	}
}

// TestDeleteServerRemovesInstances 验证删除 Server 会连带删除其实例。
func TestDeleteServerRemovesInstances(t *testing.T) {
	ctx := context.Background()
	store, svc := newInstanceTestService(nil)

	created, err := svc.CreateServer(ctx, CreateServerInput{Name: "Mock", Endpoint: "http://seed:9000/mcp", Transport: model.TransportStreamableHTTP})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if _, err := svc.AddInstance(ctx, created.ID, "http://second:9000/mcp", model.TransportStreamableHTTP); err != nil {
		t.Fatalf("AddInstance: %v", err)
	}

	if err := svc.DeleteServer(ctx, created.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if insts, _ := store.ListInstancesByServer(ctx, created.ID); len(insts) != 0 {
		t.Fatalf("删除 Server 后其实例应清空，得到 %d", len(insts))
	}
}

// instancesByEndpoint 在 Server 实例列表中按 endpoint 反查实例 id。
func instancesByEndpoint(t *testing.T, store *memory.Store, serverID, endpoint string) string {
	t.Helper()
	insts, err := store.ListInstancesByServer(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListInstancesByServer: %v", err)
	}
	for _, inst := range insts {
		if inst.Endpoint == endpoint {
			return inst.ID
		}
	}
	t.Fatalf("Server %q 下未找到 endpoint %q 的实例: %+v", serverID, endpoint, insts)
	return ""
}
