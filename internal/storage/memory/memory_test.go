package memory

import (
	"context"
	"testing"
	"time"

	"github.com/xmj128/mcp-conductor/internal/model"
)

func TestCredentialValueKeptInMemory(t *testing.T) {
	store := New()
	ctx := context.Background()

	server := &model.Server{Name: "S", Endpoint: "http://x", Enabled: true}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	cred := &model.Credential{ServerID: server.ID, Name: "密钥", Kind: model.CredentialAPIKey, Header: "X-Key", Value: "mem-secret"}
	if err := store.CreateCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}
	creds, err := store.ListCredentialsByServer(ctx, server.ID)
	if err != nil || len(creds) != 1 {
		t.Fatalf("List: %v / %d", err, len(creds))
	}
	if creds[0].Value != "mem-secret" {
		t.Fatalf("memory 应保留值，得到 %q", creds[0].Value)
	}
}

// TestCredentialLifecycleMemory 验证凭证更新（空值保留原值 / 新值覆盖）与删除。
func TestCredentialLifecycleMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	server := &model.Server{Name: "S", Endpoint: "http://x", Enabled: true}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	cred := &model.Credential{ServerID: server.ID, Name: "密钥", Kind: model.CredentialAPIKey, Header: "X-Key", Value: "v1"}
	if err := store.CreateCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}

	// 仅改元数据，value 空值应保留原值。
	if err := store.UpdateCredential(ctx, &model.Credential{ID: cred.ID, ServerID: server.ID, Name: "改名", Kind: model.CredentialStaticToken}); err != nil {
		t.Fatalf("UpdateCredential(元数据): %v", err)
	}
	creds, _ := store.ListCredentialsByServer(ctx, server.ID)
	if len(creds) != 1 || creds[0].Name != "改名" || creds[0].Kind != model.CredentialStaticToken ||
		creds[0].Value != "v1" || !creds[0].HasValue {
		t.Fatalf("更新后回读不一致: %+v", creds)
	}

	// 换新值。
	if err := store.UpdateCredential(ctx, &model.Credential{ID: cred.ID, ServerID: server.ID, Value: "v2"}); err != nil {
		t.Fatalf("UpdateCredential(值): %v", err)
	}
	creds, _ = store.ListCredentialsByServer(ctx, server.ID)
	if len(creds) != 1 || creds[0].Value != "v2" {
		t.Fatalf("更新值后回读不一致: %+v", creds)
	}

	// 删除。
	if err := store.DeleteCredential(ctx, cred.ID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if creds, _ := store.ListCredentialsByServer(ctx, server.ID); len(creds) != 0 {
		t.Fatalf("删除后应无凭证，得到 %d", len(creds))
	}
	if err := store.DeleteCredential(ctx, cred.ID); err == nil {
		t.Fatal("重复删除应报不存在错误")
	}
}

// TestDeleteServerCascadesCredentialsMemory 验证删除 Server 会级联清理凭证。
func TestDeleteServerCascadesCredentialsMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	server := &model.Server{Name: "S", Endpoint: "http://x", Enabled: true}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := store.CreateCredential(ctx, &model.Credential{ServerID: server.ID, Name: name, Kind: model.CredentialStaticToken}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteServer(ctx, server.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if creds, _ := store.ListCredentialsByServer(ctx, server.ID); len(creds) != 0 {
		t.Fatalf("删除 Server 后凭证应被级联清理，得到 %d", len(creds))
	}
}

// TestRecentTrafficByServerMemory 验证按 Server 过滤调用日志。
func TestRecentTrafficByServerMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, sid := range []string{"srv-1", "srv-2"} {
		for range 3 {
			sample := model.TrafficSample{ServerID: sid, Tool: "t", Status: "success", Timestamp: now}
			if err := store.AppendTraffic(ctx, sample); err != nil {
				t.Fatal(err)
			}
		}
	}
	logs, err := store.RecentTrafficByServer(ctx, "srv-1", 2)
	if err != nil {
		t.Fatalf("RecentTrafficByServer: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("应返回 2 条 srv-1 日志，得到 %d", len(logs))
	}
	for _, l := range logs {
		if l.ServerID != "srv-1" {
			t.Fatalf("混入其他 Server 日志: %+v", l)
		}
	}
}

// TestToolSetEnabledMemory 验证工具启停会同步 toolsByName 索引（路由解析依赖）。
func TestToolSetEnabledMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	tool := &model.Tool{ID: "t1", ServerID: "srv-1", OriginalName: "search", GatewayName: "mock.search", Enabled: true}
	if err := store.UpsertTool(ctx, tool); err != nil {
		t.Fatal(err)
	}
	if err := store.SetToolEnabled(ctx, "t1", false); err != nil {
		t.Fatalf("SetToolEnabled: %v", err)
	}
	byID, _ := store.GetTool(ctx, "t1")
	if byID.Enabled {
		t.Fatal("GetTool 应读到禁用")
	}
	byName, err := store.GetToolByGatewayName(ctx, "mock.search")
	if err != nil {
		t.Fatalf("GetToolByGatewayName: %v", err)
	}
	if byName.Enabled {
		t.Fatal("toolsByName 索引应同步为禁用")
	}
	// 幂等：同值再设一次不报错。
	if err := store.SetToolEnabled(ctx, "t1", false); err != nil {
		t.Fatalf("幂等设置失败: %v", err)
	}
	if err := store.SetToolEnabled(ctx, "missing", true); err == nil {
		t.Fatal("不存在的工具应报错")
	}
}

// TestRouteLifecycleMemory 验证路由 创建/读取/更新/删除。
func TestRouteLifecycleMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	server := &model.Server{Name: "S", Endpoint: "http://x", Enabled: true}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	route := &model.Route{Name: "主路由", ServerID: server.ID, ToolNames: []string{"a.search"}, Enabled: true}
	if err := store.CreateRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	if route.ID == "" || route.CreatedAt.IsZero() || route.UpdatedAt.IsZero() {
		t.Fatalf("CreateRoute 应盖章 id/时间: %+v", route)
	}
	created := route.CreatedAt

	got, err := store.GetRoute(ctx, route.ID)
	if err != nil || got.Name != "主路由" {
		t.Fatalf("GetRoute: %+v / %v", got, err)
	}

	if err := store.UpdateRoute(ctx, &model.Route{ID: route.ID, Name: "改名", ServerID: server.ID, ToolNames: []string{"b.search"}, Enabled: false}); err != nil {
		t.Fatalf("UpdateRoute: %v", err)
	}
	got, _ = store.GetRoute(ctx, route.ID)
	if got.Name != "改名" || got.Enabled || len(got.ToolNames) != 1 || got.ToolNames[0] != "b.search" {
		t.Fatalf("更新后回读不一致: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatal("UpdateRoute 不应改变 created_at")
	}

	if err := store.DeleteRoute(ctx, route.ID); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}
	if err := store.DeleteRoute(ctx, route.ID); err == nil {
		t.Fatal("重复删除应报错")
	}
	if _, err := store.GetRoute(ctx, route.ID); err == nil {
		t.Fatal("删除后 GetRoute 应报错")
	}
}

// TestPolicyLifecycleMemory 验证策略 创建/更新（替换规则）/删除。
func TestPolicyLifecycleMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	policy := &model.Policy{
		Name: "p", Enabled: true,
		Rules: []model.PolicyRule{{Subject: "agent-a", Tool: "a.search", Effect: model.PolicyEffectAllow}},
	}
	if err := store.CreatePolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if policy.ID == "" || policy.CreatedAt.IsZero() {
		t.Fatalf("CreatePolicy 应盖章: %+v", policy)
	}

	if err := store.UpdatePolicy(ctx, &model.Policy{
		ID: policy.ID, Name: "p2", Enabled: false,
		Rules: []model.PolicyRule{{Subject: "agent-b", Tool: "b.detail", Effect: model.PolicyEffectDeny}},
	}); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
	got, err := store.GetPolicy(ctx, policy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "p2" || got.Enabled || len(got.Rules) != 1 || got.Rules[0].Effect != model.PolicyEffectDeny {
		t.Fatalf("更新后回读不一致: %+v", got)
	}

	if err := store.DeletePolicy(ctx, policy.ID); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	if _, err := store.GetPolicy(ctx, policy.ID); err == nil {
		t.Fatal("删除后 GetPolicy 应报错")
	}
}

// TestDeleteServerCascadesRoutesMemory 验证删除 Server 会级联清理其路由。
func TestDeleteServerCascadesRoutesMemory(t *testing.T) {
	store := New()
	ctx := context.Background()
	server := &model.Server{Name: "S", Endpoint: "http://x", Enabled: true}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"r1", "r2"} {
		if err := store.CreateRoute(ctx, &model.Route{Name: name, ServerID: server.ID, ToolNames: []string{"a.search"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteServer(ctx, server.ID); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if routes, _ := store.ListRoutes(ctx); len(routes) != 0 {
		t.Fatalf("删除 Server 后路由应被级联清理，得到 %d", len(routes))
	}
}

func TestToolUpsertKeepsID(t *testing.T) {
	store := New()
	ctx := context.Background()

	server := &model.Server{Name: "U", Endpoint: "http://x", Enabled: true}
	_ = store.CreateServer(ctx, server)

	tool := &model.Tool{ServerID: server.ID, OriginalName: "search", GatewayName: "u.search", Enabled: true}
	if err := store.UpsertTool(ctx, tool); err != nil {
		t.Fatal(err)
	}
	id := tool.ID

	tool.Description = "更新"
	tool.ID = ""
	if err := store.UpsertTool(ctx, tool); err != nil {
		t.Fatal(err)
	}
	if tool.ID != id {
		t.Fatalf("Upsert 应保留原 id，得到 %q != %q", tool.ID, id)
	}
}

func TestAccessKeyCRUDAndIndexes(t *testing.T) {
	store := New()
	ctx := context.Background()

	key := &model.AccessKey{
		Name: "Partner A", Subject: "partner-a", Enabled: true, QPS: 2, Burst: 2,
		KeyHash: "hash-1",
		Grants:  []model.ToolGrant{{GatewayName: "mock.search", Headers: map[string]string{"X-T": "v"}}},
	}
	if err := store.CreateAccessKey(ctx, key); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// byHash 命中，并可读回 grants。
	got, err := store.GetAccessKeyByKeyHash(ctx, "hash-1")
	if err != nil || got.Subject != "partner-a" || len(got.Grants) != 1 {
		t.Fatalf("byHash: %v / %+v", err, got)
	}
	if got, err := store.GetAccessKeyBySubject(ctx, "partner-a"); err != nil || got.QPS != 2 {
		t.Fatalf("bySubject: %v / %+v", err, got)
	}

	// subject 唯一约束。
	dup := &model.AccessKey{Subject: "partner-a", KeyHash: "hash-x"}
	if err := store.CreateAccessKey(ctx, dup); err == nil {
		t.Fatal("重复 subject 应拒绝")
	}

	// List 稳定返回。
	keys, err := store.ListAccessKeys(ctx)
	if err != nil || len(keys) != 1 {
		t.Fatalf("List: %v / %d", err, len(keys))
	}

	// 更新（含切换 KeyHash 与 Subject）后索引同步。
	updated := *got
	updated.Enabled = false
	updated.Subject = "partner-b"
	updated.KeyHash = "hash-2"
	if err := store.UpdateAccessKey(ctx, &updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := store.GetAccessKeyByKeyHash(ctx, "hash-1"); err == nil {
		t.Fatal("旧哈希索引应失效")
	}
	if _, err := store.GetAccessKeyByKeyHash(ctx, "hash-2"); err != nil {
		t.Fatalf("新哈希应可查: %v", err)
	}
	if _, err := store.GetAccessKeyBySubject(ctx, "partner-a"); err == nil {
		t.Fatal("旧 subject 索引应失效")
	}

	// 删除清理索引。
	if err := store.DeleteAccessKey(ctx, updated.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.GetAccessKey(ctx, updated.ID); err == nil {
		t.Fatal("删除后应不可查")
	}
}

func TestAccessKeySecretNotPersisted(t *testing.T) {
	store := New()
	ctx := context.Background()

	key := &model.AccessKey{
		Name: "p", Subject: "p", Enabled: true, KeyHash: "h1",
		Secret: "plaintext-create-only",
	}
	if err := store.CreateAccessKey(ctx, key); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 存储副本不得保留明文（与 MySQL 不落 secret 一致），防止回读。
	byID, err := store.GetAccessKey(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAccessKey: %v", err)
	}
	if byID.Secret != "" {
		t.Fatalf("存储的 AccessKey 不应含明文 Secret: %+v", byID)
	}
	byHash, err := store.GetAccessKeyByKeyHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetAccessKeyByKeyHash: %v", err)
	}
	if byHash.Secret != "" {
		t.Fatalf("keysByHash 索引不应含明文 Secret: %+v", byHash)
	}
	bySubj, err := store.GetAccessKeyBySubject(ctx, "p")
	if err != nil {
		t.Fatalf("GetAccessKeyBySubject: %v", err)
	}
	if bySubj.Secret != "" {
		t.Fatalf("keysBySubj 索引不应含明文 Secret: %+v", bySubj)
	}
	// 调用方本地对象仍保有明文，供创建响应一次性下发。
	if key.Secret != "plaintext-create-only" {
		t.Fatalf("不应污染调用方对象，得到 %q", key.Secret)
	}

	// 更新路径同样清理。
	upd := *byID
	upd.Enabled = false
	upd.Secret = "should-not-store"
	if err := store.UpdateAccessKey(ctx, &upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	again, err := store.GetAccessKey(ctx, key.ID)
	if err != nil || again.Secret != "" {
		t.Fatalf("更新后仍不应含明文 Secret: %v / %+v", err, again)
	}
}
