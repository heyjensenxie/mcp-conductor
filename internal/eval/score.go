package eval

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ToolSpec 是评测所需的工具定义（上游原名 + 描述 + JSON Schema）。
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ProtocolInfo 是一次 initialize 握手的关键回显（由 Service 从 ProbeResult 折叠）。
type ProtocolInfo struct {
	NegotiatedVersion string
	RecognizedVersion bool
	ServerName        string
	ServerVersion     string
	ToolsCapability   bool
}

// RuntimeStats 是该 Server 的运行时观测（由进程内指标快照折叠）。
type RuntimeStats struct {
	Available   bool // 该 Server 是否已有调用流量
	Totals      int64
	SuccessRate float64 // 0..1
	P95         float64 // ms
	Errors      int64
}

// Score 是纯评分结果（EvaluateTools 的返回值），供 Service 组装 Report。
type Score struct {
	Overall           float64
	Dimensions        []DimensionReport
	ToolChecks        []ToolCheck
	Runtime           RuntimeInfo
	PlatformOverrides *OverrideNote
}

// EvaluateTools 对上游工具集合 + 协议 + 运行时做纯静态评分，无任何 I/O。
//
// 规则：
//   - 维度权重见 dimensionOrder；运行时两维仅在 rt.Available 时可用，可用维度
//     按基础权重和重新归一化（不可用维度 weight=0、available=false）；
//   - len(tools)==0 时静态三维 score=0 并各带 error finding，避免空手拿高分；
//   - 评分使用"最新上游定义"（tools/list 直读），平台覆盖数量仅作报告标注，
//     不影响上游分数。
func EvaluateTools(serverName string, tools []ToolSpec, p ProtocolInfo, rt RuntimeStats, health string, overrides int) *Score {
	namespacePrefix := namespaceFor(serverName)

	checks := make([]ToolCheck, 0, len(tools))
	// 各静态维度累计（用于取均值）。
	var schemaSum, descSum, namingSum float64
	for _, spec := range tools {
		check := scoreTool(spec, namespacePrefix)
		checks = append(checks, check)
		schemaSum += check.Scores.Schema
		descSum += check.Scores.Description
		namingSum += check.Scores.Naming
	}

	// 静态三维：均值；无工具时为 0 并带 error finding。
	var schemaDim, descDim, namingDim float64
	noTools := len(tools) == 0
	if noTools {
		schemaDim, descDim, namingDim = 0, 0, 0
	} else {
		schemaDim = schemaSum / float64(len(tools))
		descDim = descSum / float64(len(tools))
		namingDim = namingSum / float64(len(tools))
	}

	protocolScore, protocolFindings := scoreProtocol(p)
	reliabilityScore, reliabilityFindings := scoreReliability(rt, health)
	perfScore, perfFindings := scorePerformance(rt)

	dims := []DimensionReport{
		{DimProtocol, protocolScore, true, 0, 0, protocolFindings},
		{DimSchema, schemaDim, true, 0, 0, zeroToolsFindings(noTools, DimSchema)},
		{DimDescription, descDim, true, 0, 0, zeroToolsFindings(noTools, DimDescription)},
		{DimNaming, namingDim, true, 0, 0, zeroToolsFindings(noTools, DimNaming)},
		{DimReliability, reliabilityScore, rt.Available, 0, 0, reliabilityFindings},
		{DimPerformance, perfScore, rt.Available, 0, 0, perfFindings},
	}

	// 重归一化权重。
	availableBases := 0.0
	for i := range dims {
		if dims[i].Available {
			availableBases += baseWeightOf(dims[i].Key)
		}
	}
	overall := 0.0
	for i := range dims {
		d := &dims[i]
		if !d.Available || availableBases <= 0 {
			continue
		}
		d.Weight = baseWeightOf(d.Key) / availableBases
		d.WeightedContribution = d.Weight * d.Score
		overall += d.WeightedContribution
	}

	runtime := RuntimeInfo{
		Available:   rt.Available,
		Totals:      rt.Totals,
		SuccessRate: rt.SuccessRate,
		P95:         rt.P95,
		Errors:      rt.Errors,
	}
	if !rt.Available {
		runtime.Note = "该 Server 尚无调用流量，运行时维度（可靠性/性能）未纳入总分"
	}

	var ov *OverrideNote
	if overrides > 0 {
		ov = &OverrideNote{
			Count: overrides,
			Message: "平台对该 Server 工具做过人工覆盖（不影响上游质量分），" +
				"提示上游部分定义可能不佳、正由网关侧兜底",
		}
	}

	return &Score{
		Overall:           overall,
		Dimensions:        dims,
		ToolChecks:        checks,
		Runtime:           runtime,
		PlatformOverrides: ov,
	}
}

// zeroToolsFindings 在无工具时给静态维度附加一条 error finding。
func zeroToolsFindings(noTools bool, dim string) []Finding {
	if !noTools {
		return nil
	}
	return []Finding{{
		Severity:   SeverityError,
		Code:       "no_tools",
		Message:    "评测未发现任何上游工具（tools/list 为空或拨测实例不可用）",
		Suggestion: "确认该 Server 已正常返回 tools/list，或检查拨测实例是否可达",
	}}
}

// scoreTool 计算单个工具的 schema/description/naming 得分与 findings。
func scoreTool(spec ToolSpec, nsPrefix string) ToolCheck {
	schema, schemaF := scoreSchema(spec.InputSchema)
	desc, descF := scoreDescription(spec.Description)
	naming, namingF := scoreNaming(spec.Name, nsPrefix)

	findings := make([]Finding, 0, len(schemaF)+len(descF)+len(namingF))
	findings = append(findings, schemaF...)
	findings = append(findings, descF...)
	findings = append(findings, namingF...)
	return ToolCheck{
		Tool: spec.Name,
		Scores: ToolScore{
			Schema:      schema,
			Description: desc,
			Naming:      naming,
		},
		Findings: findings,
	}
}

// scoreSchema 对 JSON Schema 打分（底 100 逐项扣分）。
func scoreSchema(schema map[string]any) (float64, []Finding) {
	var f []Finding
	if len(schema) == 0 {
		return 40, []Finding{{Severity: SeverityError, Code: "missing_schema",
			Message: "工具缺少输入 Schema", Suggestion: "为工具补充 JSON Schema（type=object + properties）"}}
	}
	typ, _ := schema["type"].(string)
	if typ != "" && typ != "object" {
		f = append(f, Finding{Severity: SeverityWarn, Code: "non_object_schema",
			Message: "输入 Schema 顶层 type 应为 object", Suggestion: "将顶层 type 设为 object 并在 properties 中声明参数"})
	}
	propsRaw, _ := schema["properties"].(map[string]any)
	score := 100.0
	if len(propsRaw) == 0 {
		score -= 40
		f = append(f, Finding{Severity: SeverityWarn, Code: "no_properties",
			Message:    "Schema 未声明 properties（无参只读工具可忽略）",
			Suggestion: "若工具需要参数请在 properties 中声明；确为只读无参可忽略此提醒"})
		return max0(score), f
	}

	// required 引用校验。
	undefined := 0
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			name, _ := r.(string)
			if _, ok := propsRaw[name]; !ok {
				undefined++
			}
		}
	}
	if undefined > 0 {
		score -= math.Min(float64(undefined), 2) * 10
		f = append(f, Finding{Severity: SeverityWarn, Code: "required_undefined",
			Message: "required 引用了未在 properties 中声明的属性", Suggestion: "修正 required 与 properties 的一致性"})
	}

	// 属性级 type / description 覆盖度。
	missingType, missingDesc := 0, 0
	propNames := make([]string, 0, len(propsRaw))
	for k := range propsRaw {
		propNames = append(propNames, k)
	}
	sort.Strings(propNames)
	for _, k := range propNames {
		prop, _ := propsRaw[k].(map[string]any)
		if len(prop) == 0 {
			missingType++
			continue
		}
		if _, ok := prop["type"].(string); !ok {
			missingType++
		}
		if _, ok := prop["description"].(string); !ok {
			missingDesc++
		}
	}
	score -= math.Min(float64(missingType), 6) * 5
	if missingType > 0 {
		f = append(f, Finding{Severity: SeverityWarn, Code: "property_missing_type",
			Message: "部分参数未声明 type，模型无法校验与正确补参", Suggestion: "为每个参数补充 type（string/number/integer/boolean/array/object）"})
	}
	score -= math.Min(float64(missingDesc), 7.5) * 2
	if missingDesc > 0 {
		f = append(f, Finding{Severity: SeverityWarn, Code: "property_missing_description",
			Message: "部分参数缺少 description，模型难以准确生成参数值", Suggestion: "为每个参数补充一句话业务语义描述"})
	}
	return max0(score), f
}

// scoreDescription 按 rune 长度给工具描述打分。
func scoreDescription(desc string) (float64, []Finding) {
	n := utf8.RuneCountInString(desc)
	if n == 0 {
		return 0, []Finding{{Severity: SeverityError, Code: "missing_description",
			Message: "工具缺少描述", Suggestion: "用一句话说明工具做什么、何时使用"}}
	}
	if n <= 20 {
		return 40, []Finding{{Severity: SeverityWarn, Code: "description_too_short",
			Message: "工具描述过短，可能不足以让模型判断调用时机", Suggestion: "补充输入约束、返回值与使用场景"}}
	}
	if n <= 80 {
		return 80, []Finding{{Severity: SeverityInfo, Code: "description_ok",
			Message: "描述基本可用，可进一步补充边界条件与示例"}}
	}
	return 100, nil
}

// scoreNaming 检查上游工具命名（不应内嵌 gateway 命名空间/含点号）。
func scoreNaming(name, nsPrefix string) (float64, []Finding) {
	if name == "" {
		return 0, []Finding{{Severity: SeverityError, Code: "empty_name", Message: "工具名为空"}}
	}
	score := 100.0
	var f []Finding
	redundant := strings.Contains(name, ".") ||
		(nsPrefix != "" && strings.HasPrefix(strings.ToLower(name), nsPrefix+"_"))
	if redundant {
		score -= 25
		f = append(f, Finding{Severity: SeverityWarn, Code: "redundant_namespace",
			Message: "工具名含命名空间风格的前缀/点号，网关会再次前缀命名空间", Suggestion: "上游工具名保持简短无前缀，命名空间交由网关层添加"})
	}
	if utf8.RuneCountInString(name) > 60 {
		score -= 20
		f = append(f, Finding{Severity: SeverityWarn, Code: "name_too_long",
			Message: "工具名过长，不利于模型选择", Suggestion: "精简工具名"})
	}
	if !validToolRunes(name) {
		score -= 30
		f = append(f, Finding{Severity: SeverityWarn, Code: "invalid_name",
			Message: "工具名含非法字符（仅允许字母/数字/点/下划线/连字符）"})
	}
	return max0(score), f
}

// scoreProtocol 依据 initialize 回显评估协议兼容性。
func scoreProtocol(p ProtocolInfo) (float64, []Finding) {
	score := 0.0
	var f []Finding
	switch {
	case p.RecognizedVersion:
		score += 40
	case p.NegotiatedVersion != "":
		score += 15
		f = append(f, Finding{Severity: SeverityWarn, Code: "unsupported_protocol",
			Message:    "上游协商出的协议版本不在本网关实现清单内：" + p.NegotiatedVersion,
			Suggestion: "升级上游 SDK 到本网关支持的协议版本（见 SupportedProtocolVersions）"})
	default:
		f = append(f, Finding{Severity: SeverityInfo, Code: "missing_protocol",
			Message: "initialize 未回显协议版本"})
	}
	if p.ServerName != "" && p.ServerVersion != "" {
		score += 30
	} else if p.ServerName != "" {
		score += 15
		f = append(f, Finding{Severity: SeverityInfo, Code: "missing_server_version",
			Message: "上游未回显 serverInfo.version"})
	} else {
		f = append(f, Finding{Severity: SeverityInfo, Code: "missing_server_info",
			Message: "上游未回显 serverInfo"})
	}
	if p.ToolsCapability {
		score += 30
	} else {
		f = append(f, Finding{Severity: SeverityError, Code: "no_tools_capability",
			Message: "上游未在 capabilities 中声明 tools 能力", Suggestion: "服务端应声明 capabilities.tools 以被聚合"})
	}
	return max0(score), f
}

// scoreReliability 由成功率与健康推导可靠性分。
func scoreReliability(rt RuntimeStats, health string) (float64, []Finding) {
	if !rt.Available {
		return 0, nil // 维度不可用由调用方标记，这里不产生误导 finding
	}
	var f []Finding
	score := rt.SuccessRate * 100
	if rt.Errors > 0 {
		f = append(f, Finding{Severity: SeverityWarn, Code: "runtime_errors",
			Message: "观测到调用错误，成功率 " + pct(rt.SuccessRate)})
	}
	if health == "unhealthy" && rt.SuccessRate < 1 {
		f = append(f, Finding{Severity: SeverityError, Code: "health_success_mismatch",
			Message: "健康状态为 unhealthy 且存在失败调用，建议排查实例可用性"})
	}
	return max0(score), f
}

// scorePerformance 由 p95 延迟分段映射性能分。
func scorePerformance(rt RuntimeStats) (float64, []Finding) {
	if !rt.Available {
		return 0, nil
	}
	p95 := rt.P95
	var score float64
	switch {
	case p95 <= 200:
		score = 100
	case p95 <= 500:
		score = 100 - (p95-200)/(300)*(100-60)
	case p95 <= 1000:
		score = 60 - (p95-500)/(500)*(60-35)
	case p95 <= 3000:
		score = 35 - (p95-1000)/(2000)*(35-10)
	default:
		score = math.Max(0, 10-(p95-3000)/2000*10)
	}
	var f []Finding
	if p95 > 500 {
		f = append(f, Finding{Severity: SeverityWarn, Code: "slow_p95",
			Message:    "P95 延迟偏慢（" + ms(p95) + "），建议排查上游处理与网络",
			Suggestion: "关注慢查询/大结果集，必要时为上游扩容"})
	}
	return max0(score), f
}

// namespaceFor 由 Server 名派生小写命名空间（与 registry 的 namespace 规则一致），
// 供命名冗余启发式判断。
func namespaceFor(name string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('_')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// validToolRunes 判定名字仅含允许字符。
func validToolRunes(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// max0 截断到非负。
func max0(v float64) float64 { return math.Max(0, v) }

// pct 0..1 转百分比文本。
func pct(v float64) string { return formatFloat(v*100) + "%" }

// ms 格式化毫秒数（去掉多余小数位）。
func ms(v float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(formatFloat(v), "0"), ".") + " ms"
}

func formatFloat(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}
