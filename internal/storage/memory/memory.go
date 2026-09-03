// Package memory 提供基于进程内 map 的存储实现。
//
// 作为默认后端，保证没有任何外部依赖（MySQL/Redis）时项目也能启动
// 并跑通最小闭环。数据不持久化，进程重启即丢失，仅用于开发模式。
package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/xmj128/mcp-conductor/internal/model"
)

// Store 是基于进程内 map 的并发安全存储，实现 storage.Store 全部接口。
type Store struct {
	mu sync.RWMutex

	servers     map[string]model.Server
	tools       map[string]model.Tool
	toolsByName map[string]model.Tool // gateway_name -> tool
	routes      map[string]model.Route
	policies    map[string]model.Policy
	credentials map[string]model.Credential
	traffic     []model.TrafficSample

	seq int64
}

// New 创建空的内存存储。
func New() *Store {
	return &Store{
		servers:     make(map[string]model.Server),
		tools:       make(map[string]model.Tool),
		toolsByName: make(map[string]model.Tool),
		routes:      make(map[string]model.Route),
		policies:    make(map[string]model.Policy),
		credentials: make(map[string]model.Credential),
		traffic:     make([]model.TrafficSample, 0, 64),
	}
}

// nextID 生成单调递增的唯一 id。
func (s *Store) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

// ---- ServerStore ----

// CreateServer 新增 Server；重复 id 返回冲突错误。
func (s *Store) CreateServer(_ context.Context, server *model.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if server.ID == "" {
		server.ID = s.nextID("srv")
	}
	if _, ok := s.servers[server.ID]; ok {
		return errors.New("server 已存在")
	}
	s.servers[server.ID] = *server
	return nil
}

// GetServer 按 id 读取 Server。
func (s *Store) GetServer(_ context.Context, id string) (*model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	server, ok := s.servers[id]
	if !ok {
		return nil, fmt.Errorf("server %q 不存在", id)
	}
	return &server, nil
}

// ListServers 返回全部 Server（按 id 排序，保证输出稳定）。
func (s *Store) ListServers(_ context.Context) ([]model.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Server, 0, len(s.servers))
	for _, id := range sortedKeys(s.servers) {
		out = append(out, s.servers[id])
	}
	return out, nil
}

// UpdateServer 覆盖 Server 字段。
func (s *Store) UpdateServer(_ context.Context, server *model.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.servers[server.ID]; !ok {
		return fmt.Errorf("server %q 不存在", server.ID)
	}
	s.servers[server.ID] = *server
	return nil
}

// DeleteServer 删除 Server（不级联删除 Tool，由调用方决定）。
func (s *Store) DeleteServer(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.servers[id]; !ok {
		return fmt.Errorf("server %q 不存在", id)
	}
	delete(s.servers, id)
	return nil
}

// ---- ToolStore ----

// UpsertTool 以 gateway_name 为业务键写入 Tool；存在则覆盖且保留 id。
func (s *Store) UpsertTool(_ context.Context, tool *model.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.toolsByName[tool.GatewayName]; ok {
		tool.ID = existing.ID
	} else if tool.ID == "" {
		tool.ID = s.nextID("tool")
	}
	s.tools[tool.ID] = *tool
	s.toolsByName[tool.GatewayName] = *tool
	return nil
}

// GetTool 按 id 读取 Tool。
func (s *Store) GetTool(_ context.Context, id string) (*model.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tool, ok := s.tools[id]
	if !ok {
		return nil, fmt.Errorf("tool %q 不存在", id)
	}
	return &tool, nil
}

// GetToolByGatewayName 按对外门面名读取 Tool。
func (s *Store) GetToolByGatewayName(_ context.Context, gatewayName string) (*model.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tool, ok := s.toolsByName[gatewayName]
	if !ok {
		return nil, fmt.Errorf("tool %q 不存在", gatewayName)
	}
	return &tool, nil
}

// ListTools 返回全部 Tool。
func (s *Store) ListTools(_ context.Context) ([]model.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		out = append(out, tool)
	}
	return out, nil
}

// ListToolsByServer 返回指定 Server 的 Tool 列表。
func (s *Store) ListToolsByServer(_ context.Context, serverID string) ([]model.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Tool, 0)
	for _, tool := range s.tools {
		if tool.ServerID == serverID {
			out = append(out, tool)
		}
	}
	return out, nil
}

// DeleteToolsByServer 删除指定 Server 的 Tool。
func (s *Store) DeleteToolsByServer(_ context.Context, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, tool := range s.tools {
		if tool.ServerID == serverID {
			delete(s.tools, id)
			delete(s.toolsByName, tool.GatewayName)
		}
	}
	return nil
}

// ---- RouteStore ----

// CreateRoute 新增路由。
func (s *Store) CreateRoute(_ context.Context, route *model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if route.ID == "" {
		route.ID = s.nextID("route")
	}
	s.routes[route.ID] = *route
	return nil
}

// ListRoutes 返回全部路由。
func (s *Store) ListRoutes(_ context.Context) ([]model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Route, 0, len(s.routes))
	for _, route := range s.routes {
		out = append(out, route)
	}
	return out, nil
}

// ---- PolicyStore ----

// CreatePolicy 新增策略。
func (s *Store) CreatePolicy(_ context.Context, policy *model.Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy.ID == "" {
		policy.ID = s.nextID("pol")
	}
	s.policies[policy.ID] = *policy
	return nil
}

// GetPolicy 按 id 读取策略。
func (s *Store) GetPolicy(_ context.Context, id string) (*model.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.policies[id]
	if !ok {
		return nil, fmt.Errorf("policy %q 不存在", id)
	}
	return &policy, nil
}

// ListPolicies 返回全部策略。
func (s *Store) ListPolicies(_ context.Context) ([]model.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		out = append(out, policy)
	}
	return out, nil
}

// ---- CredentialStore ----

// CreateCredential 新增凭证元数据与进程内值（memory 不落盘，重启即失）。
func (s *Store) CreateCredential(_ context.Context, credential *model.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if credential.ID == "" {
		credential.ID = s.nextID("cred")
	}
	s.credentials[credential.ID] = *credential
	return nil
}

// ListCredentialsByServer 返回指定 Server 的凭证（含内部值）。
func (s *Store) ListCredentialsByServer(_ context.Context, serverID string) ([]model.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Credential, 0)
	for _, cred := range s.credentials {
		if cred.ServerID == serverID {
			out = append(out, cred)
		}
	}
	return out, nil
}

// ---- TrafficStore ----

// AppendTraffic 追加一条调用采样。
func (s *Store) AppendTraffic(_ context.Context, sample model.TrafficSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traffic = append(s.traffic, sample)
	return nil
}

// RecentTraffic 返回最近 limit 条调用采样（按追加顺序倒序）。
func (s *Store) RecentTraffic(_ context.Context, limit int) ([]model.TrafficSample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.traffic) {
		limit = len(s.traffic)
	}
	out := make([]model.TrafficSample, 0, limit)
	for i := len(s.traffic) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.traffic[i])
	}
	return out, nil
}

// sortedKeys 返回 map 中按 key 排序的键，保证 List 结果稳定。
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
