package registry

import (
	"context"
	"testing"

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
