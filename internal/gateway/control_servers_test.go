package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

// seedControlServer 注册一个 Server 并返回其 id。
func seedControlServer(t *testing.T, ctrl *Control) string {
	t.Helper()
	rec := httptest.NewRecorder()
	ctrl.handleCreateServer(rec, httptest.NewRequest(http.MethodPost, "/api/servers",
		strings.NewReader(`{"name":"Mock","endpoint":"http://localhost:9000/mcp","transport":"https"}`)))
	e := decodeEnvelope(t, rec)
	var srv struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(e.Data, &srv); err != nil || srv.ID == "" {
		t.Fatalf("解析创建响应失败: %v / %s", err, e.Data)
	}
	return srv.ID
}

// TestUpdateServerEditsAllowedFields 验证 PATCH Server 更新可编辑字段且 name 不变。
func TestUpdateServerEditsAllowedFields(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(registry.NewService(store, noopDiscoverer{}), store, nil, nil)
	id := seedControlServer(t, ctrl)

	req := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id,
		strings.NewReader(`{"description":"说明","endpoint":"http://new-host:9001/mcp","transport":"sse"}`))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ctrl.handleUpdateServer(rec, req)
	e := decodeEnvelope(t, rec)

	var srv struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Endpoint    string `json:"endpoint"`
		Transport   string `json:"transport"`
	}
	if err := json.Unmarshal(e.Data, &srv); err != nil {
		t.Fatalf("解析更新响应失败: %v", err)
	}
	if srv.Name != "Mock" || srv.Endpoint != "http://new-host:9001/mcp" ||
		srv.Description != "说明" || srv.Transport != "sse" {
		t.Fatalf("更新字段不一致: %+v", srv)
	}
}

// TestCredentialUpdateAndDelete 验证凭证编辑（改值/改名）与删除在控制面端到端生效。
func TestCredentialUpdateAndDelete(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(registry.NewService(store, noopDiscoverer{}), store, nil, nil)
	id := seedControlServer(t, ctrl)

	// 创建 api_key 凭证。
	rec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/credentials",
		strings.NewReader(`{"name":"上游Key","kind":"api_key","header":"X-Upstream-Key","value":"s1"}`))
	createReq.SetPathValue("id", id)
	ctrl.handleCreateCredential(rec, createReq)
	e := decodeEnvelope(t, rec)
	var cred struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(e.Data, &cred); err != nil || cred.ID == "" {
		t.Fatalf("解析凭证创建响应失败: %v / %s", err, e.Data)
	}
	if strings.Contains(string(e.Data), "s1") {
		t.Fatal("创建响应不得回发明文 value")
	}

	// 更新值 + 改名。
	rec = httptest.NewRecorder()
	upd := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id+"/credentials/"+cred.ID,
		strings.NewReader(`{"name":"改名","value":"s2"}`))
	upd.SetPathValue("id", id)
	upd.SetPathValue("credId", cred.ID)
	ctrl.handleUpdateCredential(rec, upd)
	e = decodeEnvelope(t, rec)
	var updated struct {
		Name     string `json:"name"`
		HasValue bool   `json:"has_value"`
	}
	if err := json.Unmarshal(e.Data, &updated); err != nil || updated.Name != "改名" || !updated.HasValue {
		t.Fatalf("更新后元数据不一致: %+v / %v", updated, err)
	}

	// 值确实被替换。
	creds, err := store.ListCredentialsByServer(storeCtx(), id)
	if err != nil || len(creds) != 1 || creds[0].Value != "s2" {
		t.Fatalf("存储内值未更新: %+v / %v", creds, err)
	}

	// 删除。
	rec = httptest.NewRecorder()
	del := httptest.NewRequest(http.MethodDelete, "/api/servers/"+id+"/credentials/"+cred.ID, nil)
	del.SetPathValue("id", id)
	del.SetPathValue("credId", cred.ID)
	ctrl.handleDeleteCredential(rec, del)
	_ = decodeEnvelope(t, rec)
	if creds, _ := store.ListCredentialsByServer(storeCtx(), id); len(creds) != 0 {
		t.Fatalf("删除后应无凭证，得到 %d", len(creds))
	}
}

// storeCtx 返回无取消的测试上下文。
func storeCtx() context.Context { return context.Background() }
