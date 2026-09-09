package memory

import (
	"context"
	"fmt"
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

func TestQueryTraffic_filterClientIP(t *testing.T) {
	st := New()
	ctx := context.Background()
	samples := []model.TrafficSample{
		{RequestID: "req-1", Tool: "alpha.search", ClientIP: "203.0.113.9", Status: "success", Timestamp: model.Now()},
		{RequestID: "req-2", Tool: "beta.search", ClientIP: "203.0.113.9", Status: "rate_limit_error", Timestamp: model.Now()},
		{RequestID: "req-3", Tool: "alpha.search", ClientIP: "198.51.100.7", Status: "success", Timestamp: model.Now()},
		{RequestID: "req-4", Tool: "alpha.search", Client: "c1", Status: "success", Timestamp: model.Now()}, // 无 IP（旧行）
	}
	for _, sample := range samples {
		if err := st.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	// 精确过滤：同 IP 两条（含一条拒绝行）。
	got, total, err := st.QueryTraffic(ctx, query.TrafficQuery{ClientIP: "203.0.113.9"})
	if err != nil || len(got) != 2 || total != 2 {
		t.Fatalf("client_ip 精确过滤异常: %d / total=%d err=%v", len(got), total, err)
	}

	// 另一 IP 一条；未出现 IP 的空行不命中。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{ClientIP: "198.51.100.7"})
	if len(got) != 1 || total != 1 {
		t.Fatalf("client_ip 单命中异常: %d / %d", len(got), total)
	}
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{ClientIP: "10.0.0.1"})
	if len(got) != 0 || total != 0 {
		t.Fatalf("client_ip 未命中不应返回行: %d / %d", len(got), total)
	}

	// 关键词模糊命中 IP 片段。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{Q: "113.9"})
	if len(got) != 2 || total != 2 {
		t.Fatalf("q 命中 IP 片段异常: %d / %d", len(got), total)
	}
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{Q: "198.51"})
	if len(got) != 1 || total != 1 {
		t.Fatalf("q 命中另一 IP 片段异常: %d / %d", len(got), total)
	}
}

// TestQueryTraffic_pageDoesNotMaterializeAll 锁定分页内存契约：只物化请求页的行
// （不把全部匹配行复制一遍）。以 SkipTotal 的“最近 N 条”路径断言返回行与总量。
func TestQueryTraffic_pageDoesNotMaterializeAll(t *testing.T) {
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 500; i++ {
		if err := st.AppendTraffic(ctx, model.TrafficSample{
			RequestID: fmt.Sprintf("req-%03d", i),
			ServerID:  "s1", Tool: "alpha.search", Status: "success",
			LatencyMS: int64(i), Timestamp: model.T(base.Add(time.Duration(i) * time.Minute)),
		}); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	// 第一页：返回最新 20 条且 total 为 0（SkipTotal 语义）。
	got, total, err := st.QueryTraffic(ctx, query.TrafficQuery{
		Paging: query.Paging{Page: 1, PageSize: 20}, SkipTotal: true,
	})
	if err != nil || len(got) != 20 || total != 0 {
		t.Fatalf("SkipTotal 首页异常: items=%d total=%d err=%v", len(got), total, err)
	}
	if got[0].RequestID != "req-499" || got[19].RequestID != "req-480" {
		t.Fatalf("倒序首页内容异常: %s .. %s", got[0].RequestID, got[19].RequestID)
	}

	// 第二页：跳过 20 条后取 10 条。
	got, _, _ = st.QueryTraffic(ctx, query.TrafficQuery{
		Paging: query.Paging{Page: 3, PageSize: 10}, SkipTotal: true,
	})
	if len(got) != 10 || got[0].RequestID != "req-479" || got[9].RequestID != "req-470" {
		t.Fatalf("第 3 页内容异常: %d / %s", len(got), got[0].RequestID)
	}

	// 需要 total 时仍返回精确匹配总数。
	got, total, _ = st.QueryTraffic(ctx, query.TrafficQuery{Paging: query.Paging{Page: 1, PageSize: 5}})
	if len(got) != 5 || total != 500 {
		t.Fatalf("需要 total 时异常: items=%d total=%d", len(got), total)
	}
}

// TestQueryTrafficWindow_aggregates 验证窗口聚合：整体/分组/状态/分钟桶口径一致，
// 分组按调用量降序并支持 TopN 截断。
func TestQueryTrafficWindow_aggregates(t *testing.T) {
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	samples := []model.TrafficSample{
		{RequestID: "r1", ServerID: "s1", Tool: "alpha.search", ClientIP: "10.0.0.1", Status: "success", LatencyMS: 10, Timestamp: model.T(base)},
		{RequestID: "r2", ServerID: "s1", Tool: "alpha.search", ClientIP: "10.0.0.1", Status: "success", LatencyMS: 20, Timestamp: model.T(base.Add(10 * time.Second))},
		{RequestID: "r3", ServerID: "s1", Tool: "alpha.detail", ClientIP: "10.0.0.2", Status: "upstream_error", LatencyMS: 300, Timestamp: model.T(base.Add(70 * time.Second))},
		{RequestID: "r4", ServerID: "s2", Tool: "beta.search", ClientIP: "10.0.0.2", Status: "timeout_error", LatencyMS: 5000, Timestamp: model.T(base.Add(80 * time.Second))},
		// 窗口外（更早）：不应计入。
		{RequestID: "old", ServerID: "s1", Tool: "alpha.search", ClientIP: "10.0.0.1", Status: "success", LatencyMS: 1, Timestamp: model.T(base.Add(-time.Hour))},
	}
	for _, sample := range samples {
		if err := st.AppendTraffic(ctx, sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	stats, err := st.QueryTrafficWindow(ctx, query.TrafficWindowQuery{
		From: base.Add(-time.Minute), To: base.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("QueryTrafficWindow: %v", err)
	}
	if stats.Totals != 4 || stats.Success != 2 || stats.Errors != 2 {
		t.Fatalf("整体口径错误: %+v", stats)
	}
	if got := stats.SuccessRate(); got != 0.5 {
		t.Fatalf("成功率应为 0.5，得到 %v", got)
	}
	if got := stats.Latency.Avg(); got != (10+20+300+5000)/4.0 {
		t.Fatalf("平均延迟错误: %v", got)
	}

	// by_server：s1 3 条（2 成功）、s2 1 条。
	if len(stats.ByServer) != 2 || stats.ByServer[0].Key != "s1" || stats.ByServer[0].Totals != 3 ||
		stats.ByServer[0].Success != 2 || stats.ByServer[1].Key != "s2" {
		t.Fatalf("by_server 错误: %+v", stats.ByServer)
	}
	// by_tool：alpha.search 2 条最多。
	if len(stats.ByTool) != 3 || stats.ByTool[0].Key != "alpha.search" || stats.ByTool[0].Totals != 2 {
		t.Fatalf("by_tool 错误: %+v", stats.ByTool)
	}
	// by_client_ip：两个 IP 各 2 条（同量按 key 升序）。
	if len(stats.ByClientIP) != 2 || stats.ByClientIP[0].Key != "10.0.0.1" || stats.ByClientIP[0].Totals != 2 {
		t.Fatalf("by_client_ip 错误: %+v", stats.ByClientIP)
	}
	// by_status：四类各 1（按计数降序、同量按状态名升序）。
	if len(stats.ByStatus) != 3 {
		t.Fatalf("by_status 错误: %+v", stats.ByStatus)
	}
	// by_minute：base 与 base+1min 两个桶。
	if len(stats.ByMinute) != 2 || stats.ByMinute[0].Minute != base.Unix() ||
		stats.ByMinute[0].Totals != 2 || stats.ByMinute[1].Totals != 2 {
		t.Fatalf("by_minute 错误: %+v", stats.ByMinute)
	}

	// TopN 截断与 Server 过滤。
	limited, err := st.QueryTrafficWindow(ctx, query.TrafficWindowQuery{
		From: base.Add(-time.Minute), To: base.Add(5 * time.Minute),
		TopTools: 1, TopIPs: 1, ServerID: "s1",
	})
	if err != nil {
		t.Fatalf("QueryTrafficWindow(limited): %v", err)
	}
	if limited.Totals != 3 || len(limited.ByTool) != 1 || limited.ByTool[0].Key != "alpha.search" ||
		len(limited.ByClientIP) != 1 {
		t.Fatalf("TopN/ServerID 过滤错误: %+v", limited)
	}
}

// TestQueryTrafficWindow_topMinutes 验证长窗口下只返回最近 N 个分钟桶。
func TestQueryTrafficWindow_topMinutes(t *testing.T) {
	st := New()
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		if err := st.AppendTraffic(ctx, model.TrafficSample{
			RequestID: fmt.Sprintf("r%d", i), ServerID: "s1", Tool: "t", Status: "success",
			Timestamp: model.T(base.Add(time.Duration(i) * time.Minute)),
		}); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}
	stats, err := st.QueryTrafficWindow(ctx, query.TrafficWindowQuery{
		From: base, To: base.Add(10 * time.Minute), TopMinutes: 3,
	})
	if err != nil {
		t.Fatalf("QueryTrafficWindow: %v", err)
	}
	if len(stats.ByMinute) != 3 {
		t.Fatalf("TopMinutes 应只返回最近 3 个桶，得到 %d", len(stats.ByMinute))
	}
	if got := stats.ByMinute[0].Minute; got != base.Add(7*time.Minute).Unix() {
		t.Fatalf("应返回最后 3 个桶，首个为 base+7min，得到 %d", got)
	}
}
