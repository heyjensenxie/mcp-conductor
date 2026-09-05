package memory

import (
	"context"
	"testing"

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
