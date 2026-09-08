package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeInstanceProber 模拟 registry.InstanceProber：按预设结论返回探测结果。
type fakeInstanceProber struct {
	status model.ServerStatus
	err    error
}

func (f *fakeInstanceProber) Check(_ context.Context, _ model.Server, _ model.Instance) (model.ServerStatus, error) {
	return f.status, f.err
}

// newInstancesControl 构造带 prober 的 Control（prober 为空则不装配）。
func newInstancesControl(prober *fakeInstanceProber) (*Control, *memory.Store) {
	store := memory.New()
	reg := registry.NewService(store, noopDiscoverer{})
	if prober != nil {
		reg = reg.WithProber(prober)
	}
	return NewControl(reg, store, nil, nil), store
}

// instanceIDByEndpoint 在 Server 实例列表中按 endpoint 反查实例 id。
func instanceIDByEndpoint(t *testing.T, store *memory.Store, serverID, endpoint string) string {
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

// TestControlInstances_GetServerHydrated 验证 GET Server 返回水合实例（seed 实例的
// endpoint/transport 承载于 instances），Server 顶层不再有 endpoint/transport 字段。
func TestControlInstances_GetServerHydrated(t *testing.T) {
	ctrl, _ := newInstancesControl(nil)
	id := seedControlServer(t, ctrl)

	req := httptest.NewRequest(http.MethodGet, "/api/servers/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ctrl.handleGetServer(rec, req)

	e := decodeEnvelope(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET server 应为 200，得到 %d", rec.Code)
	}
	var data map[string]any
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("解析 server 数据失败: %v / %s", err, e.Data)
	}
	// Server 顶层不再暴露 endpoint/transport（它们属于实例）。
	if _, ok := data["endpoint"]; ok {
		t.Fatal("Server 响应不应再含顶层 endpoint 字段")
	}
	if _, ok := data["transport"]; ok {
		t.Fatal("Server 响应不应再含顶层 transport 字段")
	}
	instances, ok := data["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("应水合出 1 个实例，得到 %v", data["instances"])
	}
	first := instances[0].(map[string]any)
	if first["endpoint"] != "http://localhost:9000/mcp" || first["transport"] != "https" {
		t.Fatalf("seed 实例 endpoint/transport 不一致: %+v", first)
	}
}

// TestControlInstances_ListServersHydratesInstances 验证列表每项 Server 都携带水合
// 实例（新增第二实例后 len==2）。
func TestControlInstances_ListServersHydratesInstances(t *testing.T) {
	ctrl, _ := newInstancesControl(nil)
	id := seedControlServer(t, ctrl)

	// 经处理器新增第二实例。
	rec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/instances",
		strings.NewReader(`{"endpoint":"http://second:9000/mcp","transport":"https"}`))
	createReq.SetPathValue("id", id)
	ctrl.handleCreateInstance(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("新增实例应为 201，得到 %d / %s", rec.Code, rec.Body.String())
	}
	_ = decodeEnvelope(t, rec)

	// 列表每项都带实例（共 2）。
	rec = httptest.NewRecorder()
	ctrl.handleListServers(rec, httptest.NewRequest(http.MethodGet, "/api/servers?page_size=0", nil))
	var list struct {
		Items []model.Server `json:"items"`
		Total int            `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &list); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if len(list.Items) != 1 || list.Total != 1 {
		t.Fatalf("应只有 1 个 Server，得到 %d / total=%d", len(list.Items), list.Total)
	}
	if len(list.Items[0].Instances) != 2 {
		t.Fatalf("Server 应水合 2 个实例，得到 %d", len(list.Items[0].Instances))
	}
}

// TestControlInstances_LifecycleViaHandlers 端到端覆盖 新增→列表→启停→删除 以及
// 最后一个实例的删除保护。
func TestControlInstances_LifecycleViaHandlers(t *testing.T) {
	ctrl, store := newInstancesControl(nil)
	id := seedControlServer(t, ctrl)
	seedID := instanceIDByEndpoint(t, store, id, "http://localhost:9000/mcp")

	// 新增第二实例。
	rec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/instances",
		strings.NewReader(`{"endpoint":"http://second:9000/mcp","transport":"https"}`))
	createReq.SetPathValue("id", id)
	ctrl.handleCreateInstance(rec, createReq)
	e := decodeEnvelope(t, rec)
	var second struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(e.Data, &second); err != nil || second.ID == "" {
		t.Fatalf("解析新增实例失败: %v / %s", err, e.Data)
	}

	// handleListInstances 应显示 2 条。
	rec = httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/servers/"+id+"/instances", nil)
	listReq.SetPathValue("id", id)
	ctrl.handleListInstances(rec, listReq)
	var insts []model.Instance
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &insts); err != nil || len(insts) != 2 {
		t.Fatalf("实例列表应为 2 条: %v / %d", err, len(insts))
	}

	// ToggleInstance(enabled=false) 持久化。
	rec = httptest.NewRecorder()
	tgReq := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id+"/instances/"+second.ID+"/toggle",
		strings.NewReader(`{"enabled":false}`))
	tgReq.SetPathValue("id", id)
	tgReq.SetPathValue("iid", second.ID)
	ctrl.handleToggleInstance(rec, tgReq)
	_ = decodeEnvelope(t, rec)
	if got, _ := store.GetInstance(context.Background(), second.ID); got.Enabled {
		t.Fatalf("ToggleInstance(false) 后实例应禁用: %+v", got)
	}

	// DeleteInstance 删除第二个实例成功 → 剩 1。
	rec = httptest.NewRecorder()
	delReq := httptest.NewRequest(http.MethodDelete, "/api/servers/"+id+"/instances/"+second.ID, nil)
	delReq.SetPathValue("id", id)
	delReq.SetPathValue("iid", second.ID)
	ctrl.handleDeleteInstance(rec, delReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除实例应 204，得到 %d / %s", rec.Code, rec.Body.String())
	}
	insts, _ = store.ListInstancesByServer(context.Background(), id)
	if len(insts) != 1 {
		t.Fatalf("删除后应剩 1 个实例，得到 %d", len(insts))
	}

	// 删除最后一个实例 → invalid_argument 信封，Server 仍保留实例。
	rec = httptest.NewRecorder()
	lastReq := httptest.NewRequest(http.MethodDelete, "/api/servers/"+id+"/instances/"+seedID, nil)
	lastReq.SetPathValue("id", id)
	lastReq.SetPathValue("iid", seedID)
	ctrl.handleDeleteInstance(rec, lastReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("删除最后实例应 400，得到 %d / %s", rec.Code, rec.Body.String())
	}
	if code := envelopeCode(t, rec); code != "invalid_argument" {
		t.Fatalf("删除最后实例应返回 invalid_argument 信封，得到 %q / %s", code, rec.Body.String())
	}
	insts, _ = store.ListInstancesByServer(context.Background(), id)
	if len(insts) != 1 {
		t.Fatalf("拒绝删除后 Server 仍应保留 1 个实例，得到 %d", len(insts))
	}
}

// TestControlInstances_TestInstancePersistsHealthy 验证 handleTestInstance 由健康
// prober 探测后返回 200 且实例 health_status=healthy。
func TestControlInstances_TestInstancePersistsHealthy(t *testing.T) {
	prober := &fakeInstanceProber{status: model.ServerStatusHealthy}
	ctrl, store := newInstancesControl(prober)
	id := seedControlServer(t, ctrl)
	iid := instanceIDByEndpoint(t, store, id, "http://localhost:9000/mcp")

	req := httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/instances/"+iid+"/test", nil)
	req.SetPathValue("id", id)
	req.SetPathValue("iid", iid)
	rec := httptest.NewRecorder()
	ctrl.handleTestInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("TestInstance 应 200，得到 %d / %s", rec.Code, rec.Body.String())
	}
	e := decodeEnvelope(t, rec)
	var inst struct {
		ID           string `json:"id"`
		HealthStatus string `json:"health_status"`
	}
	if err := json.Unmarshal(e.Data, &inst); err != nil || inst.ID != iid || inst.HealthStatus != "healthy" {
		t.Fatalf("TestInstance 响应不一致: %v / %+v", err, inst)
	}
	if got, _ := store.GetInstance(context.Background(), iid); got.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("探测结果应落库 healthy，得到 %s", got.HealthStatus)
	}
}

// TestControlInstances_TestInstanceFailureSurfacesSafeEnvelope 验证实例测试失败时
// 信封 code 非 "ok"（供前端进入错误分支）、HTTP 仍 200 且携带落库后的 unhealthy
// 实例；消息为脱敏诊断，只含 HTTP 状态，不含上游响应体回显的凭据。
func TestControlInstances_TestInstanceFailureSurfacesSafeEnvelope(t *testing.T) {
	prober := &fakeInstanceProber{err: &mcp.UpstreamHTTPError{
		Status: http.StatusUnauthorized,
		Body:   `{"echo":"Bearer sekret-token"}`,
	}}
	ctrl, store := newInstancesControl(prober)
	id := seedControlServer(t, ctrl)
	iid := instanceIDByEndpoint(t, store, id, "http://localhost:9000/mcp")

	req := httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/instances/"+iid+"/test", nil)
	req.SetPathValue("id", id)
	req.SetPathValue("iid", iid)
	rec := httptest.NewRecorder()
	ctrl.handleTestInstance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("TestInstance 失败应 200，得到 %d / %s", rec.Code, rec.Body.String())
	}
	var e struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析信封失败: %v", err)
	}
	if e.Code == "ok" {
		t.Fatalf("失败信封 code 不应为 ok：%s", rec.Body.String())
	}
	if strings.Contains(e.Message, "sekret") || strings.Contains(e.Message, "Bearer") {
		t.Fatalf("信封消息不应泄露上游响应体/凭据：%q", e.Message)
	}
	if !strings.Contains(e.Message, "上游返回 HTTP 状态 401") {
		t.Fatalf("信封消息应含可诊断的 HTTP 状态：%q", e.Message)
	}
	var inst struct {
		ID           string `json:"id"`
		HealthStatus string `json:"health_status"`
	}
	if err := json.Unmarshal(e.Data, &inst); err != nil || inst.ID != iid || inst.HealthStatus != "unhealthy" {
		t.Fatalf("失败信封仍应携带 unhealthy 实例：err=%v inst=%+v", err, inst)
	}
	if got, _ := store.GetInstance(context.Background(), iid); got.HealthStatus != model.ServerStatusUnhealthy {
		t.Fatalf("探测失败结果应落库 unhealthy，得到 %s", got.HealthStatus)
	}
}

// TestControlInstances_HealthStatusListFilterReflectsAggregate 验证实例启停驱动的
// Server 聚合健康能被列表 health_status 筛选命中。
func TestControlInstances_HealthStatusListFilterReflectsAggregate(t *testing.T) {
	ctrl, store := newInstancesControl(nil)
	// 注册即发现成功（noopDiscoverer 空工具）→ seed 实例 healthy → 聚合 healthy。
	id := seedControlServer(t, ctrl)
	iid := instanceIDByEndpoint(t, store, id, "http://localhost:9000/mcp")

	// 摘除唯一实例 → Server 聚合 unhealthy。
	rec := httptest.NewRecorder()
	tgReq := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id+"/instances/"+iid+"/toggle",
		strings.NewReader(`{"enabled":false}`))
	tgReq.SetPathValue("id", id)
	tgReq.SetPathValue("iid", iid)
	ctrl.handleToggleInstance(rec, tgReq)
	_ = decodeEnvelope(t, rec)

	// health_status=unhealthy 应命中该 Server。
	rec = httptest.NewRecorder()
	ctrl.handleListServers(rec, httptest.NewRequest(http.MethodGet, "/api/servers?health_status=unhealthy", nil))
	var list struct {
		Items []model.Server `json:"items"`
		Total int            `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &list); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != id {
		t.Fatalf("health_status=unhealthy 应命中该 Server，得到 %d", len(list.Items))
	}

	// health_status=healthy 不应再命中（已无可用实例）。
	rec = httptest.NewRecorder()
	ctrl.handleListServers(rec, httptest.NewRequest(http.MethodGet, "/api/servers?health_status=healthy", nil))
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &list); err != nil {
		t.Fatalf("解析列表失败: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("health_status=healthy 不应命中，得到 %d", len(list.Items))
	}
}

// TestControlInstances_StdioArgsRoundTrip 验证控制面新增/更新 stdio 实例时 args
// 经 JSON 往返并落库；更新时端点/传输/args 组合校验生效。
func TestControlInstances_StdioArgsRoundTrip(t *testing.T) {
	ctrl, store := newInstancesControl(nil)
	id := seedControlServer(t, ctrl)

	// 新增 stdio 实例（command + args）。
	rec := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/servers/"+id+"/instances",
		strings.NewReader(`{"endpoint":"./bin/mock-mcp","transport":"stdio","args":["-stdio","--port","9100"]}`))
	createReq.SetPathValue("id", id)
	ctrl.handleCreateInstance(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("新增 stdio 实例应为 201，得到 %d / %s", rec.Code, rec.Body.String())
	}
	var inst struct {
		ID   string   `json:"id"`
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &inst); err != nil || inst.ID == "" {
		t.Fatalf("解析新增实例失败: %v / %s", err, rec.Body.String())
	}
	if len(inst.Args) != 3 || inst.Args[0] != "-stdio" {
		t.Fatalf("新增实例应回显 args: %+v", inst)
	}
	insts, _ := store.ListInstancesByServer(context.Background(), id)
	found := false
	for _, s := range insts {
		if s.ID == inst.ID && len(s.Args) == 3 && s.Args[1] == "--port" {
			found = true
		}
	}
	if !found {
		t.Fatalf("stdio 实例 args 未落库: %+v", insts)
	}

	// 更新该实例 args（清空）+ 切回 https（组合合法：空 args）。
	rec = httptest.NewRecorder()
	updReq := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id+"/instances/"+inst.ID,
		strings.NewReader(`{"transport":"https","args":[]}`))
	updReq.SetPathValue("id", id)
	updReq.SetPathValue("iid", inst.ID)
	ctrl.handleUpdateInstance(rec, updReq)
	e := decodeEnvelope(t, rec)
	var updated struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(e.Data, &updated); err != nil || len(updated.Args) != 0 {
		t.Fatalf("清空 args 后应为空数组: %+v / %v", updated, err)
	}
}
