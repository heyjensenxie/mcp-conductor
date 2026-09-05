// Package registry 提供 MCP Server 管理、Tool 自动发现与 Tool Registry 服务。
//
// 面向客户的 Tool 名采用 Server 命名空间（如 university.search_policy），
// 解决多 Server 聚合后的 Tool Name Collision，并维护 对外名 → (Server, 原名) 映射。
package registry

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/storage"
)

// DiscoveredTool 是上游发现到的工具定义。
type DiscoveredTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolDiscoverer 连接上游 MCP Server 并发现其工具。
type ToolDiscoverer interface {
	Discover(ctx context.Context, server model.Server) ([]DiscoveredTool, error)
}

// ToolCaller 调用上游 MCP Server 的某个工具。
// extraHeaders 是按工具附加的请求头（per-tool 鉴权），可为 nil。
type ToolCaller interface {
	Call(ctx context.Context, server model.Server, tool string, arguments map[string]any, extraHeaders map[string]string) ([]CallContent, error)
}

// CallContent 是工具调用的文本结果片段（对标 MCP content 结构）。
type CallContent struct {
	Type string // text | resource
	Text string
}

// Stores 是 Service 依赖的 Server 与 Tool 存储。
type Stores interface {
	storage.ServerStore
	storage.ToolStore
	storage.CredentialStore
	storage.RouteStore
	storage.AccessKeyStore
}

var gatewayToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)

// Service 是控制面对 Server/Tool 的服务门面。
type Service struct {
	stores     Stores
	discoverer ToolDiscoverer
}

// NewService 创建 Registry 服务。
func NewService(stores Stores, discoverer ToolDiscoverer) *Service {
	return &Service{stores: stores, discoverer: discoverer}
}

// CreateServer 新增 Server 并立即触发健康检查与工具发现。
func (s *Service) CreateServer(ctx context.Context, server *model.Server) (*model.Server, error) {
	if strings.TrimSpace(server.Name) == "" {
		return nil, errs.New(errs.CodeInvalidArgument, "server.name 不能为空")
	}
	if strings.TrimSpace(server.Endpoint) == "" {
		return nil, errs.New(errs.CodeInvalidArgument, "server.endpoint 不能为空")
	}
	if server.Transport == "" {
		server.Transport = model.TransportStreamableHTTP
	}
	// 注册即启用；后续通过 toggle 操作禁用。
	server.Enabled = true
	now := time.Now().UTC()
	server.CreatedAt = now
	server.UpdatedAt = now
	server.HealthStatus = model.ServerStatusUnknown

	if err := s.stores.CreateServer(ctx, server); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "保存 Server 失败")
	}

	s.discoverServer(ctx, server.ID)
	return server, nil
}

// GetServer 读取 Server。
func (s *Service) GetServer(ctx context.Context, id string) (*model.Server, error) {
	server, err := s.stores.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	return server, nil
}

// ListServers 列出全部 Server。
func (s *Service) ListServers(ctx context.Context) ([]model.Server, error) {
	return s.stores.ListServers(ctx)
}

// ToggleServer 启用/禁用 Server；禁用时标记健康状态为 Disabled，
// 重新启用时复位为 Unknown，让健康巡检能够重新接管探活（否则会停留在
// Disabled 永远无法恢复）。
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

// DeleteServer 删除 Server 及其聚合的 Tool 与凭证（含上游注入凭据）。
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
	if err := s.stores.DeleteServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 失败")
	}
	return nil
}

// UpdateServerPatch 是 Server 可编辑字段（name 不可改：改名会重建对外工具
// 命名空间，涉及删除/重发现，v0.1 不在线支持）。
type UpdateServerPatch struct {
	Description *string `json:"description,omitempty"`
	Endpoint    *string `json:"endpoint,omitempty"`
	Transport   *string `json:"transport,omitempty"`
}

// UpdateServer 按补丁更新 Server 的可编辑字段；name/enabled/health 不受影响。
func (s *Service) UpdateServer(ctx context.Context, id string, patch UpdateServerPatch) (*model.Server, error) {
	server, err := s.stores.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	if patch.Description != nil {
		server.Description = *patch.Description
	}
	if patch.Endpoint != nil {
		if strings.TrimSpace(*patch.Endpoint) == "" {
			return nil, errs.New(errs.CodeInvalidArgument, "server.endpoint 不能为空")
		}
		server.Endpoint = *patch.Endpoint
	}
	if patch.Transport != nil {
		t := model.Transport(strings.TrimSpace(*patch.Transport))
		if t == "" {
			t = model.TransportStreamableHTTP
		}
		if t != model.TransportStreamableHTTP && t != model.TransportSSE && t != model.TransportStdio {
			return nil, errs.New(errs.CodeInvalidArgument, "transport 仅支持 https / sse / stdio")
		}
		server.Transport = t
	}
	server.UpdatedAt = time.Now().UTC()
	if err := s.stores.UpdateServer(ctx, server); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新 Server 失败")
	}
	return server, nil
}

// Rediscover 重新发现指定 Server 的工具（测试连接/刷新 Registry 用）。
func (s *Service) Rediscover(ctx context.Context, serverID string) error {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	return s.discover(ctx, *server)
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

// discoverServer 后台执行一次工具发现，失败仅记录（不阻塞写操作）。
func (s *Service) discoverServer(ctx context.Context, serverID string) {
	server, err := s.stores.GetServer(ctx, serverID)
	if err != nil {
		return
	}
	_ = s.discover(ctx, *server)
}

// discover 发现并落库指定 Server 的工具；同一对外名重复时覆盖。
func (s *Service) discover(ctx context.Context, server model.Server) error {
	namespace := namespaceFor(server.Name)
	tools, err := s.discoverer.Discover(ctx, server)
	if err != nil {
		return errs.Wrap(errs.CodeUpstream, err, "发现 Server %q 工具失败", server.Name)
	}
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
