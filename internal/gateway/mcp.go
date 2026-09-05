package gateway

import (
	"context"
	"time"

	"github.com/xmj128/mcp-conductor/internal/access"
	"github.com/xmj128/mcp-conductor/internal/auth"
	"github.com/xmj128/mcp-conductor/internal/balancer"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/mcp"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/observability"
	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/router"
	"github.com/xmj128/mcp-conductor/internal/storage"
)

// ToolLister 是聚合 tools/list 所需的最小存储能力。
type ToolLister interface {
	ListTools(ctx context.Context) ([]model.Tool, error)
}

// ToolCaller 是工具调用能力（由 registry.ToolCaller 满足）。
type ToolCaller interface {
	Call(ctx context.Context, server model.Server, tool string, arguments map[string]any, extraHeaders map[string]string) ([]registry.CallContent, error)
}

// Authorizer 裁定主体对工具的访问权限（managed key 白名单 / 非管理回退遗留规则）。
type Authorizer interface {
	Authorize(ctx context.Context, identity *auth.Identity, tool string) error
}

// MCPGateway 实现 mcp.ToolService，作为统一 MCP 端点的后端。
//
// 职责边界：鉴权/限流由 HTTP 中间件完成；本服务负责授权（Policy）、
// 路由解析、负载均衡、上游调用与调用观测（Metrics + Audit）。
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

// NewMCPGateway 创建统一端点后端。
func NewMCPGateway(
	store storage.Store,
	resolver router.Resolver,
	balancer balancer.LoadBalancer,
	caller ToolCaller,
	authorizer Authorizer,
	metrics *observability.Metrics,
	recorder *observability.Recorder,
	opts ...Option,
) *MCPGateway {
	g := &MCPGateway{
		tools:           store,
		resolver:        resolver,
		balancer:        balancer,
		caller:          caller,
		authorizer:      authorizer,
		metrics:         metrics,
		recorder:        recorder,
		upstreamTimeout: 10 * time.Second,
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

	// 1. 授权（managed key 白名单 / 非管理回退 Policy）
	if err := g.authorizer.Authorize(ctx, identity, name); err != nil {
		return g.fail(name, start, err)
	}

	// 2. 路由解析
	resolved, err := g.resolver.Resolve(ctx, name)
	if err != nil {
		return g.fail(name, start, err)
	}

	// 3. 负载均衡
	target, err := g.balancer.Pick(ctx, resolved.Targets)
	if err != nil {
		return g.fail(name, start, errs.Wrap(errs.CodeRoute, err, "无可用上游实例"))
	}
	_ = target // MVP 单实例；多实例时 target 决定调用端点

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

	// 6. 上游调用（带超时）
	callCtx, cancel := context.WithTimeout(ctx, g.upstreamTimeout)
	defer cancel()
	contents, callErr := g.caller.Call(callCtx, resolved.Server, resolved.Tool.OriginalName, targetArgs, extraHeaders)

	// 7. 观测（无论成败）
	g.record(ctx, resolved, name, identity.Subject, start, callErr)

	if callErr != nil {
		return g.fail(name, start, callErr)
	}

	content := make([]mcp.ContentBlock, 0, len(contents))
	for _, block := range contents {
		content = append(content, mcp.ContentBlock{Type: block.Type, Text: block.Text})
	}
	return &mcp.CallToolResult{Content: content}, nil
}

// record 写入指标与调用日志；错误状态只使用统一错误码，避免暴露内部细节。
func (g *MCPGateway) record(ctx context.Context, resolved *router.Resolved, name, subject string, start time.Time, callErr error) {
	latency := time.Since(start)
	ok := callErr == nil
	g.metrics.Record(name, ok, latency)

	status := "success"
	if !ok {
		status = string(errs.CodeOf(callErr))
	}
	g.recorder.Record(ctx, model.TrafficSample{
		RequestID: RequestIDFrom(ctx),
		TraceID:   TraceIDFrom(ctx),
		ServerID:  resolved.Server.ID,
		Tool:      name,
		Client:    subject,
		Status:    status,
		LatencyMS: latency.Milliseconds(),
		Timestamp: time.Now(),
	})
}

// fail 封装调用失败为 isError 结果，并计入指标。
func (g *MCPGateway) fail(name string, start time.Time, err error) (*mcp.CallToolResult, error) {
	g.metrics.Record(name, false, time.Since(start))
	return &mcp.CallToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: err.Error()}},
		IsError: true,
	}, nil
}
