// Package router 负责把客户端调用的对外工具名解析为具体上游 Server 与其实例集合。
//
// Resolver 与具体 MCP Server 解耦，只依赖 Tool/Server/Route/Instance 存储的查询能力。
// 语义：默认把工具解析到其所属 Server 的实例集合；当存在启用的 Route 且其
// tool_names 命中该工具时，Route 把「调用目标 Server」覆盖为 route.server_id
// （恒等即原样；目标不可调用则 route_error，不回退原 Server）。负载均衡在
// 目标 Server 的实例维度展开（balancer 只选 healthy 的实例）。
package router

import (
	"context"
	"slices"

	"github.com/heyjensenxie/mcp-conductor/internal/balancer"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// Resolved 是一次工具解析的完整结果：对外 Tool、目标逻辑 Server 与其实例候选。
type Resolved struct {
	Tool    model.Tool
	Server  model.Server
	Targets []balancer.Target
}

// ToolLookup 是 Resolver 需要的工具查询能力。
type ToolLookup interface {
	GetToolByGatewayName(ctx context.Context, gatewayName string) (*model.Tool, error)
}

// ServerLookup 是 Resolver 需要的逻辑 Server 查询能力。
type ServerLookup interface {
	GetServer(ctx context.Context, id string) (*model.Server, error)
}

// InstanceLookup 是 Resolver 需要的实例查询能力（目标 Server 的候选端点）。
type InstanceLookup interface {
	ListInstancesByServer(ctx context.Context, serverID string) ([]model.Instance, error)
}

// RouteLookup 是 Route 参与转发所需的全部启用路由读取能力。
// storage.RouteStore 天然满足（ListRoutes）。
type RouteLookup interface {
	ListRoutes(ctx context.Context) ([]model.Route, error)
}

// Resolver 将对外工具名解析为上游调用目标。
type Resolver interface {
	Resolve(ctx context.Context, gatewayTool string) (*Resolved, error)
}

// DefaultResolver 是基于 Tool Registry 的默认解析器，可选配 Route 覆盖与实例维度。
type DefaultResolver struct {
	tools     ToolLookup
	servers   ServerLookup
	instances InstanceLookup // nil 表示未启用实例维度（解析会失败）
	routes    RouteLookup    // nil 表示不启用 Route 覆盖
}

// NewResolver 创建默认解析器（未启用 Route 覆盖与实例维度；需要时链式调用）。
func NewResolver(tools ToolLookup, servers ServerLookup) *DefaultResolver {
	return &DefaultResolver{tools: tools, servers: servers}
}

// WithRoutes 让 Resolver 在解析时参考启用 Route，命中即覆盖调用目标 Server。
func (r *DefaultResolver) WithRoutes(routes RouteLookup) *DefaultResolver {
	r.routes = routes
	return r
}

// WithInstances 让 Resolver 从实例存储构造候选端点（Server 多实例负载均衡）。
func (r *DefaultResolver) WithInstances(instances InstanceLookup) *DefaultResolver {
	r.instances = instances
	return r
}

// Resolve 校验工具可调用性，按需应用 Route 覆盖后从目标 Server 的实例集合
// 构造负载均衡候选。
//
// 状态规则：工具与目标 Server 必须启用；单实例是否入选由 IsCallable 决定
// （unknown 乐观可调，unhealthy/disabled 剔除），不再以 Server 聚合健康做整机
// 门禁——只要还有可用实例，Server 即被视为可服务。
func (r *DefaultResolver) Resolve(ctx context.Context, gatewayTool string) (*Resolved, error) {
	tool, err := r.tools.GetToolByGatewayName(ctx, gatewayTool)
	if err != nil {
		return nil, errs.Wrap(errs.CodeRoute, err, "工具 %q 未注册", gatewayTool)
	}
	if !tool.Enabled {
		return nil, errs.New(errs.CodeRoute, "工具 %q 已被禁用", gatewayTool)
	}

	// 可选：启用的 Route 命中 tool_names 时覆盖目标 Server（server_id 与工具
	// 所属相同视为恒等、无覆盖）。多个命中按 created_at/id 稳定取最早的非恒等。
	var routeName string
	targetServerID := tool.ServerID
	if r.routes != nil {
		override, lerr := r.pickRouteOverride(ctx, gatewayTool, tool.ServerID)
		if lerr != nil {
			return nil, lerr
		}
		if override != nil {
			targetServerID = override.ServerID
			routeName = override.Name
		}
	}

	server, err := r.servers.GetServer(ctx, targetServerID)
	if err != nil {
		if routeName != "" {
			return nil, errs.Wrap(errs.CodeRoute, err, "路由 %q 指向的 Server %q 不存在", routeName, targetServerID)
		}
		return nil, errs.Wrap(errs.CodeRoute, err, "工具 %q 归属的 Server 不存在", gatewayTool)
	}
	if !server.Enabled {
		return nil, unavailableMsg(*server, routeName, "已被禁用")
	}
	if r.instances == nil {
		return nil, errs.New(errs.CodeRoute, "实例查找未配置")
	}
	instances, err := r.instances.ListInstancesByServer(ctx, server.ID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeRoute, err, "读取 Server %q 实例失败", server.ID)
	}
	if len(instances) == 0 {
		return nil, unavailableMsg(*server, routeName, "没有配置实例")
	}
	targets := buildTargets(instances)
	alive := false
	for _, t := range targets {
		if t.Healthy {
			alive = true
			break
		}
	}
	if !alive {
		return nil, unavailableMsg(*server, routeName, "没有可调用的启用实例")
	}
	return &Resolved{Tool: *tool, Server: *server, Targets: targets}, nil
}

// pickRouteOverride 从启用 Route 中找出命中工具且指向其它 Server 的最早一条；
// 读失败采取 fail-closed（内聚治理读失败应暴露，而非静默回退）。无命中返回 nil。
func (r *DefaultResolver) pickRouteOverride(ctx context.Context, gatewayTool, ownerServerID string) (*model.Route, error) {
	routes, err := r.routes.ListRoutes(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取路由列表失败")
	}
	var best *model.Route
	for i := range routes {
		rt := &routes[i]
		if !rt.Enabled || !slices.Contains(rt.ToolNames, gatewayTool) || rt.ServerID == ownerServerID {
			continue
		}
		if best == nil || routeBefore(rt, best) {
			best = rt
		}
	}
	return best, nil
}

// routeBefore 按 (created_at, id) 稳定比较两条路由，用于确定命中的最早覆盖。
func routeBefore(a, b *model.Route) bool {
	if !a.CreatedAt.Equal(b.CreatedAt.Time) {
		return a.CreatedAt.Before(b.CreatedAt.Time)
	}
	return a.ID < b.ID
}

// unavailableMsg 构造 Server 不可调用错误；覆盖场景附带路由名便于定位。
func unavailableMsg(server model.Server, routeName, reason string) error {
	if routeName != "" {
		return errs.New(errs.CodeRoute, "Server %q（路由 %q 目标）%s", server.Name, routeName, reason)
	}
	return errs.New(errs.CodeRoute, "Server %q %s", server.Name, reason)
}

// buildTargets 把 Server 的实例集合映射为负载均衡候选（每实例一个 Target，
// Healthy 由实例自身启停与健康决定）。
func buildTargets(instances []model.Instance) []balancer.Target {
	targets := make([]balancer.Target, 0, len(instances))
	for _, inst := range instances {
		targets = append(targets, balancer.Target{
			ID:        inst.ID,
			ServerID:  inst.ServerID,
			Endpoint:  inst.Endpoint,
			Transport: inst.Transport,
			Args:      inst.Args,
			Weight:    1,
			Healthy:   inst.IsCallable(),
		})
	}
	return targets
}
