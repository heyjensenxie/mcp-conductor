package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// rpFixture 预置一个带两工具的 Server，返回 store / control / server。
func rpFixture(t *testing.T) (*memory.Store, *Control, *model.Server) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()
	srv := &model.Server{Name: "A", Enabled: true, HealthStatus: model.ServerStatusHealthy}
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ id, name string }{
		{"t-search", "search"}, {"t-detail", "detail"},
	} {
		if err := store.UpsertTool(ctx, &model.Tool{
			ID: spec.id, ServerID: srv.ID, OriginalName: spec.name, GatewayName: "a." + spec.name, Enabled: true,
		}); err != nil {
			t.Fatalf("UpsertTool: %v", err)
		}
	}
	ctrl := NewControl(registry.NewService(store, noopDiscoverer{}), store, nil, nil)
	return store, ctrl, srv
}

// envelopeCode 只解信封 code，用于断言失败响应。
func envelopeCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析信封失败: %v", err)
	}
	return e.Code
}

func TestCreateRoute_RejectsUnknownServer(t *testing.T) {
	_, ctrl, _ := rpFixture(t)
	rec := httptest.NewRecorder()
	ctrl.handleCreateRoute(rec, httptest.NewRequest(http.MethodPost, "/api/routes",
		strings.NewReader(`{"name":"r","server_id":"ghost","tool_names":["a.search"]}`)))
	if envelopeCode(t, rec) != "not_found" || rec.Code != http.StatusNotFound {
		t.Fatalf("未知 Server 应 not_found，状态 %d / %s", rec.Code, rec.Body.String())
	}
}

func TestCreateRoute_RejectsUnknownTool(t *testing.T) {
	_, ctrl, srv := rpFixture(t)
	rec := httptest.NewRecorder()
	ctrl.handleCreateRoute(rec, httptest.NewRequest(http.MethodPost, "/api/routes",
		strings.NewReader(`{"name":"r","server_id":"`+srv.ID+`","tool_names":["a.missing"]}`)))
	if envelopeCode(t, rec) != "not_found" {
		t.Fatalf("未知工具应 not_found: %s", rec.Body.String())
	}
}

func TestRouteCRUDLifecycle(t *testing.T) {
	_, ctrl, srv := rpFixture(t)

	// 创建。
	rec := httptest.NewRecorder()
	ctrl.handleCreateRoute(rec, httptest.NewRequest(http.MethodPost, "/api/routes",
		strings.NewReader(`{"name":"主路由","server_id":"`+srv.ID+`","tool_names":["a.search"],"enabled":true}`)))
	e := decodeEnvelope(t, rec)
	var created struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Enabled bool     `json:"enabled"`
		Tools   []string `json:"tool_names"`
	}
	if err := json.Unmarshal(e.Data, &created); err != nil || created.ID == "" || created.Tools[0] != "a.search" {
		t.Fatalf("创建路由失败: %v / %s", err, e.Data)
	}

	// 编辑（改名/启停）。
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/routes/"+created.ID,
		strings.NewReader(`{"name":"改名","tool_names":["a.detail"],"enabled":false}`))
	req.SetPathValue("id", created.ID)
	ctrl.handleUpdateRoute(rec, req)
	e = decodeEnvelope(t, rec)
	var updated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(e.Data, &updated); err != nil || updated.Name != "改名" {
		t.Fatalf("编辑路由失败: %v / %s", err, e.Data)
	}

	// 启停。
	rec = httptest.NewRecorder()
	tg := httptest.NewRequest(http.MethodPatch, "/api/routes/"+created.ID+"/toggle", strings.NewReader(`{"enabled":true}`))
	tg.SetPathValue("id", created.ID)
	ctrl.handleToggleRoute(rec, tg)
	_ = decodeEnvelope(t, rec)

	// 列表应只剩 1 条且 enabled（统一分页信封 data.items）。
	rec = httptest.NewRecorder()
	ctrl.handleListRoutes(rec, httptest.NewRequest(http.MethodGet, "/api/routes", nil))
	var list struct {
		Items []model.Route `json:"items"`
		Total int           `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &list); err != nil || len(list.Items) != 1 || !list.Items[0].Enabled || list.Total != 1 {
		t.Fatalf("列表异常: %v / %+v", err, list.Items)
	}

	// 删除。
	rec = httptest.NewRecorder()
	del := httptest.NewRequest(http.MethodDelete, "/api/routes/"+created.ID, nil)
	del.SetPathValue("id", created.ID)
	ctrl.handleDeleteRoute(rec, del)
	_ = decodeEnvelope(t, rec)
	if routes, _ := ctrl.store.ListRoutes(context.Background()); len(routes) != 0 {
		t.Fatalf("删除后应无路由，得到 %d", len(routes))
	}
}

func TestToggleToolViaControl(t *testing.T) {
	store, ctrl, _ := rpFixture(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/tools/t-search/toggle", strings.NewReader(`{"enabled":false}`))
	req.SetPathValue("id", "t-search")
	ctrl.handleToggleTool(rec, req)
	_ = decodeEnvelope(t, rec)

	got, err := store.GetTool(context.Background(), "t-search")
	if err != nil || got.Enabled {
		t.Fatalf("工具应被禁用: %+v / %v", got, err)
	}
	byName, _ := store.GetToolByGatewayName(context.Background(), "a.search")
	if byName.Enabled {
		t.Fatal("按名索引也应同步为禁用")
	}
}
