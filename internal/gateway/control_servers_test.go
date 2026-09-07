package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
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

// TestUpdateServerEditsDescription 验证 PATCH Server 只更新逻辑字段（description），
// name 不可改、endpoint/transport 属于实例不受影响。
func TestUpdateServerEditsDescription(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(registry.NewService(store, noopDiscoverer{}), store, nil, nil)
	id := seedControlServer(t, ctrl)

	req := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id,
		strings.NewReader(`{"description":"说明"}`))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ctrl.handleUpdateServer(rec, req)
	e := decodeEnvelope(t, rec)

	var srv struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Instances   []struct {
			Endpoint  string `json:"endpoint"`
			Transport string `json:"transport"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(e.Data, &srv); err != nil {
		t.Fatalf("解析更新响应失败: %v", err)
	}
	if srv.Name != "Mock" || srv.Description != "说明" {
		t.Fatalf("更新字段不一致: %+v", srv)
	}
	if len(srv.Instances) != 1 || srv.Instances[0].Endpoint != "http://localhost:9000/mcp" ||
		srv.Instances[0].Transport != "https" {
		t.Fatalf("seed 实例应不受 Server 更新影响: %+v", srv.Instances)
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

// TestCreateServerStdioWithArgs 验证经控制面注册 stdio Server 时，args 落到 seed
// 实例并随响应返回；https 携带 args 被 CodeInvalidArgument 拒绝。
func TestCreateServerStdioWithArgs(t *testing.T) {
	ctrl, store := newInstancesControl(nil)

	// 注册 stdio Server：endpoint=命令，args=启动参数。
	rec := httptest.NewRecorder()
	ctrl.handleCreateServer(rec, httptest.NewRequest(http.MethodPost, "/api/servers",
		strings.NewReader(`{"name":"MockStdio","endpoint":"./bin/mock-mcp","transport":"stdio","args":["-stdio","--port","9100"]}`)))
	e := decodeEnvelope(t, rec)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("注册 stdio Server 失败: %d / %s", rec.Code, rec.Body.String())
	}
	var srv struct {
		ID        string `json:"id"`
		Instances []struct {
			Endpoint  string   `json:"endpoint"`
			Transport string   `json:"transport"`
			Args      []string `json:"args"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(e.Data, &srv); err != nil || srv.ID == "" {
		t.Fatalf("解析创建响应失败: %v / %s", err, e.Data)
	}
	if len(srv.Instances) != 1 || srv.Instances[0].Transport != "stdio" ||
		srv.Instances[0].Endpoint != "./bin/mock-mcp" || len(srv.Instances[0].Args) != 3 || srv.Instances[0].Args[0] != "-stdio" {
		t.Fatalf("seed 实例应含 stdio 命令与 args: %+v", srv.Instances)
	}
	// 存储侧也持久化 args。
	insts, _ := store.ListInstancesByServer(context.Background(), srv.ID)
	if len(insts) != 1 || len(insts[0].Args) != 3 || insts[0].Args[1] != "--port" {
		t.Fatalf("seed 实例 args 未落库: %+v", insts)
	}

	// https 携带 args 应被拒绝（校验拦截配置错误）。
	rec = httptest.NewRecorder()
	ctrl.handleCreateServer(rec, httptest.NewRequest(http.MethodPost, "/api/servers",
		strings.NewReader(`{"name":"Bad","endpoint":"http://x:9000/mcp","transport":"https","args":["-x"]}`)))
	var rej struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rej); err != nil {
		t.Fatalf("解析拒绝信封失败: %v", err)
	}
	if rej.Code != "invalid_argument" {
		t.Fatalf("https 带 args 应以 invalid_argument 拒绝，得到 %q / %s", rej.Code, rec.Body.String())
	}
}
