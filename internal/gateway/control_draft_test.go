package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeDraftProber 模拟 registry.DraftInstanceProber：记录拨测入参，返回预设结论。
type fakeDraftProber struct {
	calls   int
	server  model.Server
	inst    model.Instance
	headers map[string]string
	status  model.ServerStatus
	err     error
}

func (f *fakeDraftProber) CheckDraft(_ context.Context, server model.Server, instance model.Instance, headers map[string]string) (model.ServerStatus, error) {
	f.calls++
	f.server, f.inst, f.headers = server, instance, headers
	if f.status == "" {
		f.status = model.ServerStatusHealthy
	}
	return f.status, f.err
}

// newDraftControl 构造带草稿探针的 Control（探针为空则不装配）。
func newDraftControl(prober *fakeDraftProber) (*Control, *memory.Store) {
	store := memory.New()
	reg := registry.NewService(store, noopDiscoverer{})
	if prober != nil {
		reg = reg.WithDraftProber(prober)
	}
	return NewControl(reg, store, nil, nil), store
}

// TestControlTestConnectionNoSideEffects 验证 POST /api/servers/test-connection：
// 返回 200 + status=ok，且不创建 Server/实例（草稿拨测无副作用）。
func TestControlTestConnectionNoSideEffects(t *testing.T) {
	prober := &fakeDraftProber{}
	ctrl, store := newDraftControl(prober)

	body := `{"name":"Demo","endpoint":"http://localhost:9000/mcp","transport":"https",
		"args":[],"headers":{"Authorization":"Bearer xxx"}}`
	rec := httptest.NewRecorder()
	ctrl.handleTestServerConnection(rec, httptest.NewRequest(http.MethodPost, "/api/servers/test-connection", strings.NewReader(body)))

	e := decodeEnvelope(t, rec)
	var data struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil || data.Status != "ok" {
		t.Fatalf("响应应为 status=ok: %+v / %v", data, err)
	}
	if prober.calls != 1 {
		t.Fatalf("草稿探针应被调用 1 次，得到 %d", prober.calls)
	}
	if prober.headers["Authorization"] != "Bearer xxx" {
		t.Fatalf("临时请求头应透传给草稿拨测: %+v", prober.headers)
	}
	if prober.server.Name != "Demo" || prober.inst.Endpoint != "http://localhost:9000/mcp" {
		t.Fatalf("草稿参数不正确: %+v / %+v", prober.server, prober.inst)
	}
	if servers, _ := store.ListServers(context.Background()); len(servers) != 0 {
		t.Fatalf("保存前测试不应创建 Server，得到 %d 个", len(servers))
	}
}

// TestControlTestConnectionLoadsSavedCredentials 验证编辑已有 Server 时后端加载已保存
// 凭据，且请求体临时 Header 覆盖同名已保存 Header。
func TestControlTestConnectionLoadsSavedCredentials(t *testing.T) {
	prober := &fakeDraftProber{}
	ctrl, store := newDraftControl(prober)
	id := seedControlServer(t, ctrl)

	ctx := context.Background()
	if err := store.CreateCredential(ctx, &model.Credential{
		ServerID: id, Name: "key", Kind: model.CredentialAPIKey, Header: "X-Upstream-Key", Value: "saved",
	}); err != nil {
		t.Fatalf("创建凭证失败: %v", err)
	}

	body := `{"server_id":"` + id + `","name":"Mock","endpoint":"http://localhost:9000/mcp","transport":"https",
		"headers":{"X-Upstream-Key":"temp"}}`
	rec := httptest.NewRecorder()
	ctrl.handleTestServerConnection(rec, httptest.NewRequest(http.MethodPost, "/api/servers/test-connection", strings.NewReader(body)))
	_ = decodeEnvelope(t, rec)

	if prober.headers["X-Upstream-Key"] != "temp" {
		t.Fatalf("临时 Header 应覆盖已保存 Header，得到 %+v", prober.headers)
	}
	// 草稿拨测不落库：凭证仍只有 1 条，且值未被临时头改写。
	creds, _ := store.ListCredentialsByServer(ctx, id)
	if len(creds) != 1 || creds[0].Value != "saved" {
		t.Fatalf("临时 Header 不应保存到存储: %+v", creds)
	}
}

// TestControlTestConnectionFailureEnvelope 验证拨测失败以非 ok 信封 + 安全文案返回，
// 且不产生任何落库副作用。
func TestControlTestConnectionFailureEnvelope(t *testing.T) {
	prober := &fakeDraftProber{err: errs.New(errs.CodeUpstream, "上游返回 HTTP 状态 401")}
	ctrl, store := newDraftControl(prober)

	body := `{"name":"Demo","endpoint":"http://localhost:9000/mcp","transport":"https"}`
	rec := httptest.NewRecorder()
	ctrl.handleTestServerConnection(rec, httptest.NewRequest(http.MethodPost, "/api/servers/test-connection", strings.NewReader(body)))

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析失败信封: %v / %s", err, rec.Body.String())
	}
	if e.Code != string(errs.CodeUpstream) {
		t.Fatalf("应回传 upstream_error，得到 %q / %s", e.Code, rec.Body.String())
	}
	if !strings.Contains(e.Message, "连接测试失败") {
		t.Fatalf("错误文案应说明连接测试失败: %q", e.Message)
	}
	if servers, _ := store.ListServers(context.Background()); len(servers) != 0 {
		t.Fatalf("失败的草稿测试不应创建 Server，得到 %d 个", len(servers))
	}
}

// TestControlTestConnectionInvalidBody 验证非法请求体（参数校验失败）返回 400。
func TestControlTestConnectionInvalidBody(t *testing.T) {
	prober := &fakeDraftProber{}
	ctrl, _ := newDraftControl(prober)

	cases := []struct {
		name string
		body string
	}{
		{"缺少 endpoint", `{"name":"Demo","transport":"https"}`},
		{"endpoint 非绝对 URL", `{"endpoint":"localhost:9000","transport":"https"}`},
		{"https 携带 args", `{"endpoint":"http://a:9000/mcp","transport":"https","args":["-x"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctrl.handleTestServerConnection(rec, httptest.NewRequest(http.MethodPost, "/api/servers/test-connection", strings.NewReader(tc.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应返回 400，得到 %d / %s", rec.Code, rec.Body.String())
			}
		})
	}
	if prober.calls != 0 {
		t.Fatalf("参数非法不应触发拨测，实际调用 %d 次", prober.calls)
	}
}

// TestControlListCredentialsNeverReturnsPlaintext 验证凭证列表（page_size=0 全量，
// 供编辑表单回显）只返回元数据：不含明文 value，仅 has_value 标记是否已配置。
func TestControlListCredentialsNeverReturnsPlaintext(t *testing.T) {
	ctrl, store := newDraftControl(nil)
	id := seedControlServer(t, ctrl)
	ctx := context.Background()
	for _, cred := range []*model.Credential{
		{ServerID: id, Name: "token", Kind: model.CredentialStaticToken, Value: "super-secret"},
		{ServerID: id, Name: "key", Kind: model.CredentialAPIKey, Header: "X-K", Value: "another-secret"},
	} {
		if err := store.CreateCredential(ctx, cred); err != nil {
			t.Fatalf("创建凭证失败: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/servers/"+id+"/credentials?page_size=0", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ctrl.handleListCredentials(rec, req)
	e := decodeEnvelope(t, rec)
	body := rec.Body.String()
	if strings.Contains(body, "super-secret") || strings.Contains(body, "another-secret") {
		t.Fatalf("凭证列表不得回传明文: %s", body)
	}
	var data struct {
		Items []model.Credential `json:"items"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("解析凭证列表失败: %v / %s", err, e.Data)
	}
	if len(data.Items) != 2 {
		t.Fatalf("page_size=0 应返回全部凭证，得到 %d", len(data.Items))
	}
	for _, cred := range data.Items {
		if !cred.HasValue || cred.Value != "" {
			t.Fatalf("列表应只回元数据（has_value=true、value 为空）: %+v", cred)
		}
	}
}

// TestControlUpdateServerPrimaryInstance 验证 PATCH /api/servers/{id} 可同时更新描述与
// 主实例 endpoint/transport/args，并即时触发探活。
func TestControlUpdateServerPrimaryInstance(t *testing.T) {
	ctrl, store := newInstancesControl(nil)
	id := seedControlServer(t, ctrl)
	probed := 0
	ctrl.probeNow = func(string) { probed++ }

	body := `{"description":"新描述","endpoint":"./bin/mock-mcp","transport":"stdio","args":["-stdio"]}`
	req := httptest.NewRequest(http.MethodPatch, "/api/servers/"+id, strings.NewReader(body))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ctrl.handleUpdateServer(rec, req)
	e := decodeEnvelope(t, rec)

	var srv struct {
		Description string `json:"description"`
		Instances   []struct {
			Endpoint  string   `json:"endpoint"`
			Transport string   `json:"transport"`
			Args      []string `json:"args"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(e.Data, &srv); err != nil {
		t.Fatalf("解析更新响应失败: %v", err)
	}
	if srv.Description != "新描述" {
		t.Fatalf("描述未更新: %+v", srv)
	}
	if len(srv.Instances) != 1 || srv.Instances[0].Transport != "stdio" ||
		srv.Instances[0].Endpoint != "./bin/mock-mcp" || len(srv.Instances[0].Args) != 1 {
		t.Fatalf("主实例未更新: %+v", srv.Instances)
	}
	if probed != 1 {
		t.Fatalf("主实例变更应触发一次即时探活，得到 %d", probed)
	}
	insts, _ := store.ListInstancesByServer(context.Background(), id)
	if len(insts) != 1 || insts[0].HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("主实例配置变更后健康应复位: %+v", insts)
	}
}
