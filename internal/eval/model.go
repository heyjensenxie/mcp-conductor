// Package eval 提供 MCP 评测（Evaluation）：Server 质量评分 + 回归用例执行。
//
// 本阶段无 LLM / 无 Python / 不落库（即时计算）：质量评测现场拨测一个逻辑
// Server 的可拨测实例，拿到真实协议握手结果与最新上游工具定义后，对工具做
// 静态质量检查（Schema/描述/命名），并叠加该 Server 的运行时指标（成功率/
// P95/错误率）与健康聚合，输出分维度 MCP Score 与可执行的改进建议。
package eval

import "time"

// 严重级别与维度 key 常量。
const (
	SeverityError = "error"
	SeverityWarn  = "warn"
	SeverityInfo  = "info"

	DimProtocol    = "protocol_compat"
	DimSchema      = "schema_quality"
	DimDescription = "tool_description"
	DimNaming      = "tool_naming"
	DimReliability = "reliability"
	DimPerformance = "performance"
)

// dimWeight 描述一个评分维度的 key 与其基础权重。
type dimWeight struct {
	Key  string
	Base float64
}

// dimensionOrder 决定各维度在报告中的稳定顺序；Base 权重和 = 1.0。运行时两维
// 仅在对应 Server 有流量时才纳入总分（可用维度按基础权重和重新归一化）。
var dimensionOrder = []dimWeight{
	{DimProtocol, 0.15},
	{DimSchema, 0.25},
	{DimDescription, 0.20},
	{DimNaming, 0.10},
	{DimReliability, 0.20},
	{DimPerformance, 0.10},
}

// baseWeightOf 返回某维度的基础权重（未在 dimensionOrder 中则 0）。
func baseWeightOf(key string) float64 {
	for _, dw := range dimensionOrder {
		if dw.Key == key {
			return dw.Base
		}
	}
	return 0
}

// Finding 是一条静态/协议/运行时结论与改进建议。
type Finding struct {
	Severity   string `json:"severity"` // error | warn | info
	Code       string `json:"code"`     // missing_description / no_properties ...
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ToolScore 是单个工具在三个静态维度的得分（0-100）。
type ToolScore struct {
	Schema      float64 `json:"schema"`
	Description float64 `json:"description"`
	Naming      float64 `json:"naming"`
}

// ToolCheck 是单个上游工具的质量检查结果（Tool 为上游原名）。
type ToolCheck struct {
	Tool        string    `json:"tool"`
	GatewayName string    `json:"gateway_name,omitempty"`
	Scores      ToolScore `json:"scores"`
	Findings    []Finding `json:"findings,omitempty"`
}

// DimensionReport 是一个评分维度的小结。
type DimensionReport struct {
	Key                  string    `json:"key"`
	Score                float64   `json:"score"` // 0-100
	Available            bool      `json:"available"`
	Weight               float64   `json:"weight"` // 归一化后的有效权重
	WeightedContribution float64   `json:"weighted_contribution"`
	Findings             []Finding `json:"findings,omitempty"`
}

// RuntimeInfo 是运行时观测在报告里的呈现。
type RuntimeInfo struct {
	Available   bool    `json:"available"`
	Totals      int64   `json:"totals"`
	SuccessRate float64 `json:"success_rate"` // 0..1
	P95         float64 `json:"p95"`          // ms
	Errors      int64   `json:"errors"`
	Note        string  `json:"note,omitempty"`
}

// InstanceMeta 是报告中拨测实例的简要信息。
type InstanceMeta struct {
	ID           string `json:"id"`
	Endpoint     string `json:"endpoint"`
	Transport    string `json:"transport"`
	HealthStatus string `json:"health_status"`
}

// ServerMeta 是报告开头标识被评测 Server。
type ServerMeta struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	HealthStatus   string        `json:"health_status"`
	ProbedInstance *InstanceMeta `json:"probed_instance,omitempty"`
}

// OverrideNote 提示平台对该 Server 工具做过多少人工覆盖（覆盖本身是网关侧
// 治理，不影响对上游质量的评分，但提示当前上游定义可能不佳、正被人工兜底）。
type OverrideNote struct {
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// Report 是一次质量评测的完整输出（即时计算，不落库）。
type Report struct {
	Server            ServerMeta        `json:"server"`
	OverallScore      float64           `json:"overall_score"`
	Dimensions        []DimensionReport `json:"dimensions"`
	ToolChecks        []ToolCheck       `json:"tool_checks"`
	Runtime           *RuntimeInfo      `json:"runtime,omitempty"`
	PlatformOverrides *OverrideNote     `json:"platform_overrides,omitempty"`
	GeneratedAt       time.Time         `json:"generated_at"`
}
