package router

import (
	"context"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// seedInstance 为 Server 下挂一条实例。
func seedInstance(t *testing.T, store *memory.Store, serverID, endpoint string, status model.ServerStatus) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateInstance(ctx, &model.Instance{
		ServerID:     serverID,
		Endpoint:     endpoint,
		Transport:    model.TransportStreamableHTTP,
		Enabled:      true,
		HealthStatus: status,
	}); err != nil {
		t.Fatalf("创建实例失败: %v", err)
	}
}

// newFixture 构造一个含单 Server（一条 health 状态实例）+ 单 Tool 的解析环境。
func newFixture(t *testing.T, health model.ServerStatus) *DefaultResolver {
	t.Helper()
	store := memory.New()
	ctx := context.Background()

	server := &model.Server{Name: "University", Enabled: true, HealthStatus: health}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("创建 Server 失败: %v", err)
	}
	seedInstance(t, store, server.ID, "http://upstream:9000", health)
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID:     server.ID,
		OriginalName: "search",
		GatewayName:  "university.search",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("创建 Tool 失败: %v", err)
	}
	return NewResolver(store, store).WithInstances(store)
}

func TestResolver_ResolvesCallableServer(t *testing.T) {
	r := newFixture(t, model.ServerStatusUnknown) // UNKNOWN 乐观可调
	resolved, err := r.Resolve(context.Background(), "university.search")
	if err != nil {
		t.Fatalf("Resolve 不应失败: %v", err)
	}
	if resolved.Server.ID == "" || resolved.Tool.OriginalName != "search" {
		t.Fatalf("解析结果不正确: %+v", resolved)
	}
	if len(resolved.Targets) != 1 || !resolved.Targets[0].Healthy {
		t.Fatalf("应解析出 1 个健康目标: %+v", resolved.Targets)
	}
}

func TestResolver_RejectsUnhealthyServer(t *testing.T) {
	r := newFixture(t, model.ServerStatusUnhealthy)
	_, err := r.Resolve(context.Background(), "university.search")
	if !errs.Is(err, errs.CodeRoute) {
		t.Fatalf("实例全不健康应返回 route_error，得到 %v", err)
	}
}

// TestResolver_MultiInstanceBuildsAllTargets 验证多实例解析出多候选，健康/未知入选。
func TestResolver_MultiInstanceBuildsAllTargets(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	if err := store.CreateServer(ctx, &model.Server{Name: "University", Enabled: true, HealthStatus: model.ServerStatusUnknown}); err != nil {
		t.Fatal(err)
	}
	server, _ := store.GetServer(ctx, "srv-1")
	for _, ep := range []string{"http://a:9000", "http://b:9000", "http://c:9000"} {
		if err := store.CreateInstance(ctx, &model.Instance{ServerID: server.ID, Endpoint: ep, Enabled: true, HealthStatus: model.ServerStatusUnknown}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertTool(ctx, &model.Tool{ServerID: server.ID, OriginalName: "search", GatewayName: "university.search", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithInstances(store)
	resolved, err := r.Resolve(ctx, "university.search")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.Targets) != 3 {
		t.Fatalf("应有 3 个实例候选，得到 %d", len(resolved.Targets))
	}
}

// TestResolver_DisabledInstanceExcluded 验证摘除（enabled=false）的实例不产生健康候选，
// 且全摘除时报错。
func TestResolver_DisabledInstanceExcluded(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	if err := store.CreateServer(ctx, &model.Server{Name: "University", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server, _ := store.GetServer(ctx, "srv-1")
	if err := store.CreateInstance(ctx, &model.Instance{ServerID: server.ID, Endpoint: "http://a:9000", Enabled: false, HealthStatus: model.ServerStatusUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTool(ctx, &model.Tool{ServerID: server.ID, OriginalName: "search", GatewayName: "university.search", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithInstances(store)
	if _, err := r.Resolve(ctx, "university.search"); !errs.Is(err, errs.CodeRoute) {
		t.Fatalf("实例全禁用应 route_error，得到 %v", err)
	}
}

func TestResolver_UnknownTool(t *testing.T) {
	r := newFixture(t, model.ServerStatusHealthy)
	_, err := r.Resolve(context.Background(), "unknown.find")
	if !errs.Is(err, errs.CodeRoute) {
		t.Fatalf("未知工具应返回 route_error，得到 %v", err)
	}
}

func TestResolver_DisabledTool(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	if err := store.CreateServer(ctx, &model.Server{Name: "Course", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	seedInstance(t, store, "srv-1", "http://b", model.ServerStatusHealthy)
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID:     "srv-1",
		OriginalName: "search",
		GatewayName:  "course.search",
		Enabled:      false,
	}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithInstances(store)
	if _, err := r.Resolve(ctx, "course.search"); err == nil {
		t.Fatal("禁用工具应拒绝调用")
	}
}

// routeFixture 构造双 Server 环境：A 拥有 university.search，B/C 为潜在路由目标。
func routeFixture(t *testing.T) (*memory.Store, *model.Server, *model.Server, *model.Server) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()
	newSrv := func(name string) *model.Server {
		s := &model.Server{Name: name, Enabled: true, HealthStatus: model.ServerStatusHealthy}
		if err := store.CreateServer(ctx, s); err != nil {
			t.Fatalf("CreateServer(%s): %v", name, err)
		}
		seedInstance(t, store, s.ID, "http://"+name, model.ServerStatusHealthy)
		return s
	}
	a, b, c := newSrv("univ"), newSrv("mirrorB"), newSrv("mirrorC")
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID: a.ID, OriginalName: "search", GatewayName: "univ.search", Enabled: true,
	}); err != nil {
		t.Fatalf("UpsertTool: %v", err)
	}
	return store, a, b, c
}

func TestResolver_RouteOverridesTargetServer(t *testing.T) {
	store, a, b, _ := routeFixture(t)
	ctx := context.Background()
	if err := store.CreateRoute(ctx, &model.Route{Name: "覆盖到B", ServerID: b.ID, ToolNames: []string{"univ.search"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithRoutes(store).WithInstances(store)
	resolved, err := r.Resolve(ctx, "univ.search")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Server.ID != b.ID {
		t.Fatalf("命中 Route 应覆盖到 B（%s），实际 %s", b.ID, resolved.Server.ID)
	}
	if resolved.Tool.ServerID != a.ID {
		t.Fatal("Resolved.Tool 仍应保留原归属（不变 gateway_name 语义）")
	}
}

func TestResolver_RouteIdentityIsNoOp(t *testing.T) {
	store, a, _, _ := routeFixture(t)
	ctx := context.Background()
	if err := store.CreateRoute(ctx, &model.Route{Name: "恒等", ServerID: a.ID, ToolNames: []string{"univ.search"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithRoutes(store).WithInstances(store)
	resolved, err := r.Resolve(ctx, "univ.search")
	if err != nil || resolved.Server.ID != a.ID {
		t.Fatalf("恒等 Route 不应改变目标: %+v / %v", resolved, err)
	}
}

func TestResolver_DisabledRouteIgnored(t *testing.T) {
	store, a, b, _ := routeFixture(t)
	ctx := context.Background()
	if err := store.CreateRoute(ctx, &model.Route{Name: "停用", ServerID: b.ID, ToolNames: []string{"univ.search"}, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithRoutes(store).WithInstances(store)
	resolved, err := r.Resolve(ctx, "univ.search")
	if err != nil || resolved.Server.ID != a.ID {
		t.Fatalf("停用 Route 应被忽略: %+v / %v", resolved, err)
	}
}

func TestResolver_RouteOverrideTargetUnavailable(t *testing.T) {
	store, _, b, _ := routeFixture(t)
	ctx := context.Background()
	// 目标 B 已被禁用（Server 级）。
	got, _ := store.GetServer(ctx, b.ID)
	got.Enabled = false
	_ = store.UpdateServer(ctx, got)
	if err := store.CreateRoute(ctx, &model.Route{Name: "到禁用B", ServerID: b.ID, ToolNames: []string{"univ.search"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithRoutes(store).WithInstances(store)
	if _, err := r.Resolve(ctx, "univ.search"); !errs.Is(err, errs.CodeRoute) {
		t.Fatalf("覆盖目标不可用应 route_error，得到 %v", err)
	}
}

func TestResolver_RouteMultiHitPicksEarliest(t *testing.T) {
	store, _, b, c := routeFixture(t)
	ctx := context.Background()
	base := time.Now().UTC()
	if err := store.CreateRoute(ctx, &model.Route{Name: "后来到C", ServerID: c.ID, ToolNames: []string{"univ.search"}, Enabled: true, CreatedAt: model.T(base.Add(2 * time.Minute))}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRoute(ctx, &model.Route{Name: "最早到B", ServerID: b.ID, ToolNames: []string{"univ.search"}, Enabled: true, CreatedAt: model.T(base)}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithRoutes(store).WithInstances(store)
	resolved, err := r.Resolve(ctx, "univ.search")
	if err != nil || resolved.Server.ID != b.ID {
		t.Fatalf("多命中应取最早的非恒等（B），实际 %+v / %v", resolved, err)
	}
}

func TestResolver_RouteTargetMissing(t *testing.T) {
	store, _, _, _ := routeFixture(t)
	ctx := context.Background()
	if err := store.CreateRoute(ctx, &model.Route{Name: "指向幽灵", ServerID: "ghost-server", ToolNames: []string{"univ.search"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store).WithRoutes(store).WithInstances(store)
	if _, err := r.Resolve(ctx, "univ.search"); !errs.Is(err, errs.CodeRoute) {
		t.Fatalf("覆盖目标缺失应 route_error，得到 %v", err)
	}
}
