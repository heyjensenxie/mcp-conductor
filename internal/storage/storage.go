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
	GetToolBySource(ctx context.Context, serverID string, originalName string) (*model.Tool, error)
	ListTools(ctx context.Context) ([]model.Tool, error)
	ListToolsByServer(ctx context.Context, serverID string) ([]model.Tool, error)
	DeleteToolsByServer(ctx context.Context, serverID string) error
	// SetToolEnabled 切换单个工具的启用状态（Tool 是发现产物，其余字段由 discovery 拥有）。
	SetToolEnabled(ctx context.Context, id string, enabled bool) error
	// UpdateTool 更新人工维护的对外元数据；发现流程仍通过 UpsertTool 写入源定义。
	UpdateTool(ctx context.Context, tool *model.Tool) error
}

// RouteStore 管理路由定义。
type RouteStore interface {
	CreateRoute(ctx context.Context, route *model.Route) error
	GetRoute(ctx context.Context, id string) (*model.Route, error)
	ListRoutes(ctx context.Context) ([]model.Route, error)
	UpdateRoute(ctx context.Context, route *model.Route) error
	DeleteRoute(ctx context.Context, id string) error
}

// CredentialStore 管理 Gateway→Upstream 凭证元数据。
type CredentialStore interface {
	CreateCredential(ctx context.Context, credential *model.Credential) error
	ListCredentialsByServer(ctx context.Context, serverID string) ([]model.Credential, error)
	// UpdateCredential 更新凭证元数据；Name/Kind/Header 空值表示不改动。
	// Value 空值表示保留原凭证值（MySQL 侧不重写密文）；非空则替换并标记 HasValue。
	UpdateCredential(ctx context.Context, credential *model.Credential) error
	DeleteCredential(ctx context.Context, id string) error
	DeleteCredentialsByServer(ctx context.Context, serverID string) error
}

// AccessKeyStore 管理 API Key 调用方（白名单授权 + 限流配额 + 调用配置）。
type AccessKeyStore interface {
	CreateAccessKey(ctx context.Context, key *model.AccessKey) error
	GetAccessKey(ctx context.Context, id string) (*model.AccessKey, error)
	GetAccessKeyBySubject(ctx context.Context, subject string) (*model.AccessKey, error)
	GetAccessKeyByKeyHash(ctx context.Context, keyHash string) (*model.AccessKey, error)
	ListAccessKeys(ctx context.Context) ([]model.AccessKey, error)
	UpdateAccessKey(ctx context.Context, key *model.AccessKey) error
	DeleteAccessKey(ctx context.Context, id string) error
}

// TrafficStore 以追加方式记录工具调用采样（Request Log）。
type TrafficStore interface {
	AppendTraffic(ctx context.Context, sample model.TrafficSample) error
	RecentTraffic(ctx context.Context, limit int) ([]model.TrafficSample, error)
	// RecentTrafficByServer 返回指定 Server 最近 limit 条调用采样（按写入倒序）。
	RecentTrafficByServer(ctx context.Context, serverID string, limit int) ([]model.TrafficSample, error)
}

// Store 聚合全部实体存储接口，作为组合注入的入口。
type Store interface {
	ServerStore
	ToolStore
	RouteStore
	CredentialStore
	AccessKeyStore
	TrafficStore
}
