package memory

import (
	"context"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

func boolPtr(b bool) *bool { return &b }

// seedQueryServers 造 3 个 Server（srv-1..3 按 id 升序）。
func seedQueryServers(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	servers := []model.Server{
		{Name: "Alpha", Enabled: true, HealthStatus: model.ServerStatusHealthy},
		{Name: "beta-search", Enabled: false, HealthStatus: model.ServerStatusUnhealthy},
		{Name: "Gamma", Enabled: true, HealthStatus: model.ServerStatusDisabled},
	}
	for _, s := range servers {
		if err := st.CreateServer(ctx, &s); err != nil {
			t.Fatalf("CreateServer: %v", err)
		}
	}
}

func TestQueryServers_filterAndPage(t *testing.T) {
	st := New()
	seedQueryServers(t, st)
	ctx := context.Background()

	// 关键词（大小写不敏感）+ enabled + health_status 组合。
	got, total, err := st.QueryServers(ctx, query.ServerQuery{Q: "beta", Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 1 || got[0].Name != "beta-search" || total != 1 {
		t.Fatalf("q 过滤异常: items=%d total=%d err=%v", len(got), total, err)
	}

	got, total, err = st.QueryServers(ctx, query.ServerQuery{Enabled: boolPtr(true), Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 2 || total != 2 {
		t.Fatalf("enabled 过滤异常: items=%d total=%d err=%v", len(got), total, err)
	}

	got, _, err = st.QueryServers(ctx, query.ServerQuery{HealthStatus: string(model.ServerStatusUnhealthy)})
	if err != nil || len(got) != 1 || got[0].Name != "beta-search" {
		t.Fatalf("health_status 过滤异常: %d err=%v", len(got), err)
	}

	// 分页：每页 2，第 1 页应只含 id 升序前两条，total=3。
	got, total, err = st.QueryServers(ctx, query.ServerQuery{Paging: query.Paging{Page: 1, PageSize: 2}})
	if err != nil || len(got) != 2 || got[0].ID != "srv-1" || got[1].ID != "srv-2" || total != 3 {
		t.Fatalf("page1 异常: items=%d first=%q total=%d err=%v", len(got), got[0].ID, total, err)
	}
	got, _, err = st.QueryServers(ctx, query.ServerQuery{Paging: query.Paging{Page: 2, PageSize: 2}})
	if err != nil || len(got) != 1 || got[0].ID != "srv-3" {
		t.Fatalf("page2 异常: items=%d err=%v", len(got), err)
	}

	// 全量模式 page_size<=0 返回全部匹配且 total 一致。
	got, total, err = st.QueryServers(ctx, query.ServerQuery{Paging: query.Paging{PageSize: 0}})
	if err != nil || len(got) != 3 || total != 3 {
		t.Fatalf("all-mode 异常: items=%d total=%d err=%v", len(got), total, err)
	}

	// 越界页返回空 items、total 不变。
	got, total, _ = st.QueryServers(ctx, query.ServerQuery{Paging: query.Paging{Page: 9, PageSize: 2}})
	if len(got) != 0 || total != 3 {
		t.Fatalf("越界页异常: items=%d total=%d", len(got), total)
	}
}

// seedQueryTools 造 3 个工具（按 gateway_name 升序：alpha.search、beta.detail、gamma.list）。
func seedQueryTools(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	tools := []model.Tool{
		{ServerID: "s1", OriginalName: "list", GatewayName: "gamma.list", Description: "列出", Enabled: true},
		{ServerID: "s1", OriginalName: "search", GatewayName: "alpha.search", Enabled: true},
		{ServerID: "s2", OriginalName: "detail", GatewayName: "beta.detail", Enabled: false},
	}
	for _, tool := range tools {
		if err := st.UpsertTool(ctx, &tool); err != nil {
			t.Fatalf("UpsertTool: %v", err)
		}
	}
}

func TestQueryTools_filterAndPage(t *testing.T) {
	st := New()
	seedQueryTools(t, st)
	ctx := context.Background()

	got, total, err := st.QueryTools(ctx, query.ToolQuery{Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 3 || total != 3 {
		t.Fatalf("全量异常: items=%d total=%d err=%v", len(got), total, err)
	}
	// 确定性排序：gateway_name 升序。
	if got[0].GatewayName != "alpha.search" || got[1].GatewayName != "beta.detail" || got[2].GatewayName != "gamma.list" {
		t.Fatalf("排序异常: %+v", []string{got[0].GatewayName, got[1].GatewayName, got[2].GatewayName})
	}

	// 关键词命中 gateway_name 或 original_name。
	got, total, _ = st.QueryTools(ctx, query.ToolQuery{Q: "search"})
	if len(got) != 1 || got[0].GatewayName != "alpha.search" || total != 1 {
		t.Fatalf("q 过滤异常: %d", len(got))
	}

	// server_id + enabled 组合。
	got, total, _ = st.QueryTools(ctx, query.ToolQuery{ServerID: "s1", Enabled: boolPtr(true)})
	if len(got) != 2 || total != 2 {
		t.Fatalf("server+enabled 过滤异常: items=%d total=%d", len(got), total)
	}

	// 分页。
	got, total, _ = st.QueryTools(ctx, query.ToolQuery{Paging: query.Paging{Page: 1, PageSize: 2}})
	if len(got) != 2 || got[0].GatewayName != "alpha.search" || got[1].GatewayName != "beta.detail" || total != 3 {
		t.Fatalf("tool 分页异常: %d / total=%d", len(got), total)
	}
}

func TestQueryRoutes_filter(t *testing.T) {
	st := New()
	ctx := context.Background()
	routes := []model.Route{
		{Name: "主路由", ServerID: "s1", ToolNames: []string{"a.search"}, Enabled: true},
		{Name: "备路由", ServerID: "s2", ToolNames: []string{"b.search"}, Enabled: false},
	}
	for _, rt := range routes {
		if err := st.CreateRoute(ctx, &rt); err != nil {
			t.Fatalf("CreateRoute: %v", err)
		}
	}
	got, total, err := st.QueryRoutes(ctx, query.RouteQuery{Q: "路由", Enabled: boolPtr(true)})
	if err != nil || len(got) != 1 || got[0].Name != "主路由" || total != 1 {
		t.Fatalf("route 筛选异常: %d / total=%d err=%v", len(got), total, err)
	}
	got, total, _ = st.QueryRoutes(ctx, query.RouteQuery{ServerID: "s2"})
	if len(got) != 1 || total != 1 || got[0].ToolNames[0] != "b.search" {
		t.Fatalf("route server 过滤异常: %d / %+v", len(got), got)
	}
}

func TestQueryCredentials_metadataAndFilter(t *testing.T) {
	st := New()
	ctx := context.Background()
	creds := []model.Credential{
		{ServerID: "s1", Name: "密钥A", Kind: model.CredentialAPIKey, Header: "X-A", Value: "secret-a"},
		{ServerID: "s1", Name: "令牌B", Kind: model.CredentialStaticToken, Value: "token-b"},
	}
	for _, cred := range creds {
		if err := st.CreateCredential(ctx, &cred); err != nil {
			t.Fatalf("CreateCredential: %v", err)
		}
	}
	got, total, err := st.QueryCredentials(ctx, query.CredentialQuery{ServerID: "s1", Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 2 || total != 2 {
		t.Fatalf("credential 全量异常: %d / total=%d err=%v", len(got), total, err)
	}
	for _, cred := range got {
		if cred.Value != "" {
			t.Fatalf("QueryCredentials 不得下发内部 Value: %+v", cred)
		}
	}

	got, total, _ = st.QueryCredentials(ctx, query.CredentialQuery{ServerID: "s1", Kind: string(model.CredentialAPIKey)})
	if len(got) != 1 || got[0].Name != "密钥A" || total != 1 {
		t.Fatalf("kind 过滤异常: %d", len(got))
	}
	got, total, _ = st.QueryCredentials(ctx, query.CredentialQuery{ServerID: "s1", HasValue: boolPtr(false)})
	if len(got) != 0 || total != 0 {
		t.Fatalf("has_value=false 过滤异常: %d", len(got))
	}
	// 跨 Server 隔离。
	got, _, _ = st.QueryCredentials(ctx, query.CredentialQuery{ServerID: "other"})
	if len(got) != 0 {
		t.Fatalf("应隔离到指定 Server: %d", len(got))
	}
}

func TestQueryAccessKeys_filterAndGrants(t *testing.T) {
	st := New()
	ctx := context.Background()
	keys := []model.AccessKey{
		{Name: "Partner A", Subject: "partner-a", Enabled: true, Grants: []model.ToolGrant{{GatewayName: "svc.prod"}}},
		{Name: "Partner B", Subject: "partner-b", Enabled: false},
	}
	for _, key := range keys {
		if err := st.CreateAccessKey(ctx, &key); err != nil {
			t.Fatalf("CreateAccessKey: %v", err)
		}
	}
	got, total, err := st.QueryAccessKeys(ctx, query.AccessKeyQuery{Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 2 || total != 2 {
		t.Fatalf("access key 全量异常: %d / total=%d err=%v", len(got), total, err)
	}
	// grants 随 key 返回。
	if len(got[0].Grants) != 1 || got[0].Grants[0].GatewayName != "svc.prod" {
		t.Fatalf("grants 未随 key 返回: %+v", got[0].Grants)
	}
	got, total, _ = st.QueryAccessKeys(ctx, query.AccessKeyQuery{Q: "partner-b"})
	if len(got) != 1 || got[0].Name != "Partner B" || total != 1 {
		t.Fatalf("q 过滤异常: %d", len(got))
	}
	got, total, _ = st.QueryAccessKeys(ctx, query.AccessKeyQuery{Enabled: boolPtr(true)})
	if len(got) != 1 || got[0].Subject != "partner-a" || total != 1 {
		t.Fatalf("enabled 过滤异常: %d", len(got))
	}
}

func TestQueryTraffic_filterTimeAndPage(t *testing.T) {
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	samples := []model.TrafficSample{
		{RequestID: "req-1", ServerID: "s1", InstanceID: "i1", Tool: "alpha.search", Client: "c1", Status: "success", Timestamp: model.T(base.Add(1 * time.Minute))},
		{RequestID: "req-2", ServerID: "s2", InstanceID: "i2", Tool: "beta.search", Client: "c2", Status: "upstream_error", Timestamp: model.T(base.Add(2 * time.Minute))},
		{RequestID: "req-3", ServerID: "s1", InstanceID: "i1", Tool: "alpha.search", Client: "c1", Status: "success", Timestamp: model.T(base.Add(3 * time.Minute))},
	}
	for _, sample := range samples {
		if err := st.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	got, total, err := st.QueryTraffic(ctx, query.TrafficQuery{Paging: query.Paging{PageSize: 10}})
	if err != nil || len(got) != 3 || total != 3 {
		t.Fatalf("全量异常: %d / total=%d err=%v", len(got), total, err)
	}
	// 写入倒序：最新 req-3 在前。
	if got[0].RequestID != "req-3" || got[2].RequestID != "req-1" {
		t.Fatalf("倒序异常: %s %s %s", got[0].RequestID, got[1].RequestID, got[2].RequestID)
	}

	// server + status 组合。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{ServerID: "s1", Status: "success"})
	if len(got) != 2 || total != 2 {
		t.Fatalf("server+status 过滤异常: %d / total=%d", len(got), total)
	}

	// server + instance 组合：命中 s1 的 i1 两条（s2/i2 与 i1 均排除）。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{ServerID: "s1", InstanceID: "i1"})
	if len(got) != 2 || total != 2 {
		t.Fatalf("server+instance 过滤异常: %d / total=%d", len(got), total)
	}

	// 关键词命中 request_id。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{Q: "req-2"})
	if len(got) != 1 || got[0].RequestID != "req-2" || total != 1 {
		t.Fatalf("q 过滤异常: %d", len(got))
	}

	// 时间闭区间：命中 2~3 分钟两条。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{From: base.Add(2 * time.Minute), To: base.Add(3 * time.Minute)})
	if len(got) != 2 || total != 2 {
		t.Fatalf("时间区间过滤异常: %d / total=%d", len(got), total)
	}

	// 分页取最近 2 条。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{Paging: query.Paging{Page: 1, PageSize: 2}})
	if len(got) != 2 || got[0].RequestID != "req-3" || got[1].RequestID != "req-2" || total != 3 {
		t.Fatalf("分页异常: %d / total=%d", len(got), total)
	}
}
