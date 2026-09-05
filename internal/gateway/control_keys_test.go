package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/storage/memory"
)

// env 是控制面信封响应，仅用于测试解码。
type env struct {
	Code string          `json:"code"`
	Data json.RawMessage `json:"data"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) env {
	t.Helper()
	var e env
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析信封失败: %v", err)
	}
	if e.Code != "ok" {
		t.Fatalf("非成功响应 %q: %s", e.Code, rec.Body.String())
	}
	return e
}

// TestKeySecretReturnedOnlyOnCreate 锁定"明文仅创建时返回一次"：
// create 响应含 secret；list/get/update 响应绝不携带 secret 与 key_hash。
func TestKeySecretReturnedOnlyOnCreate(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, nil, func(token string) (string, error) { return "hash-" + token, nil })

	createBody := `{"name":"Partner A","subject":"partner-a","qps":2,"burst":2,
		"grants":[{"gateway_name":"svc1.prod","headers":{"X-Env":"prod"}}]}`
	rec := httptest.NewRecorder()
	ctrl.handleCreateKey(rec, httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(createBody)))

	created := decodeEnvelope(t, rec)
	var createdKey struct {
		ID      string `json:"id"`
		Subject string `json:"subject"`
		Secret  string `json:"secret"`
	}
	if err := json.Unmarshal(created.Data, &createdKey); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	if createdKey.ID == "" || createdKey.Subject != "partner-a" {
		t.Fatalf("创建响应缺少主体字段: %+v", createdKey)
	}
	if createdKey.Secret == "" {
		t.Fatal("创建响应必须一次性下发明文 secret")
	}
	secret := createdKey.Secret
	if !strings.Contains(string(created.Data), secret) {
		t.Fatal("创建响应应包含明文 secret")
	}

	// list 不得回读 secret。
	rec = httptest.NewRecorder()
	ctrl.handleListKeys(rec, httptest.NewRequest(http.MethodGet, "/api/keys", nil))
	listRaw := decodeEnvelope(t, rec)
	if strings.Contains(string(listRaw.Data), secret) {
		t.Fatalf("list 不得回读明文 secret: %s", listRaw.Data)
	}
	var list []model.AccessKey
	if err := json.Unmarshal(listRaw.Data, &list); err != nil || len(list) != 1 {
		t.Fatalf("解析 list 失败: %v / %d", err, len(list))
	}

	// get 单个同样不得回读。
	getReq := httptest.NewRequest(http.MethodGet, "/api/keys/"+createdKey.ID, nil)
	getReq.SetPathValue("id", createdKey.ID)
	rec = httptest.NewRecorder()
	ctrl.handleGetKey(rec, getReq)
	getRaw := decodeEnvelope(t, rec)
	if strings.Contains(string(getRaw.Data), secret) {
		t.Fatalf("get 不得回读明文 secret: %s", getRaw.Data)
	}

	// update 响应同样不得回读。
	updReq := httptest.NewRequest(http.MethodPatch, "/api/keys/"+createdKey.ID, strings.NewReader(`{"enabled":false}`))
	updReq.SetPathValue("id", createdKey.ID)
	rec = httptest.NewRecorder()
	ctrl.handleUpdateKey(rec, updReq)
	updRaw := decodeEnvelope(t, rec)
	if strings.Contains(string(updRaw.Data), secret) {
		t.Fatalf("update 不得回读明文 secret: %s", updRaw.Data)
	}

	for _, raw := range []json.RawMessage{listRaw.Data, getRaw.Data, updRaw.Data} {
		s := string(raw)
		if strings.Contains(s, `"secret"`) || strings.Contains(s, "key_hash") {
			t.Fatalf("任何回读响应都不得携带 secret/key_hash 字段: %s", s)
		}
	}
}

// TestKeySecretRotateReturnsOnce 锁定"轮换密钥"契约：新明文仅本响应下发一次，
// 旧哈希立即失效、新哈希可命中，后续 get 不再携带新明文。
func TestKeySecretRotateReturnsOnce(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, nil, func(token string) (string, error) { return "hash-" + token, nil })

	rec := httptest.NewRecorder()
	ctrl.handleCreateKey(rec, httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"Partner A","subject":"partner-a"}`)))
	created := decodeEnvelope(t, rec)
	var createdKey struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(created.Data, &createdKey); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	oldSecret := createdKey.Secret

	rotReq := httptest.NewRequest(http.MethodPost, "/api/keys/"+createdKey.ID+"/rotate", nil)
	rotReq.SetPathValue("id", createdKey.ID)
	rec = httptest.NewRecorder()
	ctrl.handleRotateKeySecret(rec, rotReq)
	rotated := decodeEnvelope(t, rec)
	var rotatedKey struct {
		ID      string `json:"id"`
		Subject string `json:"subject"`
		Secret  string `json:"secret"`
	}
	if err := json.Unmarshal(rotated.Data, &rotatedKey); err != nil {
		t.Fatalf("解析轮换响应失败: %v", err)
	}
	if rotatedKey.ID != createdKey.ID || rotatedKey.Subject != "partner-a" {
		t.Fatalf("轮换应返回同一 key 的元数据: %+v", rotatedKey)
	}
	if rotatedKey.Secret == "" || rotatedKey.Secret == oldSecret {
		t.Fatalf("轮换必须下发新的明文 secret: old=%q new=%q", oldSecret, rotatedKey.Secret)
	}
	newSecret := rotatedKey.Secret

	// 旧密钥哈希立即失效，新哈希可命中。
	ctx := context.Background()
	if _, err := store.GetAccessKeyByKeyHash(ctx, "hash-"+oldSecret); err == nil {
		t.Fatal("旧密钥哈希在轮换后应失效")
	}
	if got, err := store.GetAccessKeyByKeyHash(ctx, "hash-"+newSecret); err != nil || got.ID != createdKey.ID {
		t.Fatalf("新密钥哈希应能定位到同一 key: %v / %+v", err, got)
	}

	// 后续 get 不得回读新的明文 secret。
	getReq := httptest.NewRequest(http.MethodGet, "/api/keys/"+createdKey.ID, nil)
	getReq.SetPathValue("id", createdKey.ID)
	rec = httptest.NewRecorder()
	ctrl.handleGetKey(rec, getReq)
	getRaw := decodeEnvelope(t, rec)
	if strings.Contains(string(getRaw.Data), newSecret) {
		t.Fatalf("轮换后 get 不得回读明文 secret: %s", getRaw.Data)
	}
}
