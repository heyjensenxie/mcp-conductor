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

	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/storage"
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

func testServer(name, endpoint string) *model.Server {
	return &model.Server{Name: name, Description: "集成测试", Endpoint: endpoint, Transport: model.TransportStreamableHTTP}
}

func TestServerCRUD(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("集成-Server", "http://localhost:9000/mcp")
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
	if got.Name != srv.Name || got.Transport != model.TransportStreamableHTTP || got.Enabled {
		t.Fatalf("Server 字段回读不一致: %+v", got)
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

func TestServerUpdateMissing(t *testing.T) {
	store := openTest(t)
	if err := store.UpdateServer(context.Background(), &model.Server{ID: "no-such-id", UpdatedAt: time.Now().UTC()}); err == nil {
		t.Fatal("更新不存在的 Server 应报错")
	}
}

func TestToolUpsertIdempotent(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	srv := testServer("工具-Server", "http://localhost:9000/mcp")
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

	srv := testServer("级联-Server", "http://localhost:9000/mcp")
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

	srv := testServer("支撑-Server", "http://localhost:9000/mcp")
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

	srv := testServer("密钥-Server", "http://localhost:9000/mcp")
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

	srv := testServer("无密钥-Server", "http://localhost:9000/mcp")
	_ = store.CreateServer(ctx, srv)

	err := store.CreateCredential(ctx, &model.Credential{ServerID: srv.ID, Name: "x", Kind: model.CredentialAPIKey, Header: "X", Value: "secret"})
	if err == nil {
		t.Fatal("未配置密钥时应拒绝保存非空凭据值")
	}
}

func TestPolicyRulesRoundTrip(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	policy := &model.Policy{
		Name:    "支付风险",
		Enabled: true,
		Rules: []model.PolicyRule{
			{Subject: "anon", Tool: "payment.create", Effect: model.PolicyEffectDeny},
			{Subject: "*", Tool: "payment.*", Effect: model.PolicyEffectDeny},
		},
	}
	if err := store.CreatePolicy(ctx, policy); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	got, err := store.GetPolicy(ctx, policy.ID)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if len(got.Rules) != 2 || got.Rules[0].Effect != model.PolicyEffectDeny {
		t.Fatalf("策略规则回读不一致: %+v", got.Rules)
	}

	policies, err := store.ListPolicies(ctx)
	if err != nil || len(policies) == 0 {
		t.Fatalf("ListPolicies: %v / %d", err, len(policies))
	}
	found := false
	for _, p := range policies {
		if p.ID == policy.ID && len(p.Rules) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("ListPolicies 应包含刚建的策略及其规则")
	}
}

func TestTrafficAppendRecent(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := store.AppendTraffic(ctx, model.TrafficSample{
			RequestID: "req-" + string(rune('a'+i)),
			ServerID:  "srv-1", Tool: "mock.search", Status: "success",
			LatencyMS: int64(i + 1), Timestamp: now,
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
}
