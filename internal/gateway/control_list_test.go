package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// TestControlListServersPagedEnvelope 覆盖列表统一分页信封 + 全量模式（page_size<=0）。
func TestControlListServersPagedEnvelope(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, nil, nil)
	ctx := context.Background()
	for i, name := range []string{"A", "B", "C", "D", "E"} {
		srv := &model.Server{Name: name, Enabled: true}
		if err := store.CreateServer(ctx, srv); err != nil {
			t.Fatalf("CreateServer: %v", err)
		}
		_ = i
	}

	// page1/page2/page3。
	page := func(qs string) ([]model.Server, int) {
		rec := httptest.NewRecorder()
		ctrl.handleListServers(rec, httptest.NewRequest(http.MethodGet, "/api/servers?"+qs, nil))
		var data struct {
			Items []model.Server `json:"items"`
			Total int            `json:"total"`
		}
		if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &data); err != nil {
			t.Fatalf("解析分页响应失败: %v", err)
		}
		return data.Items, data.Total
	}

	items, total := page("page=1&page_size=2")
	if len(items) != 2 || total != 5 {
		t.Fatalf("page1 异常: items=%d total=%d", len(items), total)
	}
	items, _ = page("page=2&page_size=2")
	if len(items) != 2 {
		t.Fatalf("page2 异常: %d", len(items))
	}
	items, _ = page("page=3&page_size=2")
	if len(items) != 1 {
		t.Fatalf("page3 异常: %d", len(items))
	}

	// 全量模式：page_size=0 返回全部。
	items, total = page("page_size=0")
	if len(items) != 5 || total != 5 {
		t.Fatalf("all-mode 异常: items=%d total=%d", len(items), total)
	}
}

// TestControlListFilters 覆盖各列表筛选参数透传到存储。
func TestControlListFilters(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, nil, nil)
	ctx := context.Background()

	if err := store.CreateServer(ctx, &model.Server{Name: "Payment", Enabled: true, HealthStatus: model.ServerStatusHealthy}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if err := store.CreateServer(ctx, &model.Server{Name: "Search", Enabled: false, HealthStatus: model.ServerStatusUnhealthy}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	_ = store.UpsertTool(ctx, &model.Tool{ServerID: "s1", OriginalName: "search", GatewayName: "svc.search", Enabled: true})
	_ = store.UpsertTool(ctx, &model.Tool{ServerID: "s2", OriginalName: "detail", GatewayName: "svc.detail", Enabled: false})
	_ = store.CreateAccessKey(ctx, &model.AccessKey{Name: "Partner", Subject: "partner", Enabled: true})

	// /api/servers?q=earch 命中 Search。
	rec := httptest.NewRecorder()
	ctrl.handleListServers(rec, httptest.NewRequest(http.MethodGet, "/api/servers?q=earch&enabled=false", nil))
	var servers struct {
		Items []model.Server `json:"items"`
		Total int            `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &servers); err != nil || len(servers.Items) != 1 || servers.Items[0].Name != "Search" || servers.Total != 1 {
		t.Fatalf("servers q/enabled 过滤异常: %+v", servers)
	}

	// /api/servers?health_status=unhealthy 命中 Search。
	rec = httptest.NewRecorder()
	ctrl.handleListServers(rec, httptest.NewRequest(http.MethodGet, "/api/servers?health_status=unhealthy", nil))
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &servers); err != nil || len(servers.Items) != 1 {
		t.Fatalf("servers health_status 过滤异常: %+v", servers)
	}

	// /api/tools?server_id=s1&enabled=true 命中 svc.search。
	rec = httptest.NewRecorder()
	ctrl.handleListTools(rec, httptest.NewRequest(http.MethodGet, "/api/tools?server_id=s1&enabled=true", nil))
	var tools struct {
		Items []model.Tool `json:"items"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &tools); err != nil || len(tools.Items) != 1 || tools.Items[0].GatewayName != "svc.search" || tools.Total != 1 {
		t.Fatalf("tools 筛选异常: %+v", tools)
	}

	// /api/keys?q=art 命中 Partner。
	rec = httptest.NewRecorder()
	ctrl.handleListKeys(rec, httptest.NewRequest(http.MethodGet, "/api/keys?q=art", nil))
	var keys struct {
		Items []model.AccessKey `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &keys); err != nil || len(keys.Items) != 1 || keys.Items[0].Name != "Partner" || keys.Total != 1 {
		t.Fatalf("keys q 过滤异常: %+v", keys)
	}
}

// TestControlLogsFilter 覆盖调用日志 server/status/时间筛选。
func TestControlLogsFilter(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, nil, nil)
	now := time.Now().UTC().Truncate(time.Second)
	samples := []model.TrafficSample{
		{RequestID: "req-1", ServerID: "s1", InstanceID: "i1", Tool: "svc.search", Client: "c1", Status: "success", Timestamp: now.Add(-2 * time.Minute)},
		{RequestID: "req-2", ServerID: "s2", InstanceID: "i2", Tool: "svc.search", Client: "c1", Status: "upstream_error", Timestamp: now.Add(-4 * time.Minute)},
		{RequestID: "req-3", ServerID: "s1", InstanceID: "i2", Tool: "svc.detail", Client: "c1", Status: "success", Timestamp: now.Add(-5 * time.Minute)},
	}
	for _, sample := range samples {
		if err := store.AppendTraffic(context.Background(), sample); err != nil {
			t.Fatalf("AppendTraffic: %v", err)
		}
	}

	from := now.Add(-3 * time.Minute).Format(time.RFC3339)
	rec := httptest.NewRecorder()
	ctrl.handleLogs(rec, httptest.NewRequest(http.MethodGet, "/api/logs?server_id=s1&status=success&from="+strings.ReplaceAll(from, "+", "%2B"), nil))
	var logs struct {
		Items []model.TrafficSample `json:"items"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &logs); err != nil || len(logs.Items) != 1 || logs.Items[0].RequestID != "req-1" || logs.Items[0].InstanceID != "i1" || logs.Total != 1 {
		t.Fatalf("logs 筛选异常: %+v err=%v", logs, err)
	}

	// 实例级筛选：server_id + instance_id 收敛到该 Server 的指定实例。
	rec = httptest.NewRecorder()
	ctrl.handleLogs(rec, httptest.NewRequest(http.MethodGet, "/api/logs?server_id=s1&instance_id=i2", nil))
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &logs); err != nil || len(logs.Items) != 1 || logs.Items[0].RequestID != "req-3" || logs.Total != 1 {
		t.Fatalf("instance 筛选异常: %+v err=%v", logs, err)
	}
}

// TestControlListParamValidation 覆盖非法分页/筛选参数 → 400 invalid_argument。
func TestControlListParamValidation(t *testing.T) {
	store := memory.New()
	ctrl := NewControl(nil, store, nil, nil)

	// servers 通用分页/布尔/枚举参数校验。
	cases := []string{
		"page=0",
		"page=abc",
		"page_size=999",
		"page_size=abc",
		"enabled=maybe",
		"health_status=weird",
	}
	for _, qs := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/servers?"+qs, nil)
		ctrl.handleListServers(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_argument") {
			t.Fatalf("%s: 期望 400 invalid_argument，得到 %d / %s", qs, rec.Code, rec.Body.String())
		}
	}

	// status/from 等 logs 专属参数校验走 /api/logs。
	for _, qs := range []string{"status=bogus", "from=notatime", "page_size=999"} {
		rec := httptest.NewRecorder()
		ctrl.handleLogs(rec, httptest.NewRequest(http.MethodGet, "/api/logs?"+qs, nil))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_argument") {
			t.Fatalf("logs %s: 期望 400 invalid_argument，得到 %d / %s", qs, rec.Code, rec.Body.String())
		}
	}
}
