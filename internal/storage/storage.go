// Package storage 定义各领域实体的持久化接口。
//
// 方法名按实体唯一（如 CreateServer / ListTools），避免多个实体合并成
// 组合接口时产生同名方法冲突；memory 与 mysql 等实现均可替换。
package storage

import (
	"context"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// ServerStore 管理 MCP Server（逻辑实体）元数据。
//
// 注意：Server 不承载 Endpoint/Transport——具体上游端点属于 InstanceStore 的
// Instance。本接口只持久化逻辑字段（含聚合 HealthStatus，见模型注释）。
type ServerStore interface {
	CreateServer(ctx context.Context, server *model.Server) error
	GetServer(ctx context.Context, id string) (*model.Server, error)
	ListServers(ctx context.Context) ([]model.Server, error)
	UpdateServer(ctx context.Context, server *model.Server) error
	DeleteServer(ctx context.Context, id string) error
}

// InstanceStore 管理 Server 下的具体上游实例。
//
// 列表按 (created_at, id) 稳定升序返回（首条即"主实例"，用于展示与默认发现
// 拨测）；DeleteServer 须级联删除其全部实例（MySQL 由 FK ON DELETE CASCADE
// 承担，memory 实现需手动删除，二者保持一致）。
type InstanceStore interface {
	CreateInstance(ctx context.Context, instance *model.Instance) error
	GetInstance(ctx context.Context, id string) (*model.Instance, error)
	ListInstances(ctx context.Context) ([]model.Instance, error)
	ListInstancesByServer(ctx context.Context, serverID string) ([]model.Instance, error)
	UpdateInstance(ctx context.Context, instance *model.Instance) error
	DeleteInstance(ctx context.Context, id string) error
	DeleteInstancesByServer(ctx context.Context, serverID string) error
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
	// GetTraffic 按流水主键读取单条调用采样（含已捕获入参，供详情/回放；不存在返回 CodeNotFound）。
	GetTraffic(ctx context.Context, id int64) (*model.TrafficSample, error)
	RecentTraffic(ctx context.Context, limit int) ([]model.TrafficSample, error)
	// RecentTrafficByServer 返回指定 Server 最近 limit 条调用采样（按写入倒序）。
	RecentTrafficByServer(ctx context.Context, serverID string, limit int) ([]model.TrafficSample, error)
}

// ---- 管理面列表查询（分页 + 关键词/字段筛选）----
//
// 方法名 Query* 与上文 List* 并存：List* 返回全量，供数据面/内部全量消费
// （MCP tools/list、健康巡检、路由热路径、改名引用迁移、上游凭证注入等）；
// Query* 面向控制台管理列表，按 query.X 过滤并分页，返回 (匹配行, 匹配总数)。
// 排序固定（见各实现与文档），不开放列排序。

// ServerQueryStore 分页查询 Server。
type ServerQueryStore interface {
	QueryServers(ctx context.Context, q query.ServerQuery) ([]model.Server, int, error)
}

// ToolQueryStore 分页查询 Tool（ServerID 过滤复用 /api/servers/{id}/tools）。
type ToolQueryStore interface {
	QueryTools(ctx context.Context, q query.ToolQuery) ([]model.Tool, int, error)
}

// RouteQueryStore 分页查询路由。
type RouteQueryStore interface {
	QueryRoutes(ctx context.Context, q query.RouteQuery) ([]model.Route, int, error)
}

// CredentialQueryStore 分页查询单 Server 的凭证元数据。
type CredentialQueryStore interface {
	QueryCredentials(ctx context.Context, q query.CredentialQuery) ([]model.Credential, int, error)
}

// AccessKeyQueryStore 分页查询 API Key。
type AccessKeyQueryStore interface {
	QueryAccessKeys(ctx context.Context, q query.AccessKeyQuery) ([]model.AccessKey, int, error)
}

// TrafficQueryStore 分页查询调用日志。
type TrafficQueryStore interface {
	QueryTraffic(ctx context.Context, q query.TrafficQuery) ([]model.TrafficSample, int, error)
}

// TrendStore 持久化分钟桶趋势：进程内指标聚合器的“已闭合分钟”由 app 定期
// 幂等 upsert 到此（PK (scope,dim_key,minute) 防重）；趋势读侧作为长程权威源。
type TrendStore interface {
	// UpsertTrendBuckets 幂等写入已闭合分钟桶（同键存在则覆盖 totals/errors）。
	UpsertTrendBuckets(ctx context.Context, buckets []model.TrendMinute) error
	// QueryTrendBuckets 按 query.TrendQuery 读取窗口内的分钟桶（minute 升序）。
	QueryTrendBuckets(ctx context.Context, q query.TrendQuery) ([]model.TrendMinute, error)
	// DeleteTrendBucketsBefore 清理 minute < before 的旧桶（保留天数收敛）。
	DeleteTrendBucketsBefore(ctx context.Context, beforeMinute int64) error
}

// RuntimeConfigStore 持久化"运行期治理配置"（IP 黑名单 + 三级限流阈值的单份快照）。
// 单行存储：exists=false 表示尚无后台保存值，应回退 config.yaml 种子。
type RuntimeConfigStore interface {
	GetRuntimeConfig(ctx context.Context) (*model.RuntimeConfig, bool, error)
	PutRuntimeConfig(ctx context.Context, cfg *model.RuntimeConfig) error
}

// Store 聚合全部实体存储接口，作为组合注入的入口。
type Store interface {
	ServerStore
	InstanceStore
	ToolStore
	RouteStore
	CredentialStore
	AccessKeyStore
	TrafficStore
	ServerQueryStore
	ToolQueryStore
	RouteQueryStore
	CredentialQueryStore
	AccessKeyQueryStore
	TrafficQueryStore
	TrendStore
	RuntimeConfigStore
}
