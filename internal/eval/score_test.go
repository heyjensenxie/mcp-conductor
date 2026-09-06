package eval

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func goodToolSpec() ToolSpec {
	return ToolSpec{
		Name:        "search",
		Description: "按关键词在全局策略库中检索匹配的策略条目，返回命中的策略编号、标题、摘要与生效日期，适合需要定位政策或法规条目的查询场景；支持模糊与精确两种匹配模式，未提供关键词时返回空结果集，便于调用方在列表场景下使用。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{
					"type":        "string",
					"description": "检索关键词，必填",
				},
			},
			"required": []any{"q"},
		},
	}
}

func goodProtocol() ProtocolInfo {
	return ProtocolInfo{
		NegotiatedVersion: "2025-11-25",
		RecognizedVersion: true,
		ServerName:        "mock",
		ServerVersion:     "1.0",
		ToolsCapability:   true,
	}
}

// TestNoToolsNoTraffic_StaticZeroRuntimeNA 覆盖：无工具 + 无流量时静态三维为 0、
// 运行时两维 n/a 不纳分、总分 0、runtime note 存在。
func TestNoToolsNoTraffic_StaticZeroRuntimeNA(t *testing.T) {
	score := EvaluateTools("University", nil, goodProtocol(), RuntimeStats{}, "healthy", 0)
	// 无工具时静态三维为 0，但协议维仍可得满分；总分 = 协议贡献（权重重归一化）。
	want := 100 * baseWeightOf(DimProtocol) / (baseWeightOf(DimProtocol) + baseWeightOf(DimSchema) + baseWeightOf(DimDescription) + baseWeightOf(DimNaming))
	if !approx(score.Overall, want) {
		t.Fatalf("overall 应等于仅协议维贡献 %v，得到 %v", want, score.Overall)
	}
	byKey := map[string]DimensionReport{}
	for _, d := range score.Dimensions {
		byKey[d.Key] = d
	}
	if byKey[DimSchema].Available != true || byKey[DimSchema].Score != 0 {
		t.Fatalf("schema 维应 available=0 分，得到 %+v", byKey[DimSchema])
	}
	if byKey[DimReliability].Available || byKey[DimPerformance].Available {
		t.Fatalf("无流量时运行时两维应 n/a")
	}
	if score.Runtime.Available {
		t.Fatalf("runtime.Available 应与输入一致（无流量=false）")
	}
	if score.Runtime.Note == "" {
		t.Fatal("无流量应带 note 说明")
	}
}

// TestGoodToolNoTraffic_HighStaticScores 覆盖：优秀工具 + 无流量 → 静态维度高分，
// 可用维权重重归一化为 1、总分 100。
func TestGoodToolNoTraffic_HighStaticScores(t *testing.T) {
	score := EvaluateTools("University", []ToolSpec{goodToolSpec()}, goodProtocol(), RuntimeStats{}, "healthy", 0)
	if !approx(score.Overall, 100) {
		t.Fatalf("优秀工具应 overall≈100，得到 %v", score.Overall)
	}
	weightSum := 0.0
	for _, d := range score.Dimensions {
		if !d.Available {
			continue
		}
		if d.Score != 100 {
			t.Fatalf("维度 %s 应满分，得到 %v", d.Key, d.Score)
		}
		weightSum += d.Weight
	}
	if !approx(weightSum, 1) {
		t.Fatalf("可用维权重和应=1，得到 %v", weightSum)
	}
}

// TestRuntimeDimsIncluded 覆盖：有流量时运行时两维纳入并按权重累加。
func TestRuntimeDimsIncluded(t *testing.T) {
	rt := RuntimeStats{Available: true, Totals: 100, SuccessRate: 0.8, P95: 800, Errors: 20}
	score := EvaluateTools("University", []ToolSpec{goodToolSpec()}, goodProtocol(), rt, "healthy", 0)
	byKey := map[string]DimensionReport{}
	for _, d := range score.Dimensions {
		byKey[d.Key] = d
	}
	if !byKey[DimReliability].Available || !byKey[DimPerformance].Available {
		t.Fatal("有流量时运行时两维应可用")
	}
	if !approx(byKey[DimReliability].Score, 80) {
		t.Fatalf("可靠性应 80，得到 %v", byKey[DimReliability].Score)
	}
	if byKey[DimPerformance].Score >= 100 || byKey[DimPerformance].Score <= 0 {
		t.Fatalf("P95=800 性能分应在 (0,100)，得到 %v", byKey[DimPerformance].Score)
	}
	// 六维全可用：权重和归一。
	weightSum := 0.0
	for _, d := range score.Dimensions {
		weightSum += d.Weight
	}
	if !approx(weightSum, 1) {
		t.Fatalf("六维全可用时权重和应=1，得到 %v", weightSum)
	}
	if len(byKey[DimReliability].Findings) == 0 {
		t.Fatal("有错误应带 runtime_errors finding")
	}
}

// TestPoorToolProducesFindings 覆盖：单工具缺描述/Schema 细节时逐条 findings。
func TestPoorToolProducesFindings(t *testing.T) {
	poor := ToolSpec{
		Name:        "fetch",
		Description: "",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"missingProp"},
			"properties": map[string]any{
				"id": map[string]any{},                 // 缺 type 与 description
				"q":  map[string]any{"type": "string"}, // 缺 description
			},
		},
	}
	score := EvaluateTools("University", []ToolSpec{poor}, goodProtocol(), RuntimeStats{}, "healthy", 0)
	if len(score.ToolChecks) != 1 {
		t.Fatal("应有一条工具检查")
	}
	codes := map[string]bool{}
	for _, f := range score.ToolChecks[0].Findings {
		codes[f.Code] = true
	}
	for _, want := range []string{"missing_description", "required_undefined", "property_missing_type", "property_missing_description"} {
		if !codes[want] {
			t.Fatalf("缺少 finding %q", want)
		}
	}
}

// TestOverridesOnlyNote 覆盖：平台覆盖不改变上游分数，仅作标注。
func TestOverridesOnlyNote(t *testing.T) {
	score := EvaluateTools("University", []ToolSpec{goodToolSpec()}, goodProtocol(), RuntimeStats{}, "healthy", 2)
	if score.PlatformOverrides == nil || score.PlatformOverrides.Count != 2 {
		t.Fatalf("覆盖应仅在 note 中标注，得到 %+v", score.PlatformOverrides)
	}
	if !approx(score.Overall, 100) {
		t.Fatalf("覆盖不应影响上游分，得到 %v", score.Overall)
	}
}

// TestUnhealthyNoTraffic 覆盖：不健康但无流量 → reliability n/a（非 0 惩罚）。
func TestUnhealthyNoTraffic(t *testing.T) {
	score := EvaluateTools("University", []ToolSpec{goodToolSpec()}, goodProtocol(), RuntimeStats{}, "unhealthy", 0)
	byKey := map[string]DimensionReport{}
	for _, d := range score.Dimensions {
		byKey[d.Key] = d
	}
	if byKey[DimReliability].Available {
		t.Fatal("无流量时可靠性维不应可用")
	}
}
