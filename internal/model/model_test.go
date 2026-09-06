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

// instanceOf 构造一条用于聚合推导的实例（默认启用、状态可指定）。
func instanceOf(status ServerStatus, enabled ...bool) Instance {
	i := Instance{Enabled: true, HealthStatus: status}
	if len(enabled) > 0 {
		i.Enabled = enabled[0]
	}
	return i
}

// TestAggregateServerHealth 表驱动覆盖 Server 级聚合健康的全部推导分支：
// 禁用→disabled；任一启用 healthy→healthy；只有 unknown→unknown；
// 全 unhealthy→unhealthy；无启用实例→unhealthy；unknown+unhealthy（无 healthy）→unknown。
func TestAggregateServerHealth(t *testing.T) {
	cases := []struct {
		name      string
		enabled   bool
		instances []Instance
		want      ServerStatus
	}{
		{name: "server disabled wins even with healthy instances", enabled: false, instances: []Instance{instanceOf(ServerStatusHealthy)}, want: ServerStatusDisabled},
		{name: "any enabled healthy instance aggregates healthy", enabled: true, instances: []Instance{instanceOf(ServerStatusUnhealthy), instanceOf(ServerStatusHealthy)}, want: ServerStatusHealthy},
		{name: "healthy wins over unknown", enabled: true, instances: []Instance{instanceOf(ServerStatusUnknown), instanceOf(ServerStatusHealthy)}, want: ServerStatusHealthy},
		{name: "disabled instances are ignored", enabled: true, instances: []Instance{instanceOf(ServerStatusHealthy, false), instanceOf(ServerStatusUnknown, false)}, want: ServerStatusUnhealthy},
		{name: "only unknown enabled instance aggregates unknown", enabled: true, instances: []Instance{instanceOf(ServerStatusUnknown)}, want: ServerStatusUnknown},
		{name: "unknown plus unhealthy aggregates unknown", enabled: true, instances: []Instance{instanceOf(ServerStatusUnknown), instanceOf(ServerStatusUnhealthy)}, want: ServerStatusUnknown},
		{name: "all unhealthy aggregates unhealthy", enabled: true, instances: []Instance{instanceOf(ServerStatusUnhealthy), instanceOf(ServerStatusUnhealthy)}, want: ServerStatusUnhealthy},
		{name: "enabled server with no enabled instance aggregates unhealthy", enabled: true, instances: []Instance{instanceOf(ServerStatusHealthy, false)}, want: ServerStatusUnhealthy},
		{name: "enabled server with no instances aggregates unhealthy", enabled: true, instances: nil, want: ServerStatusUnhealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AggregateServerHealth(tc.enabled, tc.instances); got != tc.want {
				t.Fatalf("AggregateServerHealth(enabled=%v, instances=%v) = %q, want %q", tc.enabled, tc.instances, got, tc.want)
			}
		})
	}
}
