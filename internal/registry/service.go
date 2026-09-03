// Package registry 提供 MCP Server 管理、Tool 自动发现与 Tool Registry 服务。
//
// 面向客户的 Tool 名采用 Server 命名空间（如 university.search_policy），
// 解决多 Server 聚合后的 Tool Name Collision，并维护 对外名 → (Server, 原名) 映射。
package registry

import (
	"context"
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
type ToolCaller interface {
	Call(ctx context.Context, server model.Server, tool string, arguments map[string]any) ([]CallContent, error)
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
}

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

// ToggleServer 启用/禁用 Server；禁用时同步标记健康状态。
func (s *Service) ToggleServer(ctx context.Context, id string, enabled bool) (*model.Server, error) {
	server, err := s.stores.GetServer(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	server.Enabled = enabled
	server.UpdatedAt = time.Now().UTC()
	if !enabled {
		server.HealthStatus = model.ServerStatusDisabled
	}
	if err := s.stores.UpdateServer(ctx, server); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "更新 Server 失败")
	}
	return server, nil
}

// DeleteServer 删除 Server 及其聚合的 Tool。
func (s *Service) DeleteServer(ctx context.Context, id string) error {
	if _, err := s.stores.GetServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	if err := s.stores.DeleteToolsByServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 工具失败")
	}
	if err := s.stores.DeleteServer(ctx, id); err != nil {
		return errs.Wrap(errs.CodeInternal, err, "删除 Server 失败")
	}
	return nil
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
		tool := &model.Tool{
			ServerID:     server.ID,
			OriginalName: dt.Name,
			GatewayName:  namespace + "." + dt.Name,
			Description:  dt.Description,
			InputSchema:  dt.InputSchema,
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
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
