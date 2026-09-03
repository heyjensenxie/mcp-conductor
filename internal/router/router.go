// Package router 负责把客户端调用的对外工具名解析为具体上游 Server 与实例。
//
// Resolver 与具体 MCP Server 解耦，只依赖 Tool/Server 存储的查询能力，
// 未来可通过 Route 定义扩展分组、灰度等更复杂的解析逻辑。
package router

import (
	"context"

	"github.com/xmj128/mcp-conductor/internal/balancer"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
)

// Resolved 是一次工具解析的完整结果：对外 Tool、所属 Server 与可选实例。
type Resolved struct {
	Tool    model.Tool
	Server  model.Server
	Targets []balancer.Target
}

// ToolLookup 是 Resolver 需要的工具查询能力。
type ToolLookup interface {
	GetToolByGatewayName(ctx context.Context, gatewayName string) (*model.Tool, error)
}

// ServerLookup 是 Resolver 需要的 Server 查询能力。
type ServerLookup interface {
	GetServer(ctx context.Context, id string) (*model.Server, error)
}

// Resolver 将对外工具名解析为上游调用目标。
type Resolver interface {
	Resolve(ctx context.Context, gatewayTool string) (*Resolved, error)
}

// DefaultResolver 是基于 Tool Registry 的默认解析器。
type DefaultResolver struct {
	tools   ToolLookup
	servers ServerLookup
}

// NewResolver 创建默认解析器。
func NewResolver(tools ToolLookup, servers ServerLookup) *DefaultResolver {
	return &DefaultResolver{tools: tools, servers: servers}
}

// Resolve 校验工具与 Server 的可调用性，并构造实例候选列表。
//
// 状态规则：Server 处于 UNKNOWN（尚未健康检查）或 HEALTHY 时允许调用；
// UNHEALTHY / DISABLED 一律拒绝。
func (r *DefaultResolver) Resolve(ctx context.Context, gatewayTool string) (*Resolved, error) {
	tool, err := r.tools.GetToolByGatewayName(ctx, gatewayTool)
	if err != nil {
		return nil, errs.Wrap(errs.CodeRoute, err, "工具 %q 未注册", gatewayTool)
	}
	if !tool.Enabled {
		return nil, errs.New(errs.CodeRoute, "工具 %q 已被禁用", gatewayTool)
	}

	server, err := r.servers.GetServer(ctx, tool.ServerID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeRoute, err, "工具 %q 归属的 Server 不存在", gatewayTool)
	}
	if !isCallable(server.HealthStatus) {
		return nil, errs.New(errs.CodeRoute, "Server %q 当前不可用（%s）", server.Name, server.HealthStatus)
	}
	if !server.Enabled {
		return nil, errs.New(errs.CodeRoute, "Server %q 已被禁用", server.Name)
	}

	targets := []balancer.Target{
		{
			ID:        server.ID,
			ServerID:  server.ID,
			Endpoint:  server.Endpoint,
			Transport: server.Transport,
			Weight:    1,
			Healthy:   isCallable(server.HealthStatus),
		},
	}
	return &Resolved{Tool: *tool, Server: *server, Targets: targets}, nil
}

// isCallable 报告某健康状态是否允许发起调用。
func isCallable(status model.ServerStatus) bool {
	return status != model.ServerStatusUnhealthy && status != model.ServerStatusDisabled
}
