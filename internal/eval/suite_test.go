package eval

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
)

// stubCaller 固定返回文本或错误。
type stubCaller struct {
	text string
	err  error
}

func (s stubCaller) Call(_ context.Context, _ model.Server, _ model.Instance, _ string, _ map[string]any, _ map[string]string) ([]registry.CallContent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []registry.CallContent{{Type: "text", Text: s.text}}, nil
}

func suiteServer() model.Server { return model.Server{ID: "srv-1", Name: "Mock"} }

func suiteInstance() model.Instance {
	return model.Instance{ID: "inst-1", ServerID: "srv-1", Endpoint: "http://mock/mcp", Enabled: true, HealthStatus: model.ServerStatusHealthy}
}

// TestRunSuite_passAndFailNotFound 覆盖命中/未命中/映射缺失三条路径与汇总。
func TestRunSuite_passAndFailNotFound(t *testing.T) {
	ctx := context.Background()
	toolMap := map[string]string{"mock.search": "search"}
	cases := []SuiteCase{
		{Name: "命中", GatewayTool: "mock.search", Arguments: map[string]any{"q": "x"}, ExpectedSubstring: "result"},
		{Name: "未命中", GatewayTool: "mock.search", Arguments: map[string]any{"q": "x"}, ExpectedSubstring: "不存在的文本"},
		{Name: "缺失工具", GatewayTool: "mock.gone", ExpectedSubstring: "result"},
	}
	results := runSuite(ctx, stubCaller{text: "hello result"}, suiteServer(), suiteInstance(), toolMap, cases, time.Second)
	if results[0].Passed != true || results[0].Matched != true {
		t.Fatalf("命中 case 应通过: %+v", results[0])
	}
	if results[1].Passed || results[1].Matched {
		t.Fatalf("未命中 case 应失败: %+v", results[1])
	}
	if results[2].Passed || results[2].ErrorCode != string(errs.CodeNotFound) {
		t.Fatalf("缺失工具应 fail not_found: %+v", results[2])
	}

	out := summarizeSuite(suiteServer(), results)
	if out.Summary.Total != 3 || out.Summary.Passed != 1 || out.Summary.Failed != 2 {
		t.Fatalf("汇总不正确: %+v", out.Summary)
	}
	if len(out.ByTool) != 2 {
		t.Fatalf("按工具分组应 2 组（含缺失工具组）: %+v", out.ByTool)
	}
}

// TestRunSuite_upstreamError 覆盖上游错误被归类为错误码。
func TestRunSuite_upstreamError(t *testing.T) {
	toolMap := map[string]string{"mock.search": "search"}
	cases := []SuiteCase{{Name: "上游错误", GatewayTool: "mock.search", ExpectedSubstring: ""}}
	results := runSuite(context.Background(), stubCaller{err: errs.New(errs.CodeUpstream, "boom")}, suiteServer(), suiteInstance(), toolMap, cases, time.Second)
	if len(results) != 1 || results[0].Passed || results[0].ErrorCode != string(errs.CodeUpstream) {
		t.Fatalf("上游错误应 fail upstream_error: %+v", results)
	}
}

// TestRunSuite_emptyExpectedPasses 覆盖：不填期望文本时调用成功即通过。
func TestRunSuite_emptyExpectedPasses(t *testing.T) {
	toolMap := map[string]string{"mock.search": "search"}
	cases := []SuiteCase{{Name: "无期望", GatewayTool: "mock.search", Arguments: map[string]any{"q": "x"}}}
	results := runSuite(context.Background(), stubCaller{text: "ok"}, suiteServer(), suiteInstance(), toolMap, cases, time.Second)
	if len(results) != 1 || !results[0].Passed || results[0].Error != "" {
		t.Fatalf("成功且无期望应通过: %+v", results)
	}
}

// TestSummarize_p95 覆盖 p95 汇总计算。
func TestSummarize_p95(t *testing.T) {
	results := []SuiteCaseResult{
		{Name: "a", GatewayTool: "mock.search", Passed: true, LatencyMS: 100},
		{Name: "b", GatewayTool: "mock.search", Passed: true, LatencyMS: 200},
		{Name: "c", GatewayTool: "mock.search", Passed: false, LatencyMS: 900},
	}
	out := summarizeSuite(suiteServer(), results)
	if out.Summary.PassRate != 2.0/3.0 {
		t.Fatalf("pass rate 应为 2/3，得到 %v", out.Summary.PassRate)
	}
	if out.Summary.P95LatencyMS != 900 {
		t.Fatalf("p95 应为 900，得到 %v", out.Summary.P95LatencyMS)
	}
	if !strings.Contains(out.Cases[2].GatewayTool, "mock") {
		t.Fatalf("输出异常: %+v", out.Cases[2])
	}
}
