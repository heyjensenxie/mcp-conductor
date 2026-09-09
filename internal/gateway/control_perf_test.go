package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// seedDashboardStore 构造可复现的规模数据，供仪表盘/可观测性端点的性能基准使用：
// servers 个 Server、每 Server toolsPerServer 个工具、traffic 条调用日志（均匀铺满
// days 天）、每工具 500 条延迟样本（进程内指标）与 120 个分钟桶（趋势）。
func seedDashboardStore(tb testing.TB, servers, toolsPerServer, traffic, days int) (*memory.Store, *observability.Metrics) {
	tb.Helper()
	ctx := context.Background()
	store := memory.New()
	metrics := observability.NewMetrics()
	now := time.Now().UTC()

	serverIDs := make([]string, 0, servers)
	for i := 0; i < servers; i++ {
		srv := &model.Server{
			Name: fmt.Sprintf("perf-srv-%02d", i), Enabled: true,
			HealthStatus: model.ServerStatusHealthy, CreatedAt: model.Now(), UpdatedAt: model.Now(),
		}
		if err := store.CreateServer(ctx, srv); err != nil {
			tb.Fatalf("CreateServer: %v", err)
		}
		serverIDs = append(serverIDs, srv.ID)
	}

	tools := make([]string, 0, servers*toolsPerServer)
	for i := 0; i < servers*toolsPerServer; i++ {
		gw := fmt.Sprintf("perf.tool_%03d", i)
		tool := &model.Tool{
			ServerID: serverIDs[i%len(serverIDs)], OriginalName: gw, GatewayName: gw,
			Enabled: true, CreatedAt: model.Now(), UpdatedAt: model.Now(),
		}
		if err := store.UpsertTool(ctx, tool); err != nil {
			tb.Fatalf("UpsertTool: %v", err)
		}
		tools = append(tools, gw)
	}

	span := time.Duration(days) * 24 * time.Hour
	for i := 0; i < traffic; i++ {
		status := "success"
		if i%20 == 0 {
			status = "upstream_error"
		}
		ts := now.Add(-time.Duration(float64(i) / float64(traffic) * float64(span)))
		if err := store.AppendTraffic(ctx, model.TrafficSample{
			RequestID: fmt.Sprintf("req-%06d", i),
			ServerID:  serverIDs[i%len(serverIDs)],
			Tool:      tools[i%len(tools)],
			Client:    "perf-client",
			ClientIP:  "10.0.0.1",
			Status:    status,
			LatencyMS: int64(10 + i%400),
			Timestamp: model.T(ts),
		}); err != nil {
			tb.Fatalf("AppendTraffic: %v", err)
		}
	}

	for _, gw := range tools {
		for i := 0; i < 500; i++ {
			metrics.Record(gw, i%20 != 0, time.Duration(5+i%300)*time.Millisecond)
		}
	}

	buckets := make([]model.TrendMinute, 0, len(tools)*120)
	base := now.Truncate(time.Minute).Unix()
	for _, gw := range tools {
		for m := 0; m < 120; m++ {
			buckets = append(buckets, model.TrendMinute{
				Scope: "tool", DimKey: gw, Minute: base - int64(m)*60,
				Totals: int64(m%7 + 1), Errors: int64(m % 2),
			})
		}
	}
	if err := store.UpsertTrendBuckets(ctx, buckets); err != nil {
		tb.Fatalf("UpsertTrendBuckets: %v", err)
	}
	return store, metrics
}

// BenchmarkDashboardEndpoints 度量仪表盘首屏（servers/tools/trend/logs）与可观测性
// 快照端点的单次耗时，用于评估统计口径随流量增长的成本。
func BenchmarkDashboardEndpoints(b *testing.B) {
	store, metrics := seedDashboardStore(b, 10, 20, 50000, 30)
	ctrl := NewControl(registry.NewService(store, noopDiscoverer{}), store, metrics, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/servers", ctrl.handleListServers)
	mux.HandleFunc("GET /api/tools", ctrl.handleListTools)
	mux.HandleFunc("GET /api/logs", ctrl.handleLogs)
	mux.HandleFunc("GET /api/metrics", ctrl.handleMetrics)
	mux.HandleFunc("GET /api/metrics/window", ctrl.handleMetricsWindow)
	mux.HandleFunc("GET /api/metrics/trend", ctrl.handleMetricsTrend)

	now := time.Now().UTC()
	q := url.Values{}
	q.Set("page", "1")
	q.Set("page_size", "200")
	q.Set("include_total", "false")
	q.Set("from", now.Add(-30*time.Minute).Format(time.RFC3339))
	q.Set("to", now.Format(time.RFC3339))
	logsWindow := "/api/logs?" + q.Encode()
	logsRecent := "/api/logs?page=1&page_size=20&include_total=false"

	cases := []struct{ name, target string }{
		{"servers-all", "/api/servers?page_size=0&include_total=false"},
		{"tools-all", "/api/tools?page_size=0&include_total=false"},
		{"trend-30m", "/api/metrics/trend?scope=tool&minutes=30"},
		{"window-30m", "/api/metrics/window?minutes=30"},
		{"window-1d", "/api/metrics/window?minutes=1440"},
		{"logs-window-200", logsWindow},
		{"logs-recent-20", logsRecent},
		{"metrics-snapshot-tool", "/api/metrics"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
				if rec.Code != http.StatusOK {
					b.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
			}
		})
	}
}
