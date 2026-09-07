// Package mysql 集成测试：依赖真实 MySQL 5.7，通过 MYSQL_TEST_DSN 启用。
//
//	MYSQL_TEST_DSN='conductor:conductor@tcp(localhost:33061)/conductor?parseTime=true&loc=UTC&charset=utf8mb4' \
//	  go test ./internal/storage/mysql/ -v
package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// openTest 从环境变量打开被测 Store；未配置 DSN 时跳过。
func openTest(t *testing.T) storage.Store {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN 未设置，跳过 MySQL 集成测试")
	}
	store, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("打开 MySQL 失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// testServer 构造一个逻辑 Server（不含 Endpoint/Transport——它们属于实例，
// 见 model.Server / Instance 注释）。
func testServer(name string) *model.Server {
	return &model.Server{Name: name, Description: "集成测试"}
}

func TestServerCRUD(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("集成-Server")
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.ID == "" {
		t.Fatal("CreateServer 应生成 id")
	}

	got, err := store.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.Name != srv.Name || got.Enabled {
		t.Fatalf("Server 字段回读不一致: %+v", got)
	}

	// Endpoint/Transport 属于实例层：seed 一条并断言其回读。
	inst := &model.Instance{ServerID: srv.ID, Endpoint: "http://localhost:9000/mcp", Transport: model.TransportStreamableHTTP, Enabled: true, HealthStatus: model.ServerStatusUnknown}
	if err := store.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	insts, err := store.ListInstancesByServer(ctx, srv.ID)
	if err != nil || len(insts) != 1 {
		t.Fatalf("ListInstancesByServer: %v / %d", err, len(insts))
	}
	if insts[0].Endpoint != "http://localhost:9000/mcp" || insts[0].Transport != model.TransportStreamableHTTP {
		t.Fatalf("实例 endpoint/transport 回读不一致: %+v", insts[0])
	}

	// stdio 实例：endpoint 承载命令、args 落 TEXT(JSON) 列并回读。
	stdioInst := &model.Instance{ServerID: srv.ID, Endpoint: "./bin/mock-mcp", Transport: model.TransportStdio, Args: []string{"-stdio", "--port", "9100"}, Enabled: true, HealthStatus: model.ServerStatusUnknown}
	if err := store.CreateInstance(ctx, stdioInst); err != nil {
		t.Fatalf("CreateInstance(stdio): %v", err)
	}
	rows, err := store.ListInstancesByServer(ctx, srv.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("ListInstancesByServer 应含 2 条: %v / %d", err, len(rows))
	}
	for _, r := range rows {
		if r.Transport == model.TransportStdio {
			if len(r.Args) != 3 || r.Args[0] != "-stdio" || r.Args[1] != "--port" {
				t.Fatalf("stdio 实例 args 回读不一致: %+v", r)
			}
		}
	}

	got.Enabled = true
	if err := store.UpdateServer(ctx, got); err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}
	again, _ := store.GetServer(ctx, srv.ID)
	if !again.Enabled {
		t.Fatal("UpdateServer 未生效")
	}

	all, err := store.ListServers(ctx)
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("ListServers 应为非空")
	}

	if err := store.DeleteServer(ctx, srv.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if _, err := store.GetServer(ctx, srv.ID); err == nil {
		t.Fatal("删除后 GetServer 应报错")
	}
}

// TestServerInstanceCascadeMySQL 验证删除 Server 级联删除其实例（FK CASCADE）。
func TestServerInstanceCascadeMySQL(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("实例级联")
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	for _, ep := range []string{"http://a:9000/mcp", "http://b:9000/mcp"} {
		if err := store.CreateInstance(ctx, &model.Instance{ServerID: srv.ID, Endpoint: ep, Transport: model.TransportStreamableHTTP, Enabled: true, HealthStatus: model.ServerStatusUnknown}); err != nil {
			t.Fatalf("CreateInstance(%s): %v", ep, err)
		}
	}
	if insts, _ := store.ListInstancesByServer(ctx, srv.ID); len(insts) != 2 {
		t.Fatalf("应有两实例，得到 %d", len(insts))
	}
	if err := store.DeleteServer(ctx, srv.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if insts, _ := store.ListInstancesByServer(ctx, srv.ID); len(insts) != 0 {
		t.Fatalf("删除 Server 后实例应被级联清理，得到 %d", len(insts))
	}
}

// TestInstanceCRUDMySQL 验证实例 创建/读取/更新/删除 与按 Server 整组删除。
func TestInstanceCRUDMySQL(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("实例CRUD")
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	defer func() { _ = store.DeleteServer(ctx, srv.ID) }()

	inst := &model.Instance{ServerID: srv.ID, Endpoint: "http://a:9000/mcp", Transport: model.TransportStreamableHTTP, Enabled: true, HealthStatus: model.ServerStatusUnknown}
	if err := store.CreateInstance(ctx, inst); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	if inst.ID == "" {
		t.Fatal("CreateInstance 应生成 id")
	}
	got, err := store.GetInstance(ctx, inst.ID)
	if err != nil || got.Endpoint != "http://a:9000/mcp" {
		t.Fatalf("GetInstance 回读不一致: %+v / %v", got, err)
	}

	got.Endpoint = "http://b:9000/mcp"
	got.HealthStatus = model.ServerStatusHealthy
	if err := store.UpdateInstance(ctx, got); err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}
	again, _ := store.GetInstance(ctx, inst.ID)
	if again.Endpoint != "http://b:9000/mcp" || again.HealthStatus != model.ServerStatusHealthy {
		t.Fatalf("UpdateInstance 未生效: %+v", again)
	}

	// 加第二条后 DeleteInstancesByServer 整组清理。
	if err := store.CreateInstance(ctx, &model.Instance{ServerID: srv.ID, Endpoint: "http://c:9000/mcp", Transport: model.TransportStreamableHTTP}); err != nil {
		t.Fatalf("CreateInstance 2nd: %v", err)
	}
	if err := store.DeleteInstancesByServer(ctx, srv.ID); err != nil {
		t.Fatalf("DeleteInstancesByServer: %v", err)
	}
	if insts, _ := store.ListInstancesByServer(ctx, srv.ID); len(insts) != 0 {
		t.Fatalf("整组删除后应无实例，得到 %d", len(insts))
	}
}

func TestServerUpdateMissing(t *testing.T) {
	store := openTest(t)
	if err := store.UpdateServer(context.Background(), &model.Server{ID: "no-such-id", UpdatedAt: model.Now()}); err == nil {
		t.Fatal("更新不存在的 Server 应报错")
	}
}

func TestToolUpsertIdempotent(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("工具-Server")
	_ = store.CreateServer(ctx, srv)

	tool := &model.Tool{
		ServerID:     srv.ID,
		OriginalName: "search",
		GatewayName:  "工具_server.search",
		Description:  "查询",
		InputSchema:  map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
		Enabled:      true,
	}
	if err := store.UpsertTool(ctx, tool); err != nil {
		t.Fatalf("UpsertTool 首次: %v", err)
	}
	if tool.ID == "" {
		t.Fatal("UpsertTool 应生成 id")
	}

	// 同一 gateway_name 二次写入：保留原 id，刷新描述。
	tool.Description = "查询（更新）"
	tool.ID = "" // 模拟新工具对象
	if err := store.UpsertTool(ctx, tool); err != nil {
		t.Fatalf("UpsertTool 二次: %v", err)
	}
	got, err := store.GetToolByGatewayName(ctx, tool.GatewayName)
	if err != nil {
		t.Fatalf("GetToolByGatewayName: %v", err)
	}
	if got.ID != tool.ID || got.Description != "查询（更新）" {
		t.Fatalf("Upsert 应保留 id 且更新字段，得到 %+v", got)
	}
	if got.InputSchema == nil {
		t.Fatal("InputSchema 应正确回读")
	}

	// 列表查询
	if tools, _ := store.ListToolsByServer(ctx, srv.ID); len(tools) != 1 {
		t.Fatalf("ListToolsByServer 应返回 1 条，得到 %d", len(tools))
	}
	if tools, _ := store.ListTools(ctx); len(tools) < 1 {
		t.Fatal("ListTools 应非空")
	}

	if err := store.DeleteToolsByServer(ctx, srv.ID); err != nil {
		t.Fatalf("DeleteToolsByServer: %v", err)
	}
}

func TestServerCascadeDeletesTools(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("级联-Server")
	_ = store.CreateServer(ctx, srv)
	tool := &model.Tool{ServerID: srv.ID, OriginalName: "search", GatewayName: "cascade.search", Enabled: true}
	_ = store.UpsertTool(ctx, tool)

	_ = store.DeleteServer(ctx, srv.ID)
	if _, err := store.GetTool(ctx, tool.ID); err == nil {
		t.Fatal("删除 Server 后其 Tool 应被级联删除")
	}
}

func TestRouteAndCredential(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("支撑-Server")
	_ = store.CreateServer(ctx, srv)

	route := &model.Route{Name: "默认路由", ServerID: srv.ID, ToolNames: []string{"a.foo", "b.bar"}, Enabled: true}
	if err := store.CreateRoute(ctx, route); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	routes, err := store.ListRoutes(ctx)
	if err != nil || len(routes) == 0 {
		t.Fatalf("ListRoutes: %v / %d", err, len(routes))
	}
	if len(routes[0].ToolNames) != 2 {
		t.Fatalf("Route.tool_names 应回读 2 项: %+v", routes[0].ToolNames)
	}

	// 未配置值：仅登记元数据，HasValue=false。
	cred := &model.Credential{ServerID: srv.ID, Name: "上游密钥", Kind: model.CredentialAPIKey, Header: "X-Upstream-Key"}
	if err := store.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	creds, err := store.ListCredentialsByServer(ctx, srv.ID)
	if err != nil || len(creds) != 1 {
		t.Fatalf("ListCredentialsByServer: %v / %d", err, len(creds))
	}
	if creds[0].Header != "X-Upstream-Key" || creds[0].HasValue {
		t.Fatalf("Credential 元数据回读不一致: %+v", creds[0])
	}
}

// openTestKeyed 打开带加密密钥的 Store（用于凭据值集成测试）。
func openTestKeyed(t *testing.T) storage.Store {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN 未设置，跳过 MySQL 集成测试")
	}
	store, err := Open(context.Background(), dsn, WithCredentialKey(testCredentialKeyHex))
	if err != nil {
		t.Fatalf("打开 MySQL 失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestCredentialEncryptedRoundTrip 验证凭据值加密落库并可解密回读。
func TestCredentialEncryptedRoundTrip(t *testing.T) {
	store := openTestKeyed(t)
	ctx := context.Background()

	srv := testServer("密钥-Server")
	_ = store.CreateServer(ctx, srv)

	cred := &model.Credential{ServerID: srv.ID, Name: "上游密钥", Kind: model.CredentialAPIKey, Header: "X-Upstream-Key", Value: "plain-secret"}
	if err := store.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if !cred.HasValue {
		t.Fatal("写入非空值后 HasValue 应为 true")
	}

	creds, err := store.ListCredentialsByServer(ctx, srv.ID)
	if err != nil || len(creds) != 1 {
		t.Fatalf("List: %v / %d", err, len(creds))
	}
	if creds[0].Value != "plain-secret" || !creds[0].HasValue {
		t.Fatalf("值应可解密回读且 HasValue 为 true: %+v", creds[0])
	}
}

// TestCredentialCreateWithoutKeyRejectsValue 验证无密钥时拒绝落库明文。
func TestCredentialCreateWithoutKeyRejectsValue(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("无密钥-Server")
	_ = store.CreateServer(ctx, srv)

	err := store.CreateCredential(ctx, &model.Credential{ServerID: srv.ID, Name: "x", Kind: model.CredentialAPIKey, Header: "X", Value: "secret"})
	if err == nil {
		t.Fatal("未配置密钥时应拒绝保存非空凭据值")
	}
}

// TestCredentialLifecycleMySQL 验证凭证更新（空值保留原值 / 值覆盖）与删除。
func TestCredentialLifecycleMySQL(t *testing.T) {
	store := openTestKeyed(t)
	ctx := context.Background()

	srv := testServer("凭证生命周期")
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	cred := &model.Credential{ServerID: srv.ID, Name: "密钥", Kind: model.CredentialAPIKey, Header: "X-Key", Value: "v1"}
	if err := store.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	// 仅改元数据，value 空值保留原值。
	if err := store.UpdateCredential(ctx, &model.Credential{ID: cred.ID, ServerID: srv.ID, Name: "改名", Kind: model.CredentialStaticToken}); err != nil {
		t.Fatalf("UpdateCredential(元数据): %v", err)
	}
	creds, err := store.ListCredentialsByServer(ctx, srv.ID)
	if err != nil || len(creds) != 1 {
		t.Fatalf("List: %v / %d", err, len(creds))
	}
	if creds[0].Name != "改名" || creds[0].Kind != model.CredentialStaticToken || creds[0].Value != "v1" || !creds[0].HasValue {
		t.Fatalf("更新后回读不一致: %+v", creds[0])
	}

	// 换新值并回读（解密）。
	if err := store.UpdateCredential(ctx, &model.Credential{ID: cred.ID, ServerID: srv.ID, Value: "v2"}); err != nil {
		t.Fatalf("UpdateCredential(值): %v", err)
	}
	creds, err = store.ListCredentialsByServer(ctx, srv.ID)
	if err != nil || len(creds) != 1 || creds[0].Value != "v2" {
		t.Fatalf("更新值后回读不一致: %+v / %v", creds, err)
	}

	// 删除后列表为空。
	if err := store.DeleteCredential(ctx, cred.ID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if creds, _ := store.ListCredentialsByServer(ctx, srv.ID); len(creds) != 0 {
		t.Fatalf("删除后应无凭证，得到 %d", len(creds))
	}
}

// TestToolSetEnabledMySQL 验证工具启停（含索引一致性与幂等）。
func TestToolSetEnabledMySQL(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("工具启停")
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	tool := &model.Tool{ServerID: srv.ID, OriginalName: "search", GatewayName: "启停.search", Enabled: true}
	if err := store.UpsertTool(ctx, tool); err != nil {
		t.Fatalf("UpsertTool: %v", err)
	}

	if err := store.SetToolEnabled(ctx, tool.ID, false); err != nil {
		t.Fatalf("SetToolEnabled: %v", err)
	}
	got, err := store.GetToolByGatewayName(ctx, tool.GatewayName)
	if err != nil || got.Enabled {
		t.Fatalf("禁用具应生效: %+v / %v", got, err)
	}
	// 幂等。
	if err := store.SetToolEnabled(ctx, tool.ID, false); err != nil {
		t.Fatalf("幂等设置失败: %v", err)
	}
	if err := store.SetToolEnabled(ctx, "missing-tool", true); err == nil {
		t.Fatal("不存在的工具应报错")
	}
}

// TestRouteLifecycleMySQL 验证路由 创建/读取/更新/删除。
func TestRouteLifecycleMySQL(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("路由生命周期")
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	route := &model.Route{Name: "主路由", ServerID: srv.ID, ToolNames: []string{"a.search"}, Enabled: true}
	if err := store.CreateRoute(ctx, route); err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	got, err := store.GetRoute(ctx, route.ID)
	if err != nil || got.Name != "主路由" || len(got.ToolNames) != 1 {
		t.Fatalf("GetRoute: %+v / %v", got, err)
	}
	if err := store.UpdateRoute(ctx, &model.Route{ID: route.ID, Name: "改名", ServerID: srv.ID, ToolNames: []string{"b.search"}, Enabled: false}); err != nil {
		t.Fatalf("UpdateRoute: %v", err)
	}
	got, _ = store.GetRoute(ctx, route.ID)
	if got.Name != "改名" || got.Enabled || len(got.ToolNames) != 1 || got.ToolNames[0] != "b.search" {
		t.Fatalf("更新后回读不一致: %+v", got)
	}

	if err := store.DeleteRoute(ctx, route.ID); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}
	if _, err := store.GetRoute(ctx, route.ID); err == nil {
		t.Fatal("删除后 GetRoute 应报错")
	}
}

func TestTrafficAppendRecent(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := store.AppendTraffic(ctx, model.TrafficSample{
			RequestID:  "req-" + string(rune('a'+i)),
			ServerID:   "srv-1",
			InstanceID: "inst-1",
			Tool:       "mock.search", Status: "success",
			ClientIP:  "198.51.100.7",
			LatencyMS: int64(i + 1), Timestamp: model.T(now),
		}); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	samples, err := store.RecentTraffic(ctx, 2)
	if err != nil {
		t.Fatalf("RecentTraffic: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("RecentTraffic(2) 应返回 2 条，得到 %d", len(samples))
	}
	if samples[0].LatencyMS != 3 { // 倒序，最新在前
		t.Fatalf("RecentTraffic 应按写入倒序: %+v", samples)
	}
	if samples[0].InstanceID != "inst-1" || samples[1].InstanceID != "inst-1" {
		t.Fatalf("RecentTraffic 实例字段应往返: %+v", samples)
	}
	if samples[0].ClientIP != "198.51.100.7" {
		t.Fatalf("RecentTraffic client_ip 应往返: %+v", samples[0])
	}
}

// 需要迁移 0003_add_access_keys.sql 已执行。
func TestAccessKeyCRUD(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	key := &model.AccessKey{
		Name: "Partner A", Subject: "partner-a", Enabled: true, QPS: 5, Burst: 10,
		KeyHash: "hash-1",
		Grants: []model.ToolGrant{
			{GatewayName: "mock.search", Headers: map[string]string{"X-Tenant": "a"}, DefaultArgs: map[string]any{"env": "prod"}},
		},
	}
	if err := store.CreateAccessKey(ctx, key); err != nil {
		t.Fatalf("CreateAccessKey: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteAccessKey(ctx, key.ID) })
	if key.ID == "" {
		t.Fatal("CreateAccessKey 应生成 id")
	}

	// byHash / bySubject / byID 三种途径均能读回 grants 与配额。
	byHash, err := store.GetAccessKeyByKeyHash(ctx, "hash-1")
	if err != nil || byHash.Subject != "partner-a" || len(byHash.Grants) != 1 {
		t.Fatalf("GetAccessKeyByKeyHash: %v / %+v", err, byHash)
	}
	if byHash.Grants[0].Headers["X-Tenant"] != "a" || byHash.Grants[0].DefaultArgs["env"] != "prod" {
		t.Fatalf("grants JSON 回读不一致: %+v", byHash.Grants)
	}
	bySubj, err := store.GetAccessKeyBySubject(ctx, "partner-a")
	if err != nil || bySubj.QPS != 5 {
		t.Fatalf("GetAccessKeyBySubject: %v / %+v", err, bySubj)
	}

	// 更新 grants 为整体替换。
	updated := *byHash
	updated.Grants = []model.ToolGrant{{GatewayName: "mock.*"}}
	updated.Enabled = false
	if err := store.UpdateAccessKey(ctx, &updated); err != nil {
		t.Fatalf("UpdateAccessKey: %v", err)
	}
	again, err := store.GetAccessKey(ctx, updated.ID)
	if err != nil || len(again.Grants) != 1 || again.Grants[0].GatewayName != "mock.*" || again.Enabled {
		t.Fatalf("Update 后回读不一致: %v / %+v", err, again)
	}

	// List 稳定读取。
	all, err := store.ListAccessKeys(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("ListAccessKeys: %v / %d", err, len(all))
	}
}
