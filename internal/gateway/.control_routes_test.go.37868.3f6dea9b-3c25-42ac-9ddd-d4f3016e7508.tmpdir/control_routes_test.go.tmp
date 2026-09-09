package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/config"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// TestControlRoutesRegister 验证控制面路由注册本身是合法的：Go 1.22 ServeMux 在
// 模式冲突（如重复注册同一 "METHOD /path"）时会 panic，本测试通过真实注册 + 命中
// 新增端点，防止后续新增路由时把服务注册阶段打崩。
func TestControlRoutesRegister(t *testing.T) {
	store := memory.New()
	metrics := observability.NewMetrics()
	mux := http.NewServeMux()
	registerControlRoutes(mux, Deps{
		Registry: registry.NewService(store, noopDiscoverer{}),
		Store:    store,
		Metrics:  metrics,
	}, newRuntimeConfigCache(store, fallbackRuntimeConfig(config.Config{})), false)

	cases := []struct {
		name   string
		method string
		target string
		status int
	}{
		{"窗口聚合端点已注册", http.MethodGet, "/api/metrics/window?minutes=30", http.StatusOK},
		{"趋势端点已注册", http.MethodGet, "/api/metrics/trend?scope=tool&minutes=30", http.StatusOK},
		{"指标快照端点已注册", http.MethodGet, "/api/metrics", http.StatusOK},
		{"保存前测试连接端点已注册", http.MethodPost, "/api/servers/test-connection", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, nil))
			if rec.Code != tc.status {
				t.Fatalf("期望 %d，得到 %d / %s", tc.status, rec.Code, rec.Body.String())
			}
		})
	}
}
