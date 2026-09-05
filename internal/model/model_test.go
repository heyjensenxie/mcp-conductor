package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAccessKeyMarshalNeverLeaksSecret 锁定"密钥不回读"序列化契约：
// AccessKey 的 Secret 与 KeyHash 均为 json:"-"，任何 list/get/update 响应
// 都不可能把它们输出。明文只在创建时经专用响应体显式返回一次。
func TestAccessKeyMarshalNeverLeaksSecret(t *testing.T) {
	k := AccessKey{
		ID:      "key-1",
		Name:    "partner-a",
		Subject: "partner-a",
		Enabled: true,
		QPS:     5,
		Burst:   10,
		Grants:  []ToolGrant{{GatewayName: "svc1.prod"}},
		KeyHash: "sha-hash-should-not-appear",
		Secret:  "plaintext-secret-should-not-appear",
	}
	raw, err := json.Marshal(k)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(raw)
	for _, leaked := range []string{"plaintext-secret-should-not-appear", "sha-hash-should-not-appear", "key_hash", `"secret"`} {
		if strings.Contains(s, leaked) {
			t.Fatalf("响应不得包含 %q，得到: %s", leaked, s)
		}
	}
	for _, want := range []string{`"id":"key-1"`, `"subject":"partner-a"`, `"grants"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("应包含 %q，得到: %s", want, s)
		}
	}
}
