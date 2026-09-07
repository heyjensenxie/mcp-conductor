package eval

import (
	"context"
	"slices"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/mcpclient"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// defaultUpstreamTimeout 是回归用例默认单 case 超时（与网关默认一致）。
const defaultUpstreamTimeout = 10 * time.Second

// Store 是评测服务需要的存储能力（由 storage.Store 满足）。
type Store interface {
	GetServer(ctx context.Context, id string) (*model.Server, error)
	ListInstancesByServer(ctx context.Context, serverID string) ([]model.Instance, error)
	ListToolsByServer(ctx context.Context, serverID string) ([]model.Tool, error)
}

// Prober 是评测探测能力（由 mcpclient.Adapter.Probe 满足）。
type Prober interface {
	Probe(ctx context.Context, server model.Server, instance model.Instance) (*mcpclient.ProbeResult, error)
}

// RuntimeReader 返回指定 Server 的运行时观测；无流量时 available=false。
type RuntimeReader func(serverID string) (RuntimeStats, bool)

// Service 编排评测：Meta / 质量评测 / 回归用例。全部即时计算、不落库。
type Service struct {
	store   Store
	prober  Prober
	caller  ToolCaller
	runtime RuntimeReader
}

// NewService 创建评测服务。
func NewService(store Store, prober Prober, caller ToolCaller, runtime RuntimeReader) *Service {
	return &Service{store: store, prober: prober, caller: caller, runtime: runtime}
}

// Meta 是评测前的 Server 概况（拨测实例、工具数、是否有流量、覆盖数）。
type Meta struct {
	Server                  ServerMeta    `json:"server"`
	Probe                   *InstanceMeta `json:"probe"`
	StoredToolCount         int           `json:"stored_tool_count"`
	RuntimeMetricsAvailable bool          `json:"runtime_metrics_available"`
	PlatformOverrideCount   int           `json:"platform_override_count"`
	Issues                  []Finding     `json:"issues,omitempty"`
}

// Describe 返回指定 Server 的评测概况。
// instanceID 为空时拨测首个可拨测实例（无则仅给 warn issue）；非空时必须属于
// 该 Server 且可拨测，否则返回明确错误（供定向评测前端提示）。
func (s *Service) Describe(ctx context.Context, id, instanceID string) (*Meta, error) {
	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	var inst *model.Instance
	var issues []Finding
	if instanceID == "" {
		inst, err = s.pickTarget(ctx, id, "")
		if err != nil {
			issues = append(issues, Finding{Severity: SeverityWarn, Code: "no_callable_instance",
				Message: "该 Server 当前没有可拨测的启用实例", Suggestion: "新增实例或启用/恢复某个实例后即可评测"})
		}
	} else {
		inst, err = s.pickTarget(ctx, id, instanceID)
		if err != nil {
			return nil, err
		}
	}
	var probe *InstanceMeta
	if inst != nil {
		probe = &InstanceMeta{ID: inst.ID, Endpoint: inst.Endpoint, Transport: string(inst.Transport), HealthStatus: string(inst.HealthStatus)}
	}
	tools, err := s.store.ListToolsByServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取工具失败")
	}
	overrides := 0
	for _, tool := range tools {
		if tool.NameOverridden || tool.DescriptionOverridden || tool.InputSchemaOverridden {
			overrides++
		}
	}
	_, rtAvailable := s.runtime(id)
	return &Meta{
		Server:                  ServerMeta{ID: server.ID, Name: server.Name, HealthStatus: string(server.HealthStatus)},
		Probe:                   probe,
		StoredToolCount:         len(tools),
		RuntimeMetricsAvailable: rtAvailable,
		PlatformOverrideCount:   overrides,
		Issues:                  issues,
	}, nil
}

// RunQuality 对指定 Server 现场拨测并输出质量报告；instanceID 为空取首个可拨测
// 实例。runtime 统计块仍按整个 Server 的流量取（观测为 Server 级），定向实例只
// 决定本次拨测对象。
func (s *Service) RunQuality(ctx context.Context, id, instanceID string) (*Report, error) {
	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	instance, err := s.pickTarget(ctx, server.ID, instanceID)
	if err != nil {
		return nil, err
	}
	probe, err := s.prober.Probe(ctx, *server, *instance)
	if err != nil {
		return nil, err // mcpclient 已归类 upstream/timeout
	}

	tools := make([]ToolSpec, 0, len(probe.Tools))
	for _, t := range probe.Tools {
		tools = append(tools, ToolSpec{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	stored, err := s.store.ListToolsByServer(ctx, server.ID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取工具失败")
	}
	overrideCount := 0
	gatewayByOriginal := make(map[string]string, len(stored))
	for _, tool := range stored {
		gatewayByOriginal[tool.OriginalName] = tool.GatewayName
		if tool.NameOverridden || tool.DescriptionOverridden || tool.InputSchemaOverridden {
			overrideCount++
		}
	}

	rt, _ := s.runtime(server.ID)
	protocol := ProtocolInfo{
		NegotiatedVersion: probe.ProtocolVersion,
		RecognizedVersion: slices.Contains(mcp.SupportedProtocolVersions, probe.ProtocolVersion),
		ServerName:        probe.ServerInfo.Name,
		ServerVersion:     probe.ServerInfo.Version,
		ToolsCapability:   probe.Capabilities.Tools != nil,
	}
	score := EvaluateTools(server.Name, tools, protocol, rt, string(server.HealthStatus), overrideCount)

	// 回填网关名（用于报告对照上游名）。
	for i := range score.ToolChecks {
		score.ToolChecks[i].GatewayName = gatewayByOriginal[score.ToolChecks[i].Tool]
	}

	return &Report{
		Server: ServerMeta{
			ID:           server.ID,
			Name:         server.Name,
			HealthStatus: string(server.HealthStatus),
			ProbedInstance: &InstanceMeta{
				ID: instance.ID, Endpoint: instance.Endpoint,
				Transport: string(instance.Transport), HealthStatus: string(instance.HealthStatus),
			},
		},
		OverallScore:      score.Overall,
		Dimensions:        score.Dimensions,
		ToolChecks:        score.ToolChecks,
		Runtime:           &score.Runtime,
		PlatformOverrides: score.PlatformOverrides,
		GeneratedAt:       model.Now(),
	}, nil
}

// RunSuite 顺序执行一组回归用例（直连上游）；instanceID 为空取首个可拨测实例。
func (s *Service) RunSuite(ctx context.Context, id, instanceID string, cases []SuiteCase) (*SuiteResult, error) {
	if len(cases) == 0 {
		return nil, errs.New(errs.CodeInvalidArgument, "cases 不能为空")
	}
	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	instance, err := s.pickTarget(ctx, server.ID, instanceID)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.ListToolsByServer(ctx, server.ID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取工具失败")
	}
	toolMap := make(map[string]string, len(stored))
	for _, tool := range stored {
		toolMap[tool.GatewayName] = tool.OriginalName
	}
	results := runSuite(ctx, s.caller, *server, *instance, toolMap, cases, defaultUpstreamTimeout)
	out := summarizeSuite(*server, results)
	return &out, nil
}

// pickTarget 取该 Server 的评测/回归目标实例。
// instanceID 为空：返回第一个可拨测实例（无则 CodeRoute 错误）。
// instanceID 非空：必须是该 Server 的实例且 IsCallable()（启用且非 unhealthy），
// 否则返回明确 4xx（不属于 → CodeNotFound；不可拨测 → CodeInvalidArgument）。
func (s *Service) pickTarget(ctx context.Context, serverID, instanceID string) (*model.Instance, error) {
	instances, err := s.store.ListInstancesByServer(ctx, serverID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取实例失败")
	}
	if instanceID == "" {
		return pickInstance(instances)
	}
	for i := range instances {
		if instances[i].ID != instanceID {
			continue
		}
		if !instances[i].IsCallable() {
			return nil, errs.New(errs.CodeInvalidArgument, "实例 %q 不可拨测（未启用或 unhealthy），请选择其它实例", instanceID)
		}
		return &instances[i], nil
	}
	return nil, errs.New(errs.CodeNotFound, "实例 %q 不属于该 Server", instanceID)
}

// pickInstance 返回第一个启用且未被探测为 unhealthy 的实例。
func pickInstance(instances []model.Instance) (*model.Instance, error) {
	for i := range instances {
		if instances[i].IsCallable() {
			return &instances[i], nil
		}
	}
	return nil, errs.New(errs.CodeRoute, "没有可拨测的启用实例")
}
