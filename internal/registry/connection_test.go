package registry

import (
	"context"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// fakeDraftProber 记录草稿拨测入参并返回预设结论。
type fakeDraftProber struct {
	calls       int
	gotServer   model.Server
	gotInstance model.Instance
	gotHeaders  map[string]string
	status      model.ServerStatus
	err         error
}

func (f *fakeDraftProber) CheckDraft(_ context.Context, server model.Server, instance model.Instance, headers map[string]string) (model.ServerStatus, error) {
	f.calls++
	f.gotServer, f.gotInstance, f.gotHeaders = server, instance, headers
	if f.status == "" {
		f.status = model.ServerStatusHealthy
	}
	return f.status, f.err
}

// seedDraftServer 注册一个 Server 并挂两条凭证（static_token + api_key）。
func seedDraftServer(t *testing.T, store *memory.Store, svc *Service) model.Server {
	t.Helper()
	ctx := context.Background()
	created, err := svc.CreateServer(ctx, CreateServerInput{
		Name: "Draft", Endpoint: "http://a:9000/mcp", Transport: model.TransportStreamableHTTP,
	})
	if err != nil {
		t.Fatalf("创建 Server 失败: %v", err)
	}
	creds := []*model.Credential{
		{ServerID: created.ID, Name: "token", Kind: model.CredentialStaticToken, Value: "saved-token"},
		{ServerID: created.ID, Name: "key", Kind: model.CredentialAPIKey, Header: "X-Upstream-Key", Value: "saved-key"},
	}
	for _, cred := range creds {
		if err := store.CreateCredential(ctx, cred); err != nil {
			t.Fatalf("创建凭证失败: %v", err)
		}
	}
	return *created
}

// TestConnectionMergesSavedCredentialsAndTempHeaders 验证编辑已有 Server 时：
// 已保存凭据自动加载（static_token → Authorization: Bearer、api_key → 其 header），
// 请求体中的临时 Header 覆盖同名已保存 Header，且不影响其他头。
func TestConnectionMergesSavedCredentialsAndTempHeaders(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})
	created := seedDraftServer(t, store, svc)

	prober := &fakeDraftProber{}
	svc.WithDraftProber(prober)

	status, err := svc.TestConnection(ctx, TestConnectionInput{
		ServerID:  created.ID,
		Name:      "ignored-name",
		Endpoint:  " http://b:9100/mcp ",
		Transport: model.TransportStreamableHTTP,
		Headers:   map[string]string{"X-Upstream-Key": "temp-key", "X-Extra": "v"},
	})
	if err != nil {
		t.Fatalf("TestConnection 失败: %v", err)
	}
	if status != model.ServerStatusHealthy {
		t.Fatalf("草稿探测应返回 healthy，得到 %s", status)
	}
	if prober.calls != 1 {
		t.Fatalf("草稿探针应被调用 1 次，得到 %d", prober.calls)
	}
	// endpoint 去空白后拨测；名称取自存储（错误文案贴合上下文）。
	if prober.gotInstance.Endpoint != "http://b:9100/mcp" || prober.gotInstance.Transport != model.TransportStreamableHTTP {
		t.Fatalf("草稿实例参数不正确: %+v", prober.gotInstance)
	}
	if prober.gotServer.Name != "Draft" || prober.gotServer.ID != created.ID {
		t.Fatalf("草稿 Server 应复用已保存名称/ID: %+v", prober.gotServer)
	}
	want := map[string]string{
		"Authorization":  "Bearer saved-token",
		"X-Upstream-Key": "temp-key", // 临时头覆盖已保存头
		"X-Extra":        "v",
	}
	if len(prober.gotHeaders) != len(want) {
		t.Fatalf("header 集合不正确: %+v", prober.gotHeaders)
	}
	for k, v := range want {
		if prober.gotHeaders[k] != v {
			t.Fatalf("header %q 应为 %q，得到 %q（全部 %+v）", k, v, prober.gotHeaders[k], prober.gotHeaders)
		}
	}
}

// TestConnectionHasNoSideEffects 验证草稿拨测不落库：不创建 Server/实例、不刷新
// Tools、不改健康状态、不保存临时 Header。
func TestConnectionHasNoSideEffects(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{tools: []DiscoveredTool{{Name: "search"}}})
	created := seedDraftServer(t, store, svc)
	beforeInstances, _ := store.ListInstancesByServer(ctx, created.ID)
	beforeTools, _ := store.ListToolsByServer(ctx, created.ID)
	beforeCreds, _ := store.ListCredentialsByServer(ctx, created.ID)
	beforeServer, err := store.GetServer(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}

	prober := &fakeDraftProber{}
	svc.WithDraftProber(prober)
	if _, err := svc.TestConnection(ctx, TestConnectionInput{
		ServerID: created.ID,
		Endpoint: "http://a:9000/mcp",
		Headers:  map[string]string{"X-Temp": "v"},
	}); err != nil {
		t.Fatalf("TestConnection 失败: %v", err)
	}

	servers, _ := store.ListServers(ctx)
	if len(servers) != 1 {
		t.Fatalf("草稿拨测不应创建 Server，得到 %d 个", len(servers))
	}
	afterInstances, _ := store.ListInstancesByServer(ctx, created.ID)
	if len(afterInstances) != len(beforeInstances) {
		t.Fatalf("草稿拨测不应创建实例: before=%d after=%d", len(beforeInstances), len(afterInstances))
	}
	afterTools, _ := store.ListToolsByServer(ctx, created.ID)
	if len(afterTools) != len(beforeTools) {
		t.Fatalf("草稿拨测不应刷新 Tools: before=%d after=%d", len(beforeTools), len(afterTools))
	}
	afterCreds, _ := store.ListCredentialsByServer(ctx, created.ID)
	if len(afterCreds) != len(beforeCreds) {
		t.Fatalf("草稿拨测不应保存临时 Header: before=%d after=%d", len(beforeCreds), len(afterCreds))
	}
	// 健康状态保持原值（未拨测成功也不改存储）。
	got, err := store.GetServer(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.HealthStatus != beforeServer.HealthStatus {
		t.Fatalf("草稿拨测不应更新健康状态: before=%s after=%s", beforeServer.HealthStatus, got.HealthStatus)
	}
}

// TestConnectionWithoutServerIDUsesTempHeadersOnly 验证新增流程（无 server_id）
// 只用临时 Header，不读取任何已保存凭据。
func TestConnectionWithoutServerIDUsesTempHeadersOnly(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})
	prober := &fakeDraftProber{}
	svc.WithDraftProber(prober)

	if _, err := svc.TestConnection(ctx, TestConnectionInput{
		Name:      "Demo",
		Endpoint:  "http://localhost:9000/mcp",
		Transport: model.TransportStreamableHTTP,
		Headers:   map[string]string{"Authorization": "Bearer xxx"},
	}); err != nil {
		t.Fatalf("TestConnection 失败: %v", err)
	}
	if prober.gotServer.Name != "Demo" || prober.gotHeaders["Authorization"] != "Bearer xxx" {
		t.Fatalf("新增草稿应使用提交的名称与临时头: %+v / %+v", prober.gotServer, prober.gotHeaders)
	}
	if servers, _ := store.ListServers(ctx); len(servers) != 0 {
		t.Fatalf("草稿拨测不应创建 Server，得到 %d 个", len(servers))
	}
}

// TestConnectionValidatesDraft 验证 endpoint/transport/stdio args 的校验拦截，
// 且校验失败时不触发拨测。
func TestConnectionValidatesDraft(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})
	prober := &fakeDraftProber{}
	svc.WithDraftProber(prober)

	cases := []struct {
		name string
		in   TestConnectionInput
	}{
		{"空 endpoint", TestConnectionInput{Endpoint: "  "}},
		{"endpoint 非绝对 URL", TestConnectionInput{Endpoint: "localhost:9000"}},
		{"endpoint 协议不支持", TestConnectionInput{Endpoint: "ftp://localhost/mcp"}},
		{"未知传输", TestConnectionInput{Endpoint: "http://a:9000/mcp", Transport: "grpc"}},
		{"https 携带 args", TestConnectionInput{Endpoint: "http://a:9000/mcp", Transport: model.TransportStreamableHTTP, Args: []string{"-x"}}},
		{"stdio 缺命令", TestConnectionInput{Endpoint: " ", Transport: model.TransportStdio}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.TestConnection(ctx, tc.in); !errs.Is(err, errs.CodeInvalidArgument) {
				t.Fatalf("应返回 invalid_argument，得到 %v", err)
			}
		})
	}
	if prober.calls != 0 {
		t.Fatalf("校验失败不应触发拨测，实际调用 %d 次", prober.calls)
	}

	// 未知 server_id 按 not_found 拒绝（不静默按新增处理）。
	if _, err := svc.TestConnection(ctx, TestConnectionInput{
		ServerID: "nope", Endpoint: "http://a:9000/mcp",
	}); !errs.Is(err, errs.CodeNotFound) {
		t.Fatalf("未知 server_id 应返回 not_found，得到 %v", err)
	}
}

// TestConnectionWithoutProber 验证未装配草稿探针时返回内部错误而非静默通过。
func TestConnectionWithoutProber(t *testing.T) {
	svc := NewService(memory.New(), &fakeDiscoverer{})
	if _, err := svc.TestConnection(context.Background(), TestConnectionInput{Endpoint: "http://a:9000/mcp"}); !errs.Is(err, errs.CodeInternal) {
		t.Fatalf("未装配草稿探针应返回 internal_error，得到 %v", err)
	}
}

// TestUpdateServerRenamesName 验证 Server 名称可改：name 只是展示用（对外稳定标识
// 是 Server ID），改名不触碰实例与工具，空名被拒绝。
func TestUpdateServerRenamesName(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{tools: []DiscoveredTool{{Name: "search"}}})
	created := seedDraftServer(t, store, svc)
	beforeInstances, _ := store.ListInstancesByServer(ctx, created.ID)
	beforeTools, _ := store.ListToolsByServer(ctx, created.ID)

	name := "  Renamed Server  "
	updated, err := svc.UpdateServer(ctx, created.ID, UpdateServerPatch{Name: &name})
	if err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	if updated.Name != "Renamed Server" {
		t.Fatalf("名称应去空白后落库，得到 %q", updated.Name)
	}
	if updated.ID != created.ID {
		t.Fatalf("改名不应改变 id: %q -> %q", created.ID, updated.ID)
	}
	afterInstances, _ := store.ListInstancesByServer(ctx, created.ID)
	afterTools, _ := store.ListToolsByServer(ctx, created.ID)
	if len(afterInstances) != len(beforeInstances) || len(afterTools) != len(beforeTools) {
		t.Fatalf("改名不应触碰实例/工具: instances %d->%d tools %d->%d",
			len(beforeInstances), len(afterInstances), len(beforeTools), len(afterTools))
	}

	empty := "   "
	if _, err := svc.UpdateServer(ctx, created.ID, UpdateServerPatch{Name: &empty}); !errs.Is(err, errs.CodeInvalidArgument) {
		t.Fatalf("空名称应返回 invalid_argument，得到 %v", err)
	}
	if got, _ := store.GetServer(ctx, created.ID); got.Name != "Renamed Server" {
		t.Fatalf("被拒绝的改名不应落库，得到 %q", got.Name)
	}
}

// TestUpdateServerUpdatesPrimaryInstance 验证 PATCH Server 可同时更新描述与主实例
// （最早创建的实例）的 endpoint/transport/args，其余实例不受影响，且主实例健康复位。
func TestUpdateServerUpdatesPrimaryInstance(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	svc := NewService(store, &fakeDiscoverer{})
	created := seedDraftServer(t, store, svc)

	second, err := svc.AddInstance(ctx, created.ID, "http://second:9000/mcp", model.TransportStreamableHTTP, nil)
	if err != nil {
		t.Fatalf("新增第二实例失败: %v", err)
	}
	instances, _ := store.ListInstancesByServer(ctx, created.ID)
	primaryID := instances[0].ID

	desc := "新描述"
	endpoint := "http://primary-new:9000/mcp"
	transport := string(model.TransportStdio)
	args := []string{"-stdio"}
	updated, err := svc.UpdateServer(ctx, created.ID, UpdateServerPatch{
		Description: &desc,
		Endpoint:    &endpoint,
		Transport:   &transport,
		Args:        &args,
	})
	if err != nil {
		t.Fatalf("UpdateServer 失败: %v", err)
	}
	if updated.Description != desc || updated.Name != "Draft" {
		t.Fatalf("逻辑字段更新不正确: %+v", updated)
	}

	after, _ := store.ListInstancesByServer(ctx, created.ID)
	if after[0].ID != primaryID || after[0].Endpoint != endpoint ||
		after[0].Transport != model.TransportStdio || len(after[0].Args) != 1 {
		t.Fatalf("主实例应被更新: %+v", after[0])
	}
	if after[0].HealthStatus != model.ServerStatusUnknown {
		t.Fatalf("主实例配置变更后健康应复位为 unknown，得到 %s", after[0].HealthStatus)
	}
	for _, inst := range after {
		if inst.ID == second.ID && inst.Endpoint != "http://second:9000/mcp" {
			t.Fatalf("其余实例不应受影响: %+v", inst)
		}
	}

	// 仅描述补丁不触碰实例。
	desc2 := "只改描述"
	if _, err := svc.UpdateServer(ctx, created.ID, UpdateServerPatch{Description: &desc2}); err != nil {
		t.Fatalf("仅描述更新失败: %v", err)
	}
	after2, _ := store.ListInstancesByServer(ctx, created.ID)
	if after2[0].Endpoint != endpoint {
		t.Fatalf("仅描述更新不应改动实例: %+v", after2[0])
	}
}
