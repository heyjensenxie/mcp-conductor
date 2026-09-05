package registry

import (
	"context"
	"testing"
	"time"

	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

// fakeDiscoverer 返回固定的工具定义，用于验证发现与命名空间逻辑。
type fakeDiscoverer struct {
	tools []DiscoveredTool
	err   error
}

func (f *fakeDiscoverer) Discover(_ context.Context, _ model.Server) ([]DiscoveredTool, error) {
	return f.tools, f.err
}

func TestNamespaceFor(t *testing.T) {
	cases := map[string]string{
		"University MCP": "university_mcp",
		"Course":         "course",
		"knowledge":      "knowledge",
		"":               "server",
		"知识服务":           "server", // 非字母数字回退，防命名冲突
	}
	for in, want := range cases {
		if got := namespaceFor(in); got != want {
			t.Errorf("namespaceFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestToolNameCollisionNamespaced 验证两个 Server 暴露同名工具时，
// 聚合后的 GatewayName 不发生冲突（PRD 核心关注点）。
func TestToolNameCollisionNamespaced(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{
		tools: []DiscoveredTool{{Name: "search", Description: "查询"}, {Name: "detail", Description: "详情"}},
	})

	if _, err := svc.CreateServer(ctx, &model.Server{Name: "University", Endpoint: "http://a:9000"}); err != nil {
		t.Fatalf("创建 University 失败: %v", err)
	}
	if _, err := svc.CreateServer(ctx, &model.Server{Name: "Course", Endpoint: "http://b:9000"}); err != nil {
		t.Fatalf("创建 Course 失败: %v", err)
	}

	tools, err := svc.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools 失败: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("应聚合 4 个工具（2 Server × 2 工具），得到 %d", len(tools))
	}

	names := make(map[string]string) // gatewayName -> serverID
	for _, tool := range tools {
		if prev, ok := names[tool.GatewayName]; ok {
			t.Fatalf("GatewayName 冲突 %q（Server %s 与 %s）", tool.GatewayName, prev, tool.ServerID)
		}
		names[tool.GatewayName] = tool.ServerID
	}
	for _, want := range []string{"university.search", "university.detail", "course.search", "course.detail"} {
		if _, ok := names[want]; !ok {
			t.Errorf("缺少聚合工具 %q，实际 %v", want, names)
		}
	}
}

// TestToggleServerEnableResetsHealth 验证禁用后再启用会把健康状态复位为
// Unknown（而非停留在 Disabled），使健康巡检能重新接管探活。
func TestToggleServerEnableResetsHealth(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})

	created, err := svc.CreateServer(ctx, &model.Server{Name: "U", Endpoint: "http://a:9000"})
	if err != nil {
		t.Fatalf("创建 Server 失败: %v", err)
	}

	if _, err := svc.ToggleServer(ctx, created.ID, false); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if got, _ := store.GetServer(ctx, created.ID); got.HealthStatus != model.ServerStatusDisabled {
		t.Fatalf("禁用后应为 disabled，得到 %s", got.HealthStatus)
	}

	if _, err := svc.ToggleServer(ctx, created.ID, true); err != nil {
		t.Fatalf("重新启用失败: %v", err)
	}
	got, err := store.GetServer(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if !got.Enabled {
		t.Fatal("Server 应处于启用状态")
	}
	if got.HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("重新启用后健康状态应复位为 unknown，得到 %s", got.HealthStatus)
	}
}

// TestDeleteServerCascadesCredentials 验证删除 Server 会级联清理其凭证。
func TestDeleteServerCascadesCredentials(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})

	created, err := svc.CreateServer(ctx, &model.Server{Name: "Del", Endpoint: "http://a:9000"})
	if err != nil {
		t.Fatalf("创建 Server 失败: %v", err)
	}
	for _, name := range []string{"a", "b"} {
		if err := store.CreateCredential(ctx, &model.Credential{ServerID: created.ID, Name: name, Kind: model.CredentialStaticToken}); err != nil {
			t.Fatalf("CreateCredential: %v", err)
		}
	}

	if err := svc.DeleteServer(ctx, created.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if creds, _ := store.ListCredentialsByServer(ctx, created.ID); len(creds) != 0 {
		t.Fatalf("删除 Server 后凭证应被级联清理，得到 %d", len(creds))
	}
	if _, err := store.GetServer(ctx, created.ID); err == nil {
		t.Fatal("删除后 Server 应不存在")
	}
}

// TestToggleToolAndRediscoverPreservesDisabled 验证工具启停与"重新发现不清掉禁用"。
func TestToggleToolAndRediscoverPreservesDisabled(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{
		tools: []DiscoveredTool{{Name: "search", Description: "查询"}, {Name: "detail", Description: "详情"}},
	})

	created, err := svc.CreateServer(ctx, &model.Server{Name: "Mock", Endpoint: "http://x:9000"})
	if err != nil {
		t.Fatalf("创建 Server 失败: %v", err)
	}
	// CreateServer 触发后台发现；这里等工具落库（最多 ~2s）。
	tools := waitTools(t, svc, 2)
	if len(tools) != 2 {
		t.Fatalf("应发现 2 个工具，得到 %d", len(tools))
	}

	search := findTool(tools, "mock.search")
	if _, err := svc.ToggleTool(ctx, search.ID, false); err != nil {
		t.Fatalf("ToggleTool(禁用): %v", err)
	}

	// 重新发现不应把禁用工具改回启用。
	if err := svc.Rediscover(ctx, created.ID); err != nil {
		t.Fatalf("Rediscover: %v", err)
	}
	tools, _ = svc.ListTools(ctx)
	if got := findTool(tools, "mock.search"); got.Enabled {
		t.Fatal("Rediscover 后手工禁用的工具应保持禁用")
	}
}

// TestUpdateToolMetadataSurvivesRediscovery verifies that operator-facing
// metadata remains stable while the latest upstream definition is retained
// as the reset baseline.
func TestUpdateToolMetadataSurvivesRediscovery(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	discoverer := &fakeDiscoverer{tools: []DiscoveredTool{{
		Name: "search", Description: "upstream v1", InputSchema: map[string]any{"type": "object"},
	}}}
	svc := NewService(store, discoverer)
	server, err := svc.CreateServer(ctx, &model.Server{Name: "Mock", Endpoint: "http://x:9000"})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	tool := findTool(waitTools(t, svc, 1), "mock.search")
	name, description := "catalog.find", "面向 Agent 的商品检索"
	schema := map[string]any{"type": "object", "properties": map[string]any{"keyword": map[string]any{"type": "string", "description": "检索词"}}}
	updated, err := svc.UpdateTool(ctx, tool.ID, UpdateToolPatch{GatewayName: &name, Description: &description, InputSchema: &schema})
	if err != nil {
		t.Fatalf("UpdateTool: %v", err)
	}
	if updated.GatewayName != name || !updated.NameOverridden || !updated.DescriptionOverridden || !updated.InputSchemaOverridden {
		t.Fatalf("override flags or values missing: %+v", updated)
	}

	discoverer.tools[0].Description = "upstream v2"
	discoverer.tools[0].InputSchema = map[string]any{"type": "object", "required": []any{"q"}}
	if err := svc.Rediscover(ctx, server.ID); err != nil {
		t.Fatalf("Rediscover: %v", err)
	}
	got, err := store.GetTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetTool: %v", err)
	}
	if got.GatewayName != name || got.Description != description {
		t.Fatalf("rediscovery overwrote customized metadata: %+v", got)
	}
	if got.SourceDescription != "upstream v2" {
		t.Fatalf("latest source metadata not retained: %+v", got)
	}
}

func TestRenameToolUpdatesExactReferences(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{tools: []DiscoveredTool{{Name: "search"}}})
	server, err := svc.CreateServer(ctx, &model.Server{Name: "Mock", Endpoint: "http://x:9000"})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	tool := findTool(waitTools(t, svc, 1), "mock.search")
	key := &model.AccessKey{Name: "client", Subject: "client", Enabled: true, KeyHash: "hash", Grants: []model.ToolGrant{{GatewayName: "mock.search"}, {GatewayName: "mock.*"}}}
	if err := store.CreateAccessKey(ctx, key); err != nil {
		t.Fatalf("CreateAccessKey: %v", err)
	}
	route := &model.Route{Name: "route", ServerID: server.ID, ToolNames: []string{"mock.search"}, Enabled: true}
	if err := store.CreateRoute(ctx, route); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	newName := "catalog.find"
	if _, err := svc.UpdateTool(ctx, tool.ID, UpdateToolPatch{GatewayName: &newName}); err != nil {
		t.Fatalf("UpdateTool: %v", err)
	}
	gotKey, _ := store.GetAccessKey(ctx, key.ID)
	if gotKey.Grants[0].GatewayName != newName || gotKey.Grants[1].GatewayName != "mock.*" {
		t.Fatalf("grant references not migrated correctly: %+v", gotKey.Grants)
	}
	gotRoutes, _ := store.ListRoutes(ctx)
	if gotRoutes[0].ToolNames[0] != newName {
		t.Fatalf("route reference not migrated: %+v", gotRoutes[0].ToolNames)
	}
}

// waitTools 轮询等待工具数达到预期（容忍 CreateServer 的后台发现异步）。
func waitTools(t *testing.T, svc *Service, want int) []model.Tool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tools, err := svc.ListTools(context.Background())
		if err == nil && len(tools) == want {
			return tools
		}
		time.Sleep(20 * time.Millisecond)
	}
	tools, _ := svc.ListTools(context.Background())
	return tools
}

func findTool(tools []model.Tool, gateway string) *model.Tool {
	for i := range tools {
		if tools[i].GatewayName == gateway {
			return &tools[i]
		}
	}
	return nil
}
