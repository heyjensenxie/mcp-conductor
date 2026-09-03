package router

import (
	"context"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

// newFixture 构造一个含单 Server + 单 Tool 的解析环境。
func newFixture(t *testing.T, health model.ServerStatus) *DefaultResolver {
	t.Helper()
	store := memory.New()
	ctx := context.Background()

	server := &model.Server{
		Name:         "University",
		Endpoint:     "http://upstream:9000",
		Enabled:      true,
		HealthStatus: health,
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("创建 Server 失败: %v", err)
	}
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID:     server.ID,
		OriginalName: "search",
		GatewayName:  "university.search",
		Enabled:      true,
	}); err != nil {
		t.Fatalf("创建 Tool 失败: %v", err)
	}
	return NewResolver(store, store)
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
		t.Fatalf("Server 不健康应返回 route_error，得到 %v", err)
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
	if err := store.CreateServer(ctx, &model.Server{
		Name:         "Course",
		Endpoint:     "http://b",
		Enabled:      true,
		HealthStatus: model.ServerStatusHealthy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTool(ctx, &model.Tool{
		ServerID:     "srv-1",
		OriginalName: "search",
		GatewayName:  "course.search",
		Enabled:      false,
	}); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(store, store)
	if _, err := r.Resolve(ctx, "course.search"); err == nil {
		t.Fatal("禁用工具应拒绝调用")
	}
}
