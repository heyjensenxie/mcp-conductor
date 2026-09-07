// MySQL 5.7 集成：管理面列表 Query*（分页 + 筛选）。通过 MYSQL_TEST_DSN 启用，
// 未配置自动跳过（与 mysql_test.go 一致）。
//
// 数据隔离：每用例用唯一标记（qt<纳秒>）限定断言范围，避免与库内既有行互相
// 干扰；能清理的都随用例删除（server 级联工具/凭证、access key 单独删除），
// traffic 为追加表无删除，用唯一 request_id 标记圈定。
package mysql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

func marker(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("qt%d", time.Now().UnixNano())
}

// boolPtr 三态布尔筛选测试用辅助。
func boolPtr(b bool) *bool { return &b }

// TestQueryServersFilterAndPage 覆盖关键词/enabled/health_status 与分页。
func TestQueryServersFilterAndPage(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	m := marker(t)

	names := []string{m + "-Alpha", m + "-beta", m + "-Gamma"}
	created := make([]*model.Server, 0, 3)
	for i, name := range names {
		srv := testServer(name)
		if i == 1 {
			srv.Enabled = false
			srv.HealthStatus = model.ServerStatusUnhealthy
		} else {
			srv.Enabled = true
			srv.HealthStatus = model.ServerStatusHealthy
		}
		if err := store.CreateServer(ctx, srv); err != nil {
			t.Fatalf("CreateServer: %v", err)
		}
		created = append(created, srv)
	}
	defer func() {
		for _, s := range created {
			_ = store.DeleteServer(ctx, s.ID)
		}
	}()

	// 关键词圈定本用例 3 条。
	got, total, err := store.QueryServers(ctx, query.ServerQuery{Q: m, Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 3 || total != 3 {
		t.Fatalf("q 全量异常: items=%d total=%d err=%v", len(got), total, err)
	}

	// enabled=false 只命中 beta。
	got, total, _ = store.QueryServers(ctx, query.ServerQuery{Q: m, Enabled: boolPtr(false)})
	if len(got) != 1 || got[0].Name != names[1] || total != 1 {
		t.Fatalf("enabled=false 异常: items=%d total=%d", len(got), total)
	}

	// health_status 过滤。
	got, total, _ = store.QueryServers(ctx, query.ServerQuery{Q: m, HealthStatus: string(model.ServerStatusUnhealthy)})
	if len(got) != 1 || total != 1 {
		t.Fatalf("health_status 过滤异常: %d / %d", len(got), total)
	}

	// 分页：page_size=2 第 1 页 2 条、第 2 页 1 条，total 保持 3。
	got, total, _ = store.QueryServers(ctx, query.ServerQuery{Q: m, Paging: query.Paging{Page: 1, PageSize: 2}})
	if len(got) != 2 || total != 3 {
		t.Fatalf("page1 异常: items=%d total=%d", len(got), total)
	}
	got, total, _ = store.QueryServers(ctx, query.ServerQuery{Q: m, Paging: query.Paging{Page: 2, PageSize: 2}})
	if len(got) != 1 || total != 3 {
		t.Fatalf("page2 异常: items=%d total=%d", len(got), total)
	}
}

// TestQueryToolsByServer 覆盖单 Server 工具分页与关键词。
func TestQueryToolsByServer(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	m := marker(t)

	srv := testServer(m)
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	defer func() { _ = store.DeleteServer(ctx, srv.ID) }()

	tools := []model.Tool{
		{ServerID: srv.ID, OriginalName: m + "search", GatewayName: "gamma." + m, Enabled: true},
		{ServerID: srv.ID, OriginalName: m + "detail", GatewayName: "alpha." + m, Enabled: true},
		{ServerID: srv.ID, OriginalName: m + "list", GatewayName: "beta." + m, Enabled: false},
	}
	for i := range tools {
		if err := store.UpsertTool(ctx, &tools[i]); err != nil {
			t.Fatalf("UpsertTool: %v", err)
		}
	}

	got, total, err := store.QueryTools(ctx, query.ToolQuery{ServerID: srv.ID, Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 3 || total != 3 {
		t.Fatalf("server 工具全量异常: %d / %d err=%v", len(got), total, err)
	}
	// gateway_name 升序（alpha/beta/gamma）。
	if got[0].GatewayName != "alpha."+m || got[2].GatewayName != "gamma."+m {
		t.Fatalf("排序异常: %s %s %s", got[0].GatewayName, got[1].GatewayName, got[2].GatewayName)
	}

	got, total, _ = store.QueryTools(ctx, query.ToolQuery{ServerID: srv.ID, Q: m + "search"})
	if len(got) != 1 || total != 1 {
		t.Fatalf("q 过滤异常: %d / %d", len(got), total)
	}
	got, total, _ = store.QueryTools(ctx, query.ToolQuery{ServerID: srv.ID, Enabled: boolPtr(false)})
	if len(got) != 1 || total != 1 {
		t.Fatalf("enabled 过滤异常: %d / %d", len(got), total)
	}
}

// TestQueryAccessKeysWithGrants 覆盖分页 + grants 二次查询。
func TestQueryAccessKeysWithGrants(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	m := marker(t)

	keys := []model.AccessKey{
		{Name: m + "-partner-a", Subject: m + "-sub-a", Enabled: true, Grants: []model.ToolGrant{{GatewayName: "svc.prod", Headers: map[string]string{"X": "1"}}}},
		{Name: m + "-partner-b", Subject: m + "-sub-b", Enabled: false},
	}
	created := make([]*model.AccessKey, 0, len(keys))
	for i := range keys {
		if err := store.CreateAccessKey(ctx, &keys[i]); err != nil {
			t.Fatalf("CreateAccessKey: %v", err)
		}
		created = append(created, &keys[i])
	}
	defer func() {
		for _, k := range created {
			_ = store.DeleteAccessKey(ctx, k.ID)
		}
	}()

	got, total, err := store.QueryAccessKeys(ctx, query.AccessKeyQuery{Q: m, Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 2 || total != 2 {
		t.Fatalf("keys 全量异常: %d / %d err=%v", len(got), total, err)
	}
	var withGrants *model.AccessKey
	for i := range got {
		if got[i].Subject == m+"-sub-a" {
			withGrants = &got[i]
		}
	}
	if withGrants == nil || len(withGrants.Grants) != 1 || withGrants.Grants[0].GatewayName != "svc.prod" {
		t.Fatalf("grants 未随 key 返回: %+v", withGrants)
	}

	// 分页 + enabled 组合。
	got, total, _ = store.QueryAccessKeys(ctx, query.AccessKeyQuery{Q: m, Enabled: boolPtr(false)})
	if len(got) != 1 || total != 1 {
		t.Fatalf("enabled 过滤异常: %d / %d", len(got), total)
	}
}

// TestQueryCredentialsMetadata 覆盖凭证列表只回元数据（不返回明文 Value）。
func TestQueryCredentialsMetadata(t *testing.T) {
	store := openTestKeyed(t)
	ctx := context.Background()
	m := marker(t)

	srv := testServer(m)
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	defer func() { _ = store.DeleteServer(ctx, srv.ID) }()

	cred := &model.Credential{ServerID: srv.ID, Name: m + "-api", Kind: model.CredentialAPIKey, Header: "X-Key", Value: "plain-secret"}
	if err := store.CreateCredential(ctx, cred); err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}
	if !cred.HasValue {
		t.Fatal("写入值后 HasValue 应为 true")
	}

	got, total, err := store.QueryCredentials(ctx, query.CredentialQuery{ServerID: srv.ID, Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 1 || total != 1 {
		t.Fatalf("credentials 查询异常: %d / %d err=%v", len(got), total, err)
	}
	if got[0].Value != "" {
		t.Fatalf("QueryCredentials 不得解密下发 Value: %+v", got[0])
	}
	if !got[0].HasValue || got[0].Kind != model.CredentialAPIKey {
		t.Fatalf("凭证元数据回读不一致: %+v", got[0])
	}

	got, total, _ = store.QueryCredentials(ctx, query.CredentialQuery{ServerID: srv.ID, Kind: string(model.CredentialStaticToken)})
	if len(got) != 0 || total != 0 {
		t.Fatalf("kind 过滤异常: %d / %d", len(got), total)
	}
}

// TestQueryTrafficFilterAndPage 覆盖 server/status/时间区间与分页（追加表，按唯一
// request_id 圈定，不做删除）。
func TestQueryTrafficFilterAndPage(t *testing.T) {
	store := openTest(t)
	ctx := context.Background()
	m := marker(t)
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)

	samples := []model.TrafficSample{
		{RequestID: m + "-1", ServerID: "s1", InstanceID: "i1", Tool: "alpha.search", Client: "c1", ClientIP: "203.0.113.9", Status: "success", Timestamp: model.T(base)},
		{RequestID: m + "-2", ServerID: "s2", InstanceID: "i2", Tool: "beta.search", Client: "c2", Status: "upstream_error", Timestamp: model.T(base.Add(30 * time.Second))},
		{RequestID: m + "-3", ServerID: "s1", InstanceID: "i1", Tool: "alpha.search", Client: "c1", ClientIP: "203.0.113.9", Status: "success", Timestamp: model.T(base.Add(60 * time.Second))},
	}
	for _, sample := range samples {
		if err := store.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	got, total, err := store.QueryTraffic(ctx, query.TrafficQuery{Q: m, Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 3 || total != 3 {
		t.Fatalf("q 全量异常: %d / %d err=%v", len(got), total, err)
	}
	if got[0].RequestID != m+"-3" {
		t.Fatalf("应按 id 倒序（最新在前），得到 %s", got[0].RequestID)
	}
	if got[0].InstanceID != "i1" || got[1].InstanceID != "i2" {
		t.Fatalf("instance_id 应随行往返: %+v", got)
	}
	if got[0].ClientIP != "203.0.113.9" || got[1].ClientIP != "" {
		t.Fatalf("client_ip 应随行往返（空行为空串）: %+v", got)
	}

	got, total, _ = store.QueryTraffic(ctx, query.TrafficQuery{Q: m, ServerID: "s1", Status: "success"})
	if len(got) != 2 || total != 2 {
		t.Fatalf("server+status 过滤异常: %d / %d", len(got), total)
	}

	// server + instance 组合等值过滤。
	got, total, _ = store.QueryTraffic(ctx, query.TrafficQuery{Q: m, ServerID: "s1", InstanceID: "i1"})
	if len(got) != 2 || total != 2 {
		t.Fatalf("server+instance 过滤异常: %d / %d", len(got), total)
	}

	// 时间闭区间（含边界）：命中 30s~60s 两条。
	from := base.Add(30 * time.Second)
	to := base.Add(60 * time.Second)
	got, total, _ = store.QueryTraffic(ctx, query.TrafficQuery{Q: m, From: from, To: to})
	if len(got) != 2 || total != 2 {
		t.Fatalf("时间区间过滤异常: %d / %d", len(got), total)
	}

	got, total, _ = store.QueryTraffic(ctx, query.TrafficQuery{Q: m, Paging: query.Paging{Page: 1, PageSize: 2}})
	if len(got) != 2 || total != 3 {
		t.Fatalf("分页异常: %d / %d", len(got), total)
	}
}
