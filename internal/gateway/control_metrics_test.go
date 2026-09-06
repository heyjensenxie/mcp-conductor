package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// TestHandleMetricsScopeIsolation 验证 /api/metrics 的 tool/server/instance 三档
// scope 过滤正确：默认（工具）不含 server:/instance: 前缀；scope=server 只含
// server:；scope=instance 只含 instance:，并可用 server_id 前缀收敛。
func TestHandleMetricsScopeIsolation(t *testing.T) {
	metrics := observability.NewMetrics()
	metrics.Record("mock.search", true, time.Millisecond) // 工具维
	metrics.Record(observability.ServerDimPrefix+"srv-1", true, time.Millisecond)
	metrics.Record(observability.InstanceDimPrefix+"srv-1:inst-1", true, time.Millisecond)
	metrics.Record(observability.InstanceDimPrefix+"srv-1:inst-2", false, time.Millisecond)

	ctrl := NewControl(nil, memory.New(), metrics, nil)

	rows := getMetricsRows(t, ctrl, "/api/metrics")
	if len(rows) != 1 || rows[0].Key != "mock.search" {
		t.Fatalf("默认 scope 应只含工具维，得到 %+v", rows)
	}

	rows = getMetricsRows(t, ctrl, "/api/metrics?scope=server")
	if len(rows) != 1 || rows[0].Key != observability.ServerDimPrefix+"srv-1" {
		t.Fatalf("scope=server 应只含 server: 行，得到 %+v", rows)
	}

	rows = getMetricsRows(t, ctrl, "/api/metrics?scope=instance&server_id=srv-1")
	if len(rows) != 2 {
		t.Fatalf("scope=instance 应返回该 Server 的两条实例行，得到 %+v", rows)
	}

	rows = getMetricsRows(t, ctrl, "/api/metrics?scope=instance&server_id=srv-9")
	if len(rows) != 0 {
		t.Fatalf("scope=instance 对无实例的 Server 应返回空，得到 %+v", rows)
	}
}

// getMetricsRows 请求指定路径并解出 metric 快照行。
func getMetricsRows(t *testing.T, ctrl *Control, path string) []observability.Snapshot {
	t.Helper()
	rec := httptest.NewRecorder()
	ctrl.handleMetrics(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 应返回 200，得到 %d", path, rec.Code)
	}
	var out []observability.Snapshot
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &out); err != nil {
		t.Fatalf("解析快照失败: %v", err)
	}
	return out
}
