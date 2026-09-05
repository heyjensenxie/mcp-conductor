package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

// noopDiscoverer 返回空工具集，避免注册时后台发现 NPE。
type noopDiscoverer struct{}

func (noopDiscoverer) Discover(context.Context, model.Server) ([]registry.DiscoveredTool, error) {
	return nil, nil
}

// TestControl_ProbeNowTriggeredOnCreateAndEnable 验证注册与重新启用 Server 会
// 触发即时健康巡检，禁用不触发。
func TestControl_ProbeNowTriggeredOnCreateAndEnable(t *testing.T) {
	store := memory.New()
	reg := registry.NewService(store, noopDiscoverer{})
	var triggered []string
	ctrl := NewControl(reg, store, nil, nil)
	ctrl.probeNow = func(id string) { triggered = append(triggered, id) }

	// 注册 → 触发一次。
	rec := httptest.NewRecorder()
	ctrl.handleCreateServer(rec, httptest.NewRequest(http.MethodPost, "/api/servers",
		strings.NewReader(`{"name":"Mock","endpoint":"http://localhost:9000/mcp","transport":"https"}`)))
	created := decodeEnvelope(t, rec)
	var srv struct {
		ID     string `json:"id"`
		Health string `json:"health_status"`
	}
	if err := json.Unmarshal(created.Data, &srv); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	if len(triggered) != 1 || triggered[0] != srv.ID {
		t.Fatalf("注册应触发即时巡检 %q，实际 %v", srv.ID, triggered)
	}

	// 禁用 → 不触发。
	toggle := func(enabled bool) {
		t.Helper()
		body := `{"enabled":` + map[bool]string{true: "true", false: "false"}[enabled] + `}`
		req := httptest.NewRequest(http.MethodPatch, "/api/servers/"+srv.ID+"/toggle", strings.NewReader(body))
		req.SetPathValue("id", srv.ID)
		rec := httptest.NewRecorder()
		ctrl.handleToggleServer(rec, req)
		_ = decodeEnvelope(t, rec)
	}
	toggle(false)
	if len(triggered) != 1 {
		t.Fatalf("禁用不应触发巡检，实际 %v", triggered)
	}

	// 重新启用 → 触发。
	toggle(true)
	if len(triggered) != 2 || triggered[1] != srv.ID {
		t.Fatalf("重新启用应再次触发巡检 %q，实际 %v", srv.ID, triggered)
	}
}
