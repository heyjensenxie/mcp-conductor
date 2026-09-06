package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// trendSeriesResp 仅用于测试解码 {series:[...]}。
type trendSeriesResp struct {
	Series []struct {
		TS     int64 `json:"ts"`
		Totals int64 `json:"totals"`
		Errors int64 `json:"errors"`
	} `json:"series"`
}

// rawEnvelope 解析任意状态码的信封（不因非 ok 而 fatal）。
func rawEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (int, string, json.RawMessage) {
	t.Helper()
	var e struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("解析信封失败: %v", err)
	}
	return rec.Code, e.Message, e.Data
}

// runTrend 直接调用 handleMetricsTrend 并返回信封。
func runTrend(t *testing.T, ctrl *Control, rawurl string) (int, trendSeriesResp) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctrl.handleMetricsTrend(rec, httptest.NewRequest(http.MethodGet, rawurl, nil))
	code, _, data := rawEnvelope(t, rec)
	var out trendSeriesResp
	if code == http.StatusOK {
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("解析 series 失败: %v", err)
		}
	}
	return code, out
}

func TestTrendScopeAndMinutesValidation(t *testing.T) {
	ctrl := NewControl(nil, memory.New(), observability.NewMetrics(), nil)
	if code, _ := runTrend(t, ctrl, "/api/metrics/trend?scope=instance"); code != http.StatusBadRequest {
		t.Fatalf("instance 缺 server_id 应为 400，得到 %d", code)
	}
	if code, _ := runTrend(t, ctrl, "/api/metrics/trend?scope=bogus"); code != http.StatusBadRequest {
		t.Fatalf("未知 scope 应为 400，得到 %d", code)
	}
	if code, _ := runTrend(t, ctrl, "/api/metrics/trend?minutes=abc"); code != http.StatusBadRequest {
		t.Fatalf("minutes 非法应为 400，得到 %d", code)
	}
}

// TestTrendMergeStoreAndHot 验证 store 权威 + 热桶补缺：不同分钟各自计入，连续补零。
func TestTrendMergeStoreAndHot(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	m := observability.NewMetrics()
	ctrl := NewControl(nil, store, m, nil)

	nowMin := time.Now().UTC().Truncate(time.Minute).Unix()
	// store：过去一分钟已闭合桶（两个工具聚合）。
	if err := store.UpsertTrendBuckets(ctx, []model.TrendMinute{
		{Scope: "tool", DimKey: "demo", Minute: nowMin - 60, Totals: 2, Errors: 1},
		{Scope: "tool", DimKey: "other", Minute: nowMin - 60, Totals: 3},
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	// 热桶：当前 open 分钟（尚未落库，应被补齐而非双计）。
	m.Record("demo", true, time.Millisecond)
	m.Record("demo", true, time.Millisecond)

	code, res := runTrend(t, ctrl, "/api/metrics/trend?minutes=3")
	if code != http.StatusOK {
		t.Fatalf("trend 应成功，得到 %d", code)
	}
	if len(res.Series) != 3 {
		t.Fatalf("series 长度应为 3，得到 %d", len(res.Series))
	}
	last := res.Series[len(res.Series)-1]
	prev := res.Series[len(res.Series)-2]
	if prev.TS != nowMin-60 || prev.Totals != 5 || prev.Errors != 1 {
		t.Fatalf("store 闭合分钟聚合异常: %+v", prev)
	}
	if last.TS != nowMin || last.Totals != 2 {
		t.Fatalf("热桶 open 分钟应补齐 totals=2，得到 %+v", last)
	}
}

// TestTrendDimKey 验证 dim_key 单维聚焦（排除同 scope 其他维度）。
func TestTrendDimKey(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	nowMin := time.Now().UTC().Truncate(time.Minute).Unix()
	if err := store.UpsertTrendBuckets(ctx, []model.TrendMinute{
		{Scope: "tool", DimKey: "demo", Minute: nowMin - 60, Totals: 2},
		{Scope: "tool", DimKey: "other", Minute: nowMin - 60, Totals: 3},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctrl := NewControl(nil, store, observability.NewMetrics(), nil)
	_, res := runTrend(t, ctrl, "/api/metrics/trend?minutes=3&dim_key=other")
	mid := res.Series[len(res.Series)-2]
	if mid.Totals != 3 {
		t.Fatalf("dim_key=other 应只聚合 other，得到 %d", mid.Totals)
	}
}

// TestMergeTrendSeriesNoDoubleCount 单测合并算法：store 已含分钟不因热桶双计；
// store 缺失分钟由热桶补入；缺数据分钟补零。
func TestMergeTrendSeriesNoDoubleCount(t *testing.T) {
	rows := []model.TrendMinute{
		{Scope: "tool", ServerID: "", DimKey: "demo", Minute: 100, Totals: 2, Errors: 1},
	}
	// 热桶与 store 同维同分钟重叠（模拟 flush 前刚又+1）→ 必须被 store 覆盖不叠加；
	// 另给 store 完全没有的下一分钟 160（时序点按 60s 步进：100,160,220,280）。
	hot := []model.TrendMinute{
		{Scope: "tool", ServerID: "", DimKey: "demo", Minute: 100, Totals: 5},
		{Scope: "tool", ServerID: "", DimKey: "demo", Minute: 160, Totals: 1},
	}
	got := mergeTrendSeries(rows, hot, 100, 4) // 窗口 100,160,220,280
	if len(got) != 4 {
		t.Fatalf("长度应为 4，得到 %d", len(got))
	}
	byMin := map[int64]struct{ t, e int64 }{}
	for _, p := range got {
		byMin[p.TS] = struct{ t, e int64 }{p.Totals, p.Errors}
	}
	if byMin[100].t != 2 || byMin[100].e != 1 {
		t.Fatalf("minute 100 应取 store 值(2,1) 不被热桶 5 叠加: %+v", byMin[100])
	}
	if byMin[160].t != 1 {
		t.Fatalf("minute 160 应由热桶补入: %+v", byMin[160])
	}
	if byMin[220].t != 0 || byMin[280].t != 0 {
		t.Fatalf("空分钟应补零: %+v", byMin)
	}
}
