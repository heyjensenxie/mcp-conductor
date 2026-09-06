package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/access"
	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/balancer"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/router"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// ToolLister 是聚合 tools/list 所需的最小存储能力。
type ToolLister interface {
	ListTools(ctx context.Context) ([]model.Tool, error)
}

// ToolCaller 是工具调用能力（由 registry.ToolCaller 满足）。
// server 是逻辑 Server（凭证按 Server 组装），instance 是本次拨测的具体实例。
type ToolCaller interface {
	Call(ctx context.Context, server model.Server, instance model.Instance, tool string, arguments map[string]any, extraHeaders map[string]string) ([]registry.CallContent, error)
}

// Authorizer 裁定主体对工具的访问权限（managed key 白名单 / 非管理回退遗留规则）。
type Authorizer interface {
	Authorize(ctx context.Context, identity *auth.Identity, tool string) error
}

// MCPGateway 实现 mcp.ToolService，作为统一 MCP 端点的后端。
//
// 职责边界：鉴权/限流由 HTTP 中间件完成；本服务负责授权（managed key
// 白名单 / Operator 放行）、路由解析、负载均衡、上游调用与调用观测
// （Metrics + Audit）。
type MCPGateway struct {
	tools           ToolLister
	resolver        router.Resolver
	balancer        balancer.LoadBalancer
	caller          ToolCaller
	authorizer      Authorizer
	metrics         *observability.Metrics
	recorder        *observability.Recorder
	upstreamTimeout time.Duration
	sem             chan struct{} // 并发上限；nil 表示不限
	// instanceTracker 让网关把实例级调用失败回报给负载均衡器（短期冷却）；
	// 均衡器未实现 InstanceTracker 时为 nil（无失败反馈）。
	instanceTracker balancer.InstanceTracker
	// instanceProbe 在实例调用失败时触发对所属 Server 的快速健康探活
	// （由 app 注入 Monitor.TriggerCheck；nil 表示不触发，等周期巡检兜底）。
	instanceProbe func(serverID string)
}

// Option 是 MCPGateway 构造选项。
type Option func(*MCPGateway)

// WithUpstreamTimeout 配置上游调用超时。
func WithUpstreamTimeout(d time.Duration) Option {
	return func(g *MCPGateway) { g.upstreamTimeout = d }
}

// WithMaxConcurrency 配置并发上限（<=0 表示不限制）。
func WithMaxConcurrency(n int) Option {
	return func(g *MCPGateway) {
		if n > 0 {
			g.sem = make(chan struct{}, n)
		}
	}
}

// WithInstanceProbe 配置实例调用失败后触发对所属 Server 的快速健康探活回调。
// 回调应为非阻塞触发（如健康巡检的 TriggerCheck），周期巡检仍作为兜底。
func WithInstanceProbe(fn func(serverID string)) Option {
	return func(g *MCPGateway) {
		if fn != nil {
			g.instanceProbe = fn
		}
	}
}

// NewMCPGateway 创建统一端点后端。
func NewMCPGateway(
	store storage.Store,
	resolver router.Resolver,
	lb balancer.LoadBalancer,
	caller ToolCaller,
	authorizer Authorizer,
	metrics *observability.Metrics,
	recorder *observability.Recorder,
	opts ...Option,
) *MCPGateway {
	g := &MCPGateway{
		tools:           store,
		resolver:        resolver,
		balancer:        lb,
		caller:          caller,
		authorizer:      authorizer,
		metrics:         metrics,
		recorder:        recorder,
		upstreamTimeout: 10 * time.Second,
	}
	// 均衡器实现了 InstanceTracker（RoundRobin 具备失败冷却）才启用失败反馈。
	if tracker, ok := lb.(balancer.InstanceTracker); ok {
		g.instanceTracker = tracker
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

// ListTools 聚合返回当前主体可见的工具，作为 tools/list 响应。
// 被管理的 API Key 只下发白名单内工具；其余主体返回全部已启用工具。
func (g *MCPGateway) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	tools, err := g.tools.ListTools(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取工具注册表失败")
	}
	identity := IdentityFrom(ctx)
	if identity.Key != nil {
		tools = access.FilterTools(identity, tools)
	}
	out := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if !tool.Enabled {
			continue
		}
		out = append(out, mcp.Tool{
			Name:        tool.GatewayName,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return out, nil
}

// CallTool 是 tools/call 的完整处理：授权 → 路由 → 均衡 → 调用配置 →
// 上游调用 → 观测。业务失败统一返回 isError 结果，仅在参数层面才返回 Go error。
func (g *MCPGateway) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	start := time.Now()
	identity := IdentityFrom(ctx)

	// 1. 授权（managed key 白名单 / Operator 放行）
	if err := g.authorizer.Authorize(ctx, identity, name); err != nil {
		return g.fail(name, start, err)
	}

	// 2. 路由解析
	resolved, err := g.resolver.Resolve(ctx, name)
	if err != nil {
		return g.fail(name, start, err)
	}

	// 3. 负载均衡（从健康实例中选一个做本次拨测目标）
	target, err := g.balancer.Pick(ctx, resolved.Targets)
	if err != nil {
		return g.fail(name, start, errs.Wrap(errs.CodeRoute, err, "无可用上游实例"))
	}
	dialInstance := model.Instance{
		ID:        target.ID,
		ServerID:  target.ServerID,
		Endpoint:  target.Endpoint,
		Transport: target.Transport,
	}

	// 4. 并发上限（可选）
	if g.sem != nil {
		select {
		case g.sem <- struct{}{}:
			defer func() { <-g.sem }()
		default:
			return g.fail(name, start, errs.New(errs.CodeRateLimit, "网关并发已满，请稍后重试"))
		}
	}

	// 5. 调用配置（key×tool）：合并固定参数、附加该工具的请求头（鉴权）
	targetArgs := arguments
	var extraHeaders map[string]string
	if identity.Key != nil {
		if grant := identity.Key.GrantFor(name); grant != nil {
			targetArgs = model.MergeArguments(grant.DefaultArgs, arguments)
			extraHeaders = grant.Headers
		}
	}

	// 6. 上游调用（带超时；实际拨测被均衡选中的实例）
	callCtx, cancel := context.WithTimeout(ctx, g.upstreamTimeout)
	defer cancel()
	contents, callErr := g.caller.Call(callCtx, resolved.Server, dialInstance, resolved.Tool.OriginalName, targetArgs, extraHeaders)

	// 7. 观测（唯一观测点：成功/失败各记一次指标与调用日志）。
	// 入参捕获：传真正发出的合并参数（授权 default_args 合并后），record_args
	// 开启时才由 Recorder 落库；响应永不记录。
	g.record(ctx, resolved, name, identity.Subject, dialInstance.ID, start, targetArgs, callErr)

	if callErr != nil {
		// 失败反馈：传输/实例级故障（非工具 isError 业务失败）才触发短期冷却
		// 与快速探活，避免把一次业务失败误判成实例宕机而摘除。
		g.reportInstanceFailure(resolved.Server.ID, dialInstance.ID, callErr)
		return g.failResult(callErr)
	}

	content := make([]mcp.ContentBlock, 0, len(contents))
	for _, block := range contents {
		content = append(content, mcp.ContentBlock{Type: block.Type, Text: block.Text})
	}
	return &mcp.CallToolResult{Content: content}, nil
}

// record 是 tools/call 的唯一后置观测点：按工具、所属 Server 与其实际命中的
// 实例三个维度记录指标，并写入调用日志；错误状态只使用统一错误码与脱敏摘要，
// 避免暴露内部细节或完整敏感体。成功与失败各只记一次（调用方不得再经其它路径
// 重复计数）。instanceID 为本次实际拨测的实例；Server 单实例场景也会记录，
// instance: 维仅用于观测下沉，不影响既有 server:/tool 维语义。
func (g *MCPGateway) record(ctx context.Context, resolved *router.Resolved, name, subject, instanceID string, start time.Time, reqArgs map[string]any, callErr error) {
	latency := time.Since(start)
	ok := callErr == nil
	g.metrics.Record(name, ok, latency)
	serverID := ""
	if resolved != nil {
		serverID = resolved.Server.ID
		if serverID != "" {
			g.metrics.Record(observability.ServerDimPrefix+serverID, ok, latency)
			if instanceID != "" {
				g.metrics.Record(observability.InstanceDimPrefix+serverID+":"+instanceID, ok, latency)
			}
		}
	}

	status := "success"
	if !ok {
		status = string(errs.CodeOf(callErr))
	}
	sample := model.TrafficSample{
		RequestID:  RequestIDFrom(ctx),
		TraceID:    TraceIDFrom(ctx),
		ServerID:   serverID,
		InstanceID: instanceID,
		Tool:       name,
		Client:     subject,
		Status:     status,
		LatencyMS:  latency.Milliseconds(),
		Timestamp:  time.Now(),
	}
	if !ok {
		sample.Error = errs.SafeMessage(callErr)
	}
	// 只捕获非空入参（record_args 开启时 Recorder 才保留；空参无可回放）。
	if len(reqArgs) > 0 {
		sample.RequestArgs = reqArgs
	}
	g.recorder.Record(ctx, sample)
}

// fail 封装"目标尚未解析"阶段的失败为 isError 结果并计入指标（此时无法
// 写按 Server 维度的调用日志）。目标解析后的失败由 record + failResult 处理。
func (g *MCPGateway) fail(name string, start time.Time, err error) (*mcp.CallToolResult, error) {
	g.metrics.Record(name, false, time.Since(start))
	return g.failResult(err)
}

// failResult 把错误组装为 isError 结果返回，不重复计数：上游失败场景的
// 指标与调用日志已由 record 统一记录。
func (g *MCPGateway) failResult(err error) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: err.Error()}},
		IsError: true,
	}, nil
}

// reportInstanceFailure 在一次真实拨测到实例的调用失败后回报均衡器并触发快速
// 探活。仅传输/实例级故障（非工具 isError 业务失败）参与，避免业务失败误摘。
func (g *MCPGateway) reportInstanceFailure(serverID, instanceID string, callErr error) {
	if isToolFailure(callErr) {
		return
	}
	if g.instanceTracker != nil {
		g.instanceTracker.ReportFailure(instanceID)
	}
	if g.instanceProbe != nil {
		g.instanceProbe(serverID)
	}
}

// isToolFailure 判定错误是否为上游连通但工具执行失败（MCP isError 结果）。
// 这种业务性失败不表示实例故障，不应触发实例级冷却。
func isToolFailure(err error) bool {
	if err == nil {
		return false
	}
	var toolFail *registry.ToolFailedError
	return errors.As(err, &toolFail)
}
