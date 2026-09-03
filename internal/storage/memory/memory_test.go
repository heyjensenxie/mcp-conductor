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
