// Package registry 提供 MCP Server 管理、实例管理、Tool 自动发现与 Tool Registry 服务。
//
// 面向客户的 Tool 名采用 Server 命名空间（如 university.search_policy），
// 解决多 Server 聚合后的 Tool Name Collision，并维护 对外名 → (Server, 原名) 映射。
//
// 模型：Server 是逻辑实体（聚合工具/凭证/命名空间），其上可挂多个实例
// （endpoint+transport）；工具发现/健康探测作用于某个具体实例，负载均衡在
// 实例维度展开。Server 级 HealthStatus 由实例集合聚合推导（见模型注释）。
package registry

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// DiscoveredTool 是上游发现到的工具定义。
type DiscoveredTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolDiscoverer 连接上游某个实例并发现其工具。
type ToolDiscoverer interface {
	Discover(ctx context.Context, server model.Server, instance model.Instance) ([]DiscoveredTool, error)
}

// ToolCaller 调用上游某个实例的某个工具。
// extraHeaders 是按工具附加的请求头（per-tool 鉴权），可为 nil。
type ToolCaller interface {
	Call(ctx context.Context, server model.Server, instance model.Instance, tool string, arguments map[string]any, extraHeaders map[string]string) ([]CallContent, error)
}

// InstanceProber 对单个实例执行一次健康探测（initialize 握手），
// 用于"实例测试"即时回写；健康周期巡检由 health.Monitor 负责。
type InstanceProber interface {
	Check(ctx context.Context, server model.Server, instance model.Instance) (model.ServerStatus, error)
}

// CallContent 是工具调用的文本结果片段（对标 MCP content 结构）。
type CallContent struct {
	Type string // text | resource
	Text string
}

// Stores 是 Service 依赖的存储能力。
type Stores interface {
	storage.ServerStore
	storage.InstanceStore
	storage.ToolStore
	storage.CredentialStore
	storage.RouteStore
	storage.AccessKeyStore
}

var gatewayToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)

// Service 是控制面对 Server/实例/Tool 的服务门面。
type Service struct {
	stores     Stores
	discoverer ToolDiscoverer
	prober     InstanceProber // nil 表示"实例测试"不可用（未装配）
}

// NewService 创建 Registry 服务。
func NewService(stores Stores, discoverer ToolDiscoverer) *Service {
	return &Service{stores: stores, discoverer: discoverer}
}

// WithProber 装配实例健康探针（通常为同一个 mcpclient.Adapter）。
func (s *Service) WithProber(prober InstanceProber) *Service {
	s.prober = prober
	return s
}

// CreateServerInput 是新增 Server 的入参；Endpoint/Transport/Args 属于首个
// （seed）实例。Endpoint 语义随传输而异：https/sse 为端点 URL，stdio 为可执行
// 命令；Args 仅 stdio 使用（启动参数，不经过 shell）。
type CreateServerInput struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Endpoint    string          `json:"endpoint"`
	Transport   model.Transport `json:"transport"`
	Args        []string        `json:"args,omitempty"`
}

// CreateServer 新增逻辑 Server 及其 seed 实例，并立即触发工具发现。
//
// 非原子：先建 Server 行再建实例行，实例失败时 best-effort 回滚删除 Server。
func (s *Service) CreateServer(ctx context.Context, in CreateServerInput) (*model.Server, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errs.New(errs.CodeInvalidArgument, "server.name 不能为空")
	}
	endpoint := strings.TrimSpace(in.Endpoint)
	if endpoint == "" {
		return nil, errs.New(errs.CodeInvalidArgument, "server.endpoint 不能为空")
	}
	transport, err := normalizeTransport(in.Transport)
	if err != nil {
		return nil, err
	}
	if err := validateArgsForTransport(transport, endpoint, in.Args); err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	// 注册即启用；后续通过 toggle 操作禁用。
	server := &model.Server{
		Name:         name,
		Description:  strings.TrimSpace(in.Description),
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.stores.CreateServer(ctx, server); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "保存 Server 失败")
	}
	seed := &model.Instance{
		ServerID:     server.ID,
		Endpoint:     endpoint,
		Transport:    transport,
		Args:         in.Args,
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.stores.CreateInstance(ctx, seed); err != nil {
		// 回滚已创建的 Server 行，避免孤儿 Server；再失败仅记录。
		if delErr := s.stores.DeleteServer(ctx, server.ID); delErr != nil {
			return nil, errs.Wrap(errs.CodeInternal, delErr, "创建实例失败且回滚 Server 失败")
		}
		return nil, errs.Wrap(errs.CodeInternal, err, "保存 seed 实例失败")
	}
	s.discoverServer(ctx, server.ID)
	return server, nil
}

// GetServer 读取逻辑 Server（不含实例列表；由控制面水合）。
func (s *Service) GetServer(ctx context.Context, id string) (*model.Server, error) {
	server, err := s.stores.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	return server, nil
}

// ListServers 列出全部逻辑 Server。
func (s *Service) ListServers(ctx context.Context) ([]model.Server, error) {
	return s.stores.ListServers(ctx)
}

// ListInstances 列出指定 Server 的实例（按 (created_at, id) 升序）。
func (s *Service) ListInstances(ctx context.Context, serverID string) ([]model.Instance, error) {
	if _, err := s.stores.GetServer(ctx, serverID); err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	instances, err := s.stores.ListInstancesByServer(ctx, serverID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取实例失败")
	}
	return instances, nil
}

// ToggleServer 启用/禁用逻辑 Server；禁用时聚合状态标为 Disabled，
// 重新启用时复位为 Unknown，让健康巡检重新接管探活。
func (s *Service) ToggleServer(ctx context.Context, id string, enabled bool) (*model.Server, error) {
	server, err := s.stores.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	server.Enabled = enabled
	server.UpdatedAt = time.Now().UTC()
	if !enabled {
		server.HealthStatus = model.ServerStatusDisabled
	} else if server.HealthStatus == model.ServerStatusDisabled {
		server.HealthStatus = model.ServerStatusUnknown
	}
	if err := s.stores.UpdateServer(ctx, server); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新 Server 失败")
	}
	return server, nil
}

// DeleteServer 删除逻辑 Server 及其实例、聚合的 Tool 与凭证。
func (s *Service) DeleteServer(ctx context.Context, id string) error {
	if _, err := s.stores.GetServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	if err := s.stores.DeleteToolsByServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 工具失败")
	}
	if err := s.stores.DeleteCredentialsByServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 凭证失败")
	}
	if err := s.stores.DeleteInstancesByServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 实例失败")
	}
	if err := s.stores.DeleteServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 失败")
	}
	return nil
}

// UpdateServerPatch 是逻辑 Server 可编辑字段（name 不可改：改名会重建对外工具
// 命名空间，涉及删除/重发现，v0.1 不在线支持；endpoint/transport 属于实例，
// 请编辑实例）。
type UpdateServerPatch struct {
	Description *string `json:"description,omitempty"`
}

// UpdateServer 按补丁更新逻辑 Server 的可编辑字段；name/enabled/health 不受影响。
func (s *Service) UpdateServer(ctx context.Context, id string, patch UpdateServerPatch) (*model.Server, error) {
	server, err := s.stores.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	if patch.Description != nil {
		server.Description = strings.TrimSpace(*patch.Description)
	}
	server.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateServer(ctx, server); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新 Server 失败")
	}
	return server, nil
}

// AddInstance 为 Server 新增一个实例（启用、健康 unknown）并重算聚合。
func (s *Service) AddInstance(ctx context.Context, serverID string, endpoint string, transport model.Transport, args []string) (*model.Instance, error) {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errs.New(errs.CodeInvalidArgument, "instance.endpoint 不能为空")
	}
	transport, err = normalizeTransport(transport)
	if err != nil {
		return nil, err
	}
	if err := validateArgsForTransport(transport, endpoint, args); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	instance := &model.Instance{
		ServerID:     server.ID,
		Endpoint:     endpoint,
		Transport:    transport,
		Args:         args,
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.stores.CreateInstance(ctx, instance); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "保存实例失败")
	}
	if err := s.syncAggregate(ctx, serverID); err != nil {
		return nil, err
	}
	return instance, nil
}

// UpdateInstancePatch 是实例可编辑字段；Endpoint/Transport/Args 变更会使该实例
// 健康复位为 unknown 并交由巡检重新探活。
type UpdateInstancePatch struct {
	Endpoint  *string   `json:"endpoint,omitempty"`
	Transport *string   `json:"transport,omitempty"`
	Args      *[]string `json:"args,omitempty"`
}

// UpdateInstance 更新实例可编辑字段并重算 Server 聚合。
func (s *Service) UpdateInstance(ctx context.Context, serverID, instanceID string, patch UpdateInstancePatch) (*model.Instance, error) {
	instance, err := s.getServerInstance(ctx, serverID, instanceID)
	if err != nil {
		return nil, err
	}
	changed := false
	if patch.Endpoint != nil {
		endpoint := strings.TrimSpace(*patch.Endpoint)
		if endpoint == "" {
			return nil, errs.New(errs.CodeInvalidArgument, "instance.endpoint 不能为空")
		}
		if instance.Endpoint != endpoint {
			instance.Endpoint, changed = endpoint, true
		}
	}
	if patch.Transport != nil {
		transport, tErr := normalizeTransport(model.Transport(strings.TrimSpace(*patch.Transport)))
		if tErr != nil {
			return nil, tErr
		}
		if instance.Transport != transport {
			instance.Transport, changed = transport, true
		}
	}
	if patch.Args != nil {
		// 参数非 nil 即视为提交（空数组 = 清空参数）；与端点/传输一起校验最终组合。
		instance.Args = *patch.Args
		changed = true
	}
	if !changed {
		return instance, nil
	}
	// 组合校验（stdio 必须有命令、非 stdio 不允许参数）在落库前拦截非法配置。
	if err := validateArgsForTransport(instance.Transport, instance.Endpoint, instance.Args); err != nil {
		return nil, err
	}
	// 端点/传输变更后旧健康结论失效，复位待探。
	instance.HealthStatus = model.ServerStatusUnknown
	instance.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateInstance(ctx, instance); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新实例失败")
	}
	if err := s.syncAggregate(ctx, serverID); err != nil {
		return nil, err
	}
	return instance, nil
}

// ToggleInstance 启用/禁用实例并重算聚合；禁用即摘除（不进探测与负载均衡）。
func (s *Service) ToggleInstance(ctx context.Context, serverID, instanceID string, enabled bool) (*model.Instance, error) {
	instance, err := s.getServerInstance(ctx, serverID, instanceID)
	if err != nil {
		return nil, err
	}
	if instance.Enabled == enabled {
		return instance, nil
	}
	instance.Enabled = enabled
	instance.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateInstance(ctx, instance); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新实例失败")
	}
	if err := s.syncAggregate(ctx, serverID); err != nil {
		return nil, err
	}
	return instance, nil
}

// DeleteInstance 删除单个实例；Server 必须至少保留一个实例。
func (s *Service) DeleteInstance(ctx context.Context, serverID, instanceID string) error {
	instance, err := s.getServerInstance(ctx, serverID, instanceID)
	if err != nil {
		return err
	}
	instances, err := s.stores.ListInstancesByServer(ctx, serverID)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取实例失败")
	}
	if len(instances) <= 1 {
		return errs.New(errs.CodeInvalidArgument, "Server 至少保留一个实例（如需下线请删除整个 Server）")
	}
	if err := s.stores.DeleteInstance(ctx, instance.ID); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除实例失败")
	}
	if err := s.syncAggregate(ctx, serverID); err != nil {
		return err
	}
	return nil
}

// TestInstance 对单个实例执行一次同步健康探测（initialize 握手）并回写健康
// 与聚合；用于"实例测试"。失败时实例标记 unhealthy 并返回错误。
func (s *Service) TestInstance(ctx context.Context, serverID, instanceID string) (*model.Instance, error) {
	if s.prober == nil {
		return nil, errs.New(errs.CodeInternal, "实例探针未装配")
	}
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	instance, err := s.getServerInstance(ctx, serverID, instanceID)
	if err != nil {
		return nil, err
	}
	status, probeErr := s.prober.Check(ctx, *server, *instance)
	instance.HealthStatus = status
	if probeErr != nil {
		instance.HealthStatus = model.ServerStatusUnhealthy
	}
	instance.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateInstance(ctx, instance); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新实例失败")
	}
	if err := s.syncAggregate(ctx, serverID); err != nil {
		return nil, err
	}
	if probeErr != nil {
		return instance, errs.Wrap(errs.CodeUpstream, probeErr, "实例 %q 探测失败", instance.Endpoint)
	}
	return instance, nil
}

// Rediscover 重新发现逻辑 Server 的工具（测试连接/刷新 Registry 用），
// 拨测其主（首个可用）实例；成功后把该实例标 healthy、失败标 unhealthy。
func (s *Service) Rediscover(ctx context.Context, serverID string) error {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	return s.discover(ctx, *server)
}

// RediscoverChange 描述一次重新发现预演中对单个工具的变更（advisory）。
// Kind 取 add（上游新增）或 update（已有工具将有字段变化）。update 下：
// DescChanged / SchemaChanged 表示"对外门面"（未被覆盖保护）字段会被更新；
// SourceChanged 表示上游源元数据与最近一次登记不同（覆盖保护下仅源值刷新）；
// *Protected 表示对应门面当前有平台覆盖，实际不会被动覆盖。
type RediscoverChange struct {
	Kind            string `json:"kind"` // add | update
	OriginalName    string `json:"original_name"`
	GatewayName     string `json:"gateway_name"`
	Enabled         bool   `json:"enabled"`
	DescChanged     bool   `json:"desc_changed"`
	SchemaChanged   bool   `json:"schema_changed"`
	SourceChanged   bool   `json:"source_changed"`
	DescProtected   bool   `json:"desc_protected"`
	SchemaProtected bool   `json:"schema_protected"`
	NameProtected   bool   `json:"name_protected"`
}

// RediscoverPlan 是一次"重新发现"的只读预演结果：调用方据此人工确认后再真正落库。
type RediscoverPlan struct {
	ServerID string             `json:"server_id"`
	Added    int                `json:"added"`
	Updated  int                `json:"updated"`
	Changes  []RediscoverChange `json:"changes"`
}

// PlanRediscover 连接主实例执行一次上游发现并计算相对当前登记的变更，但不写库、
// 不改实例健康状态。变更判断与 discover() 的合并语义一致：门面字段仅在未覆盖时
// 才会被更新，覆盖字段始终保留当前值。
func (s *Service) PlanRediscover(ctx context.Context, serverID string) (*RediscoverPlan, error) {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	instance, err := s.pickDiscoveryInstance(ctx, *server)
	if err != nil {
		return nil, err
	}
	namespace := namespaceFor(server.Name)
	upstream, err := s.discoverer.Discover(ctx, *server, *instance)
	if err != nil {
		return nil, errs.Wrap(errs.CodeUpstream, err, "发现 Server %q 工具失败", server.Name)
	}

	// Changes 显式初始化为空切片（而非 nil），保证序列化输出 [] 而不是 null，
	// 避免前端对 plan.changes.length 读 null 报错。
	plan := &RediscoverPlan{ServerID: server.ID, Changes: []RediscoverChange{}}
	for _, dt := range upstream {
		gw := namespace + "." + dt.Name
		existing, foundErr := s.stores.GetToolBySource(ctx, server.ID, dt.Name)
		if foundErr != nil {
			// 未登记过的上游工具 → 新增（默认启用，与 discover 一致）。
			plan.Added++
			plan.Changes = append(plan.Changes, RediscoverChange{
				Kind: "add", OriginalName: dt.Name, GatewayName: gw, Enabled: true,
			})
			continue
		}
		sourceChanged := existing.SourceDescription != dt.Description ||
			!reflect.DeepEqual(existing.SourceInputSchema, dt.InputSchema)
		descChanged := !existing.DescriptionOverridden && existing.Description != dt.Description
		schemaChanged := !existing.InputSchemaOverridden &&
			!reflect.DeepEqual(existing.InputSchema, dt.InputSchema)
		if !sourceChanged && !descChanged && !schemaChanged {
			continue
		}
		plan.Updated++
		plan.Changes = append(plan.Changes, RediscoverChange{
			Kind: "update", OriginalName: dt.Name, GatewayName: existing.GatewayName,
			Enabled:     existing.Enabled,
			DescChanged: descChanged, SchemaChanged: schemaChanged, SourceChanged: sourceChanged,
			DescProtected:   existing.DescriptionOverridden,
			SchemaProtected: existing.InputSchemaOverridden,
			NameProtected:   existing.NameOverridden,
		})
	}
	return plan, nil
}

// ListTools 列出全部聚合后的工具。
func (s *Service) ListTools(ctx context.Context) ([]model.Tool, error) {
	return s.stores.ListTools(ctx)
}

// ListServerTools 列出指定 Server 的工具。
func (s *Service) ListServerTools(ctx context.Context, serverID string) ([]model.Tool, error) {
	return s.stores.ListToolsByServer(ctx, serverID)
}

// ToggleTool 启用/禁用单个工具（Tool 的其余字段由发现过程拥有，运维只翻 enabled）。
func (s *Service) ToggleTool(ctx context.Context, id string, enabled bool) (*model.Tool, error) {
	tool, err := s.stores.GetTool(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Tool 失败")
	}
	tool.Enabled = enabled
	tool.UpdatedAt = time.Now().UTC()
	if err := s.stores.SetToolEnabled(ctx, id, enabled); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新 Tool 失败")
	}
	return tool, nil
}

// UpdateToolPatch 描述控制台可覆盖的工具门面字段。Reset* 会恢复最近一次
// 上游发现值，并重新允许后续发现刷新该字段。
type UpdateToolPatch struct {
	GatewayName      *string         `json:"gateway_name,omitempty"`
	Description      *string         `json:"description,omitempty"`
	InputSchema      *map[string]any `json:"input_schema,omitempty"`
	ResetName        bool            `json:"reset_name,omitempty"`
	ResetDescription bool            `json:"reset_description,omitempty"`
	ResetInputSchema bool            `json:"reset_input_schema,omitempty"`
}

// UpdateTool 更新对外工具名称、描述和参数 Schema。对外名变化时同步精确的
// Key grant 与 Route 引用；通配规则保持不变。
func (s *Service) UpdateTool(ctx context.Context, id string, patch UpdateToolPatch) (*model.Tool, error) {
	tool, err := s.stores.GetTool(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Tool 失败")
	}
	oldName := tool.GatewayName
	canonicalName := namespaceForServerTool(tool.ServerID, tool.OriginalName, s.stores, ctx)
	if patch.ResetName {
		tool.GatewayName, tool.NameOverridden = canonicalName, false
	} else if patch.GatewayName != nil {
		name := strings.TrimSpace(*patch.GatewayName)
		if !gatewayToolNamePattern.MatchString(name) {
			return nil, errs.New(errs.CodeInvalidArgument, "gateway_name 仅支持字母、数字、点、下划线和连字符，且不能以符号开头")
		}
		if other, lookupErr := s.stores.GetToolByGatewayName(ctx, name); lookupErr == nil && other.ID != tool.ID {
			return nil, errs.New(errs.CodeInvalidArgument, "gateway_name %q 已被其他工具使用", name)
		}
		tool.GatewayName, tool.NameOverridden = name, name != canonicalName
	}
	if patch.ResetDescription {
		tool.Description, tool.DescriptionOverridden = tool.SourceDescription, false
	} else if patch.Description != nil {
		tool.Description = *patch.Description
		tool.DescriptionOverridden = tool.Description != tool.SourceDescription
	}
	if patch.ResetInputSchema {
		tool.InputSchema, tool.InputSchemaOverridden = tool.SourceInputSchema, false
	} else if patch.InputSchema != nil {
		tool.InputSchema = *patch.InputSchema
		tool.InputSchemaOverridden = !reflect.DeepEqual(tool.InputSchema, tool.SourceInputSchema)
	}
	tool.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateTool(ctx, tool); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新 Tool 失败")
	}
	if oldName != tool.GatewayName {
		if err := s.renameReferences(ctx, oldName, tool.GatewayName); err != nil {
			return nil, err
		}
	}
	return tool, nil
}

func namespaceForServerTool(serverID, originalName string, stores Stores, ctx context.Context) string {
	server, err := stores.GetServer(ctx, serverID)
	if err != nil {
		return originalName
	}
	return namespaceFor(server.Name) + "." + originalName
}

func (s *Service) renameReferences(ctx context.Context, oldName, newName string) error {
	keys, err := s.stores.ListAccessKeys(ctx)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取 API Key 引用失败")
	}
	for i := range keys {
		changed := false
		for j := range keys[i].Grants {
			if keys[i].Grants[j].GatewayName == oldName {
				keys[i].Grants[j].GatewayName, changed = newName, true
			}
		}
		if changed {
			if err := s.stores.UpdateAccessKey(ctx, &keys[i]); err != nil {
				return errs.Wrap(errs.CodeInternal, err, "同步 API Key 工具引用失败")
			}
		}
	}
	routes, err := s.stores.ListRoutes(ctx)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取 Route 引用失败")
	}
	for i := range routes {
		changed := false
		for j := range routes[i].ToolNames {
			if routes[i].ToolNames[j] == oldName {
				routes[i].ToolNames[j], changed = newName, true
			}
		}
		if changed {
			if err := s.stores.UpdateRoute(ctx, &routes[i]); err != nil {
				return errs.Wrap(errs.CodeInternal, err, "同步 Route 工具引用失败")
			}
		}
	}
	return nil
}

// ---- 内部辅助 ----

// normalizeTransport 空值默认 streamable HTTP 并校验枚举白名单。
func normalizeTransport(t model.Transport) (model.Transport, error) {
	if t == "" {
		t = model.TransportStreamableHTTP
	}
	switch t {
	case model.TransportStreamableHTTP, model.TransportSSE, model.TransportStdio:
		return t, nil
	default:
		return t, errs.New(errs.CodeInvalidArgument, "transport 仅支持 https / sse / stdio")
	}
}

// validateArgsForTransport 校验实例传输与 endpoint/args 的组合：stdio 的 endpoint
// 承载可执行命令（必填）并可带 args；https/sse 不允许 args（拦截配置错误）。
func validateArgsForTransport(transport model.Transport, endpoint string, args []string) error {
	if transport == model.TransportStdio {
		if strings.TrimSpace(endpoint) == "" {
			return errs.New(errs.CodeInvalidArgument, "stdio 实例必须提供启动命令（endpoint 字段）")
		}
		return nil
	}
	if len(args) > 0 {
		return errs.New(errs.CodeInvalidArgument, "args 仅 stdio 传输支持（https/sse 使用 endpoint URL）")
	}
	return nil
}

// getServerInstance 校验实例归属后返回实例副本。
func (s *Service) getServerInstance(ctx context.Context, serverID, instanceID string) (*model.Instance, error) {
	instance, err := s.stores.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取实例失败")
	}
	if instance.ServerID != serverID {
		return nil, errs.New(errs.CodeNotFound, "实例 %q 不属于 Server %q", instanceID, serverID)
	}
	return instance, nil
}

// pickDiscoveryInstance 返回首个"启用且非 unhealthy"的实例用于发现拨测；
// 全不可用时返回错误（调用方不应回退到禁用实例）。
func (s *Service) pickDiscoveryInstance(ctx context.Context, server model.Server) (*model.Instance, error) {
	instances, err := s.stores.ListInstancesByServer(ctx, server.ID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取实例失败")
	}
	for i := range instances {
		if instances[i].IsCallable() {
			return &instances[i], nil
		}
	}
	return nil, errs.New(errs.CodeRoute, "Server %q 没有可用的启用实例用于发现", server.Name)
}

// syncAggregate 读取实例集合、推导聚合健康并写回 Server（仅在变化时更新）。
func (s *Service) syncAggregate(ctx context.Context, serverID string) error {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	instances, err := s.stores.ListInstancesByServer(ctx, serverID)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取实例失败")
	}
	agg := model.AggregateServerHealth(server.Enabled, instances)
	if agg == server.HealthStatus {
		return nil
	}
	server.HealthStatus = agg
	server.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateServer(ctx, server); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "更新 Server 聚合状态失败")
	}
	return nil
}

// markInstanceHealth 直写某个实例的健康并重算聚合（发现成功/失败即反馈健康）。
func (s *Service) markInstanceHealth(ctx context.Context, instanceID string, status model.ServerStatus) {
	instance, err := s.stores.GetInstance(ctx, instanceID)
	if err != nil {
		return
	}
	if instance.HealthStatus == status {
		return
	}
	instance.HealthStatus = status
	instance.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateInstance(ctx, instance); err != nil {
		return
	}
	_ = s.syncAggregate(ctx, instance.ServerID)
}

// discoverServer 后台执行一次工具发现（写路径调用），失败仅记录。
func (s *Service) discoverServer(ctx context.Context, serverID string) {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return
	}
	if !server.Enabled {
		return
	}
	_ = s.discover(ctx, *server)
}

// discover 拨测 Server 的主实例发现并落库工具；成功后把该实例标 healthy、
// 失败标 unhealthy 并重算聚合。同一对外名重复时覆盖（保留运维启停/覆盖）。
func (s *Service) discover(ctx context.Context, server model.Server) error {
	instance, err := s.pickDiscoveryInstance(ctx, server)
	if err != nil {
		return err
	}
	namespace := namespaceFor(server.Name)
	tools, err := s.discoverer.Discover(ctx, server, *instance)
	if err != nil {
		s.markInstanceHealth(ctx, instance.ID, model.ServerStatusUnhealthy)
		return errs.Wrap(errs.CodeUpstream, err, "发现 Server %q 工具失败", server.Name)
	}
	s.markInstanceHealth(ctx, instance.ID, model.ServerStatusHealthy)

	now := time.Now().UTC()
	for _, dt := range tools {
		gw := namespace + "." + dt.Name
		// 保留既有启停状态：重新发现（rediscover / 注册 / test）不应清掉
		// 运维手工禁用的工具；仅新工具默认启用。
		enabled := true
		if existing, err := s.stores.GetToolBySource(ctx, server.ID, dt.Name); err == nil {
			enabled = existing.Enabled
		}
		tool := &model.Tool{
			ServerID:          server.ID,
			OriginalName:      dt.Name,
			GatewayName:       gw,
			Description:       dt.Description,
			InputSchema:       dt.InputSchema,
			SourceDescription: dt.Description,
			SourceInputSchema: dt.InputSchema,
			Enabled:           enabled,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := s.stores.UpsertTool(ctx, tool); err != nil {
			return errs.Wrap(errs.CodeInternal, err, "保存工具 %q 失败", dt.Name)
		}
	}
	return nil
}

// namespaceFor 由 Server 名称派生对外工具命名空间（小写、特殊字符转下划线）。
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
	ns := strings.Trim(b.String(), "_")
	if ns == "" {
		return "server"
	}
	return ns
}
