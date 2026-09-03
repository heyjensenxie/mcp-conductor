// Package storage 定义各领域实体的持久化接口。
//
// 方法名按实体唯一（如 CreateServer / ListTools），避免多个实体合并成
// 组合接口时产生同名方法冲突；memory 与 mysql 等实现均可替换。
package storage

import (
	"context"

	"github.com/xmj128/mcp-conductor/internal/model"
)

// ServerStore 管理 MCP Server 元数据。
type ServerStore interface {
	CreateServer(ctx context.Context, server *model.Server) error
	GetServer(ctx context.Context, id string) (*model.Server, error)
	ListServers(ctx context.Context) ([]model.Server, error)
	UpdateServer(ctx context.Context, server *model.Server) error
	DeleteServer(ctx context.Context, id string) error
}

// ToolStore 管理聚合后的 Tool 注册表。
type ToolStore interface {
	UpsertTool(ctx context.Context, tool *model.Tool) error
	GetTool(ctx context.Context, id string) (*model.Tool, error)
	GetToolByGatewayName(ctx context.Context, gatewayName string) (*model.Tool, error)
	ListTools(ctx context.Context) ([]model.Tool, error)
	ListToolsByServer(ctx context.Context, serverID string) ([]model.Tool, error)
	DeleteToolsByServer(ctx context.Context, serverID string) error
}

// RouteStore 管理路由定义。
type RouteStore interface {
	CreateRoute(ctx context.Context, route *model.Route) error
	ListRoutes(ctx context.Context) ([]model.Route, error)
}

// PolicyStore 管理权限策略。
type PolicyStore interface {
	CreatePolicy(ctx context.Context, policy *model.Policy) error
	GetPolicy(ctx context.Context, id string) (*model.Policy, error)
	ListPolicies(ctx context.Context) ([]model.Policy, error)
}

// CredentialStore 管理 Gateway→Upstream 凭证元数据。
type CredentialStore interface {
	CreateCredential(ctx context.Context, credential *model.Credential) error
	ListCredentialsByServer(ctx context.Context, serverID string) ([]model.Credential, error)
}

// TrafficStore 以追加方式记录工具调用采样（Request Log）。
type TrafficStore interface {
	AppendTraffic(ctx context.Context, sample model.TrafficSample) error
	RecentTraffic(ctx context.Context, limit int) ([]model.TrafficSample, error)
}

// Store 聚合全部实体存储接口，作为组合注入的入口。
type Store interface {
	ServerStore
	ToolStore
	RouteStore
	PolicyStore
	CredentialStore
	TrafficStore
}
