package eval

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
)

// ToolCaller 是回归用例执行所需的直连上游调用能力（由 mcpclient.Adapter 满足）。
type ToolCaller interface {
	Call(ctx context.Context, server model.Server, instance model.Instance, tool string, arguments map[string]any, extraHeaders map[string]string) ([]registry.CallContent, error)
}

// SuiteCase 是回归用例：输入用网关工具名（调用前映射回上游原名）。
type SuiteCase struct {
	Name              string         `json:"name"`
	GatewayTool       string         `json:"gateway_tool"`
	Arguments         map[string]any `json:"arguments"`
	ExpectedSubstring string         `json:"expected_substring"`
	TimeoutMS         int            `json:"timeout_ms,omitempty"` // 0 → 使用默认上游超时
}

// SuiteCaseResult 是单个用例的执行结果。
type SuiteCaseResult struct {
	Name          string `json:"name"`
	GatewayTool   string `json:"gateway_tool"`
	OriginalTool  string `json:"original_tool"`
	Passed        bool   `json:"passed"`
	Matched       bool   `json:"matched"`
	LatencyMS     int64  `json:"latency_ms"`
	ErrorCode     string `json:"error_code,omitempty"`
	Error         string `json:"error,omitempty"`
	OutputSnippet string `json:"output_snippet,omitempty"`
}

// SuiteSummary 是一次回归运行的汇总。
type SuiteSummary struct {
	Total        int     `json:"total"`
	Passed       int     `json:"passed"`
	Failed       int     `json:"failed"`
	PassRate     float64 `json:"pass_rate"` // 0..1
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	P95LatencyMS float64 `json:"p95_latency_ms"`
}

// SuiteToolGroup 是按网关工具分组的汇总。
type SuiteToolGroup struct {
	GatewayTool  string  `json:"gateway_tool"`
	Total        int     `json:"total"`
	Passed       int     `json:"passed"`
	P95LatencyMS float64 `json:"p95_latency_ms"`
}

// SuiteResult 是一次回归运行的完整输出。
type SuiteResult struct {
	ServerID   string            `json:"server_id"`
	ServerName string            `json:"server_name"`
	Summary    SuiteSummary      `json:"summary"`
	Cases      []SuiteCaseResult `json:"cases"`
	ByTool     []SuiteToolGroup  `json:"by_tool"`
}

// runSuite 顺序执行全部用例并判定（直连上游，评价对象是上游本身）。
// toolMap: 网关工具名 → 上游原名；缺失映射的用例直接 fail（not_found）。
func runSuite(ctx context.Context, caller ToolCaller, server model.Server, instance model.Instance,
	toolMap map[string]string, cases []SuiteCase, defaultTimeout time.Duration) []SuiteCaseResult {

	results := make([]SuiteCaseResult, 0, len(cases))
	for _, tc := range cases {
		original, ok := toolMap[tc.GatewayTool]
		if !ok {
			results = append(results, SuiteCaseResult{
				Name: tc.Name, GatewayTool: tc.GatewayTool, Passed: false,
				ErrorCode: string(errs.CodeNotFound),
				Error:     "网关工具不存在（可能已被删除/改名/未发现）",
			})
			continue
		}

		timeout := defaultTimeout
		if tc.TimeoutMS > 0 {
			timeout = time.Duration(tc.TimeoutMS) * time.Millisecond
		}
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		contents, callErr := caller.Call(callCtx, server, instance, original, tc.Arguments, nil)
		cancel()
		latency := time.Since(start).Milliseconds()

		res := SuiteCaseResult{Name: tc.Name, GatewayTool: tc.GatewayTool, OriginalTool: original, LatencyMS: latency}
		if callErr != nil {
			res.ErrorCode = string(errs.CodeOf(callErr))
			res.Error = errs.SafeMessage(callErr)
			results = append(results, res)
			continue
		}
		text := joinContent(contents)
		res.Matched = strings.Contains(text, tc.ExpectedSubstring)
		// 未填写期望文本时：只要调用成功即视为通过。
		res.Passed = tc.ExpectedSubstring == "" || res.Matched
		if !res.Passed {
			res.Error = "输出未包含期望文本"
		}
		res.OutputSnippet = truncateRunes(text, 500)
		results = append(results, res)
	}
	return results
}

// summarizeSuite 由各用例结果计算汇总与按工具分组。
func summarizeSuite(server model.Server, results []SuiteCaseResult) SuiteResult {
	summary := SuiteSummary{Total: len(results)}
	latencies := make([]float64, 0, len(results))
	for i := range results {
		r := &results[i]
		if r.Passed {
			summary.Passed++
		}
		if r.LatencyMS > 0 {
			latencies = append(latencies, float64(r.LatencyMS))
		}
	}
	summary.Failed = summary.Total - summary.Passed
	if summary.Total > 0 {
		summary.PassRate = float64(summary.Passed) / float64(summary.Total)
	}
	if len(latencies) > 0 {
		var sum float64
		for _, v := range latencies {
			sum += v
		}
		summary.AvgLatencyMS = sum / float64(len(latencies))
		summary.P95LatencyMS = p95Of(latencies)
	}

	byTool := groupByTool(results)
	return SuiteResult{
		ServerID:   server.ID,
		ServerName: server.Name,
		Summary:    summary,
		Cases:      results,
		ByTool:     byTool,
	}
}

// groupByTool 按网关工具分组汇总结果（按键稳定排序）。
func groupByTool(results []SuiteCaseResult) []SuiteToolGroup {
	type acc struct {
		total, passed int
		lats          []float64
	}
	m := make(map[string]*acc)
	var order []string
	for i := range results {
		key := results[i].GatewayTool
		a, ok := m[key]
		if !ok {
			a = &acc{}
			m[key] = a
			order = append(order, key)
		}
		a.total++
		if results[i].Passed {
			a.passed++
		}
		if results[i].LatencyMS > 0 {
			a.lats = append(a.lats, float64(results[i].LatencyMS))
		}
	}
	sort.Strings(order)
	out := make([]SuiteToolGroup, 0, len(order))
	for _, key := range order {
		a := m[key]
		g := SuiteToolGroup{GatewayTool: key, Total: a.total, Passed: a.passed}
		if len(a.lats) > 0 {
			g.P95LatencyMS = p95Of(a.lats)
		}
		out = append(out, g)
	}
	return out
}

// joinContent 拼接工具调用的文本内容。
func joinContent(blocks []registry.CallContent) string {
	if len(blocks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(block.Text)
	}
	return b.String()
}

// p95Of 计算 95 分位（nearest-rank：向上取整定位，保证小样本也取到上尾值）。
func p95Of(sortedOrNot []float64) float64 {
	if len(sortedOrNot) == 0 {
		return 0
	}
	vals := make([]float64, len(sortedOrNot))
	copy(vals, sortedOrNot)
	sort.Float64s(vals)
	idx := int(math.Ceil(float64(len(vals))*0.95)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	return vals[idx]
}

// truncateRunes 截断文本到 rune 上限。
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
