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
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/query"
)

// trendKey 是分钟桶趋势的唯一键（与 trend_minute PK (scope,dim_key,minute) 对齐）。
type trendKey struct {
	scope, serverID, dimKey string
	minute                  int64
}

// Store 是基于进程内 map 的并发安全存储，实现 storage.Store 全部接口。
type Store struct {
	mu sync.RWMutex

	servers     map[string]model.Server
	instances   map[string]model.Instance
	tools       map[string]model.Tool
	toolsByName map[string]model.Tool // gateway_name -> tool
	routes      map[string]model.Route
	credentials map[string]model.Credential
	keys        map[string]model.AccessKey
	keysByHash  map[string]model.AccessKey // key_hash -> key
	keysBySubj  map[string]model.AccessKey // subject -> key
	traffic     []model.TrafficSample
	trend       map[trendKey]model.TrendMinute // 已闭合分钟桶（幂等覆盖）
	runtime     *model.RuntimeConfig           // 单份运行期治理配置（nil=无后台保存值）

	seq        int64 // nextID 字符串 id 递增源
	trafficSeq int64 // traffic 数字流水主键递增源
}

// New 创建空的内存存储。
func New() *Store {
	return &Store{
		servers:     make(map[string]model.Server),
		instances:   make(map[string]model.Instance),
		tools:       make(map[string]model.Tool),
		toolsByName: make(map[string]model.Tool),
		routes:      make(map[string]model.Route),
		credentials: make(map[string]model.Credential),
		keys:        make(map[string]model.AccessKey),
		keysByHash:  make(map[string]model.AccessKey),
		keysBySubj:  make(map[string]model.AccessKey),
		traffic:     make([]model.TrafficSample, 0, 64),
		trend:       make(map[trendKey]model.TrendMinute),
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

// DeleteServer 删除 Server，并级联清理其实例与凭证（与 MySQL 外键 CASCADE 对齐）；
// Tool 由调用方（registry）按 DeleteToolsByServer 另行清理。
func (s *Store) DeleteServer(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.servers[id]; !ok {
		return fmt.Errorf("server %q 不存在", id)
	}
	delete(s.servers, id)
	for _, iid := range sortedKeys(s.instances) {
		if s.instances[iid].ServerID == id {
			delete(s.instances, iid)
		}
	}
	for _, cid := range sortedKeys(s.credentials) {
		if s.credentials[cid].ServerID == id {
			delete(s.credentials, cid)
		}
	}
	for _, rid := range sortedKeys(s.routes) {
		if s.routes[rid].ServerID == id {
			delete(s.routes, rid)
		}
	}
	return nil
}

// ---- InstanceStore ----

// CreateInstance 新增实例；重复 id 返回冲突错误。
func (s *Store) CreateInstance(_ context.Context, instance *model.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance.ID == "" {
		instance.ID = s.nextID("inst")
	}
	if _, ok := s.instances[instance.ID]; ok {
		return errors.New("instance 已存在")
	}
	s.instances[instance.ID] = *instance
	return nil
}

// GetInstance 按 id 读取实例。
func (s *Store) GetInstance(_ context.Context, id string) (*model.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	instance, ok := s.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %q 不存在", id)
	}
	return &instance, nil
}

// ListInstances 返回全部实例（按 (created_at, id) 升序，保证输出稳定）。
func (s *Store) ListInstances(_ context.Context) ([]model.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Instance, 0, len(s.instances))
	for _, instance := range s.instances {
		out = append(out, instance)
	}
	sortInstances(out)
	return out, nil
}

// ListInstancesByServer 返回指定 Server 的实例（按 (created_at, id) 升序）。
func (s *Store) ListInstancesByServer(_ context.Context, serverID string) ([]model.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Instance, 0)
	for _, instance := range s.instances {
		if instance.ServerID == serverID {
			out = append(out, instance)
		}
	}
	sortInstances(out)
	return out, nil
}

// UpdateInstance 覆盖实例字段。
func (s *Store) UpdateInstance(_ context.Context, instance *model.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[instance.ID]; !ok {
		return fmt.Errorf("instance %q 不存在", instance.ID)
	}
	s.instances[instance.ID] = *instance
	return nil
}

// DeleteInstance 删除单个实例。
func (s *Store) DeleteInstance(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[id]; !ok {
		return fmt.Errorf("instance %q 不存在", id)
	}
	delete(s.instances, id)
	return nil
}

// DeleteInstancesByServer 删除指定 Server 的全部实例（不存在时视为成功）。
func (s *Store) DeleteInstancesByServer(_ context.Context, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, iid := range sortedKeys(s.instances) {
		if s.instances[iid].ServerID == serverID {
			delete(s.instances, iid)
		}
	}
	return nil
}

// sortInstances 按 (created_at, id) 稳定升序排列（"主实例"为升序首条）。
func sortInstances(instances []model.Instance) {
	sort.Slice(instances, func(i, j int) bool {
		if !instances[i].CreatedAt.Equal(instances[j].CreatedAt.Time) {
			return instances[i].CreatedAt.Before(instances[j].CreatedAt.Time)
		}
		return instances[i].ID < instances[j].ID
	})
}

// ---- ToolStore ----

// UpsertTool 以 gateway_name 为业务键写入 Tool；存在则覆盖且保留 id。
func (s *Store) UpsertTool(_ context.Context, tool *model.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var existing model.Tool
	var found bool
	for _, candidate := range s.tools {
		if candidate.ServerID == tool.ServerID && candidate.OriginalName == tool.OriginalName {
			existing, found = candidate, true
			break
		}
	}
	if found {
		tool.ID = existing.ID
		tool.CreatedAt = existing.CreatedAt
		if existing.NameOverridden {
			tool.GatewayName, tool.NameOverridden = existing.GatewayName, true
		}
		if existing.DescriptionOverridden {
			tool.Description, tool.DescriptionOverridden = existing.Description, true
		}
		if existing.InputSchemaOverridden {
			tool.InputSchema, tool.InputSchemaOverridden = existing.InputSchema, true
		}
		delete(s.toolsByName, existing.GatewayName)
	} else if tool.ID == "" {
		tool.ID = s.nextID("tool")
	}
	s.tools[tool.ID] = *tool
	s.toolsByName[tool.GatewayName] = *tool
	return nil
}

func (s *Store) GetToolBySource(_ context.Context, serverID string, originalName string) (*model.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, tool := range s.tools {
		if tool.ServerID == serverID && tool.OriginalName == originalName {
			copy := tool
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("tool %q/%q 不存在", serverID, originalName)
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

// SetToolEnabled 切换单个工具的启用状态。
// toolsByName 是与 tools 同源的值副本索引，必须同步写回，否则
// GetToolByGatewayName（路由解析用）仍会读到旧 Enabled。
func (s *Store) SetToolEnabled(_ context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tool, ok := s.tools[id]
	if !ok {
		return fmt.Errorf("tool %q 不存在", id)
	}
	tool.Enabled = enabled
	tool.UpdatedAt = model.Now()
	s.tools[id] = tool
	s.toolsByName[tool.GatewayName] = tool
	return nil
}

func (s *Store) UpdateTool(_ context.Context, tool *model.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tools[tool.ID]
	if !ok {
		return fmt.Errorf("tool %q 不存在", tool.ID)
	}
	if other, ok := s.toolsByName[tool.GatewayName]; ok && other.ID != tool.ID {
		return fmt.Errorf("tool gateway_name %q 已存在", tool.GatewayName)
	}
	delete(s.toolsByName, existing.GatewayName)
	s.tools[tool.ID] = *tool
	s.toolsByName[tool.GatewayName] = *tool
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
	now := model.Now()
	if route.CreatedAt.IsZero() {
		route.CreatedAt = now
	}
	route.UpdatedAt = now
	s.routes[route.ID] = *route
	return nil
}

// GetRoute 按 id 读取路由。
func (s *Store) GetRoute(_ context.Context, id string) (*model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	route, ok := s.routes[id]
	if !ok {
		return nil, fmt.Errorf("route %q 不存在", id)
	}
	return &route, nil
}

// UpdateRoute 更新路由可编辑字段并保留 CreatedAt；缺 id 报错。
func (s *Store) UpdateRoute(_ context.Context, route *model.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.routes[route.ID]
	if !ok {
		return fmt.Errorf("route %q 不存在", route.ID)
	}
	existing.Name = route.Name
	existing.ServerID = route.ServerID
	existing.ToolNames = route.ToolNames
	existing.Enabled = route.Enabled
	existing.UpdatedAt = model.Now()
	s.routes[existing.ID] = existing
	return nil
}

// DeleteRoute 删除路由；不存在时返回存在性错误。
func (s *Store) DeleteRoute(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.routes[id]; !ok {
		return fmt.Errorf("route %q 不存在", id)
	}
	delete(s.routes, id)
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

// CreateCredential 新增凭证元数据与进程内值（memory 不落盘，重启即失）。
func (s *Store) CreateCredential(_ context.Context, credential *model.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if credential.ID == "" {
		credential.ID = s.nextID("cred")
	}
	now := model.Now()
	if credential.CreatedAt.IsZero() {
		credential.CreatedAt = now
	}
	credential.UpdatedAt = now
	credential.HasValue = credential.Value != ""
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

// UpdateCredential 更新凭证元数据；空值字段表示不改动，空 Value 保留原值
// （含 HasValue 标记），非空 Value 替换并置 HasValue=true。
func (s *Store) UpdateCredential(_ context.Context, credential *model.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.credentials[credential.ID]
	if !ok {
		return fmt.Errorf("credential %q 不存在", credential.ID)
	}
	if credential.Name != "" {
		existing.Name = credential.Name
	}
	if credential.Kind != "" {
		existing.Kind = credential.Kind
	}
	if credential.Header != "" {
		existing.Header = credential.Header
	}
	if credential.Value != "" {
		existing.Value = credential.Value
		existing.HasValue = true
	}
	existing.UpdatedAt = model.Now()
	s.credentials[existing.ID] = existing
	return nil
}

// DeleteCredential 删除单个凭证。
func (s *Store) DeleteCredential(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.credentials[id]; !ok {
		return fmt.Errorf("credential %q 不存在", id)
	}
	delete(s.credentials, id)
	return nil
}

// DeleteCredentialsByServer 删除指定 Server 的全部凭证（不存在时视为成功）。
func (s *Store) DeleteCredentialsByServer(_ context.Context, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range sortedKeys(s.credentials) {
		if s.credentials[id].ServerID == serverID {
			delete(s.credentials, id)
		}
	}
	return nil
}

// ---- AccessKeyStore ----

// CreateAccessKey 新增 API Key；subject 唯一，重复返回冲突错误。
func (s *Store) CreateAccessKey(_ context.Context, key *model.AccessKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key.ID == "" {
		key.ID = s.nextID("key")
	}
	if _, ok := s.keys[key.ID]; ok {
		return errors.New("access key 已存在")
	}
	if _, ok := s.keysBySubj[key.Subject]; ok {
		return fmt.Errorf("主题 %q 已存在", key.Subject)
	}
	// 只持久化不含明文 Secret 的副本（与 MySQL 不落 secret 一致）；明文仅在
	// 创建响应下发一次。stored 为值副本，不污染调用方仍在使用的 key 对象。
	stored := *key
	stored.Secret = ""
	s.keys[key.ID] = stored
	s.keysBySubj[key.Subject] = stored
	if key.KeyHash != "" {
		s.keysByHash[key.KeyHash] = stored
	}
	return nil
}

// GetAccessKey 按 id 读取 API Key。
func (s *Store) GetAccessKey(_ context.Context, id string) (*model.AccessKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keys[id]
	if !ok {
		return nil, fmt.Errorf("access key %q 不存在", id)
	}
	return &key, nil
}

// GetAccessKeyBySubject 按主体读取 API Key。
func (s *Store) GetAccessKeyBySubject(_ context.Context, subject string) (*model.AccessKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keysBySubj[subject]
	if !ok {
		return nil, fmt.Errorf("access key（subject %q）不存在", subject)
	}
	return &key, nil
}

// GetAccessKeyByKeyHash 按密钥哈希读取 API Key（认证用）。
func (s *Store) GetAccessKeyByKeyHash(_ context.Context, keyHash string) (*model.AccessKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, ok := s.keysByHash[keyHash]
	if !ok {
		return nil, fmt.Errorf("access key %q 不存在", keyHash)
	}
	return &key, nil
}

// ListAccessKeys 返回全部 API Key（按 id 排序，保证输出稳定）。
func (s *Store) ListAccessKeys(_ context.Context) ([]model.AccessKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.AccessKey, 0, len(s.keys))
	for _, id := range sortedKeys(s.keys) {
		out = append(out, s.keys[id])
	}
	return out, nil
}

// UpdateAccessKey 覆盖 API Key 字段并同步索引（subject/哈希可能变更）。
func (s *Store) UpdateAccessKey(_ context.Context, key *model.AccessKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.keys[key.ID]
	if !ok {
		return fmt.Errorf("access key %q 不存在", key.ID)
	}
	delete(s.keysBySubj, existing.Subject)
	if existing.KeyHash != "" {
		delete(s.keysByHash, existing.KeyHash)
	}
	// 与 CreateAccessKey 一致：落库副本不含明文 Secret。
	stored := *key
	stored.Secret = ""
	s.keys[key.ID] = stored
	s.keysBySubj[key.Subject] = stored
	if key.KeyHash != "" {
		s.keysByHash[key.KeyHash] = stored
	}
	return nil
}

// DeleteAccessKey 删除 API Key 并清理索引。
func (s *Store) DeleteAccessKey(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[id]
	if !ok {
		return fmt.Errorf("access key %q 不存在", id)
	}
	delete(s.keys, id)
	delete(s.keysBySubj, key.Subject)
	if key.KeyHash != "" {
		delete(s.keysByHash, key.KeyHash)
	}
	return nil
}

// ---- TrafficStore ----

// AppendTraffic 追加一条调用采样并分配数字流水主键（列表/详情/回放按 id 寻址）。
func (s *Store) AppendTraffic(_ context.Context, sample model.TrafficSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficSeq++
	sample.ID = s.trafficSeq
	s.traffic = append(s.traffic, sample)
	return nil
}

// stripArgs 返回去除入参体的副本并置 HasArgs，供列表类读取（入参只在详情显式下发）。
func stripArgs(sample model.TrafficSample) model.TrafficSample {
	sample.HasArgs = sample.RequestArgs != nil
	sample.RequestArgs = nil
	return sample
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
		out = append(out, stripArgs(s.traffic[i]))
	}
	return out, nil
}

// RecentTrafficByServer 返回指定 Server 最近 limit 条调用采样（按追加倒序）。
func (s *Store) RecentTrafficByServer(_ context.Context, serverID string, limit int) ([]model.TrafficSample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = len(s.traffic)
	}
	out := make([]model.TrafficSample, 0, limit)
	for i := len(s.traffic) - 1; i >= 0 && len(out) < limit; i-- {
		if s.traffic[i].ServerID == serverID {
			out = append(out, stripArgs(s.traffic[i]))
		}
	}
	return out, nil
}

// GetTraffic 按主键读取单条调用采样（含已捕获入参，供详情/回放；不存在报错）。
func (s *Store) GetTraffic(_ context.Context, id int64) (*model.TrafficSample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.traffic) - 1; i >= 0; i-- {
		sample := s.traffic[i]
		if sample.ID != id {
			continue
		}
		// 返回独立副本，避免调用方经 RequestArgs 修改存储内数据。
		sample.HasArgs = sample.RequestArgs != nil
		if sample.RequestArgs != nil {
			args := make(map[string]any, len(sample.RequestArgs))
			for k, v := range sample.RequestArgs {
				args[k] = v
			}
			sample.RequestArgs = args
		}
		return &sample, nil
	}
	return nil, fmt.Errorf("traffic %d 不存在", id)
}

// DeleteTrafficBefore 删除 ts < before 的旧调用采样（保留天数收敛，见 app
// runTrafficRetention）。limit>0 时单次最多删除 limit 行（供调用方分块，与
// MySQL 端 LIMIT 语义对齐）；limit<=0 删除全部匹配。返回实际删除条数。
func (s *Store) DeleteTrafficBefore(_ context.Context, before time.Time, limit int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 用新切片而非原地覆盖：被删除样本（含 RequestArgs）不应残留在底层数组尾部阻止 GC。
	out := make([]model.TrafficSample, 0, len(s.traffic))
	var deleted int64
	for _, sample := range s.traffic {
		if !sample.Timestamp.Before(before) || (limit > 0 && deleted >= limit) {
			out = append(out, sample)
			continue
		}
		deleted++
	}
	s.traffic = out
	return deleted, nil
}

// ---- TrendStore ----

// UpsertTrendBuckets 幂等写入已闭合分钟桶（同键覆盖，重复 flush 无害）。
func (s *Store) UpsertTrendBuckets(_ context.Context, buckets []model.TrendMinute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range buckets {
		s.trend[trendKey{scope: b.Scope, serverID: b.ServerID, dimKey: b.DimKey, minute: b.Minute}] = b
	}
	return nil
}

// QueryTrendBuckets 按 query.TrendQuery 读取窗口内的分钟桶（minute 升序）。
func (s *Store) QueryTrendBuckets(_ context.Context, q query.TrendQuery) ([]model.TrendMinute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.TrendMinute, 0)
	for key, b := range s.trend {
		if key.scope != q.Scope {
			continue
		}
		if q.ServerID != "" && key.serverID != q.ServerID {
			continue
		}
		if q.DimKey != "" && key.dimKey != q.DimKey {
			continue
		}
		if b.Minute < q.From || b.Minute > q.To {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Minute < out[j].Minute })
	return out, nil
}

// DeleteTrendBucketsBefore 清理 minute < before 的旧桶（保留天数收敛）。
func (s *Store) DeleteTrendBucketsBefore(_ context.Context, beforeMinute int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.trend {
		if key.minute < beforeMinute {
			delete(s.trend, key)
		}
	}
	return nil
}

// ---- RuntimeConfigStore ----

// GetRuntimeConfig 读取运行期治理配置；无后台保存值时返回 (nil, false, nil)。
func (s *Store) GetRuntimeConfig(_ context.Context) (*model.RuntimeConfig, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.runtime == nil {
		return nil, false, nil
	}
	// 返回独立副本，避免调用方经 blocklist/whitelist 切片改动存储内数据。
	cp := *s.runtime
	if s.runtime.IPBlocklist != nil {
		cp.IPBlocklist = append([]string(nil), s.runtime.IPBlocklist...)
	}
	if s.runtime.IPWhitelist != nil {
		cp.IPWhitelist = append([]string(nil), s.runtime.IPWhitelist...)
	}
	return &cp, true, nil
}

// PutRuntimeConfig 覆盖保存运行期治理配置（单份完整快照）。
func (s *Store) PutRuntimeConfig(_ context.Context, cfg *model.RuntimeConfig) error {
	if cfg == nil {
		return fmt.Errorf("runtime config 不能为空")
	}
	cp := *cfg
	cp.IPBlocklist = append([]string(nil), cfg.IPBlocklist...)
	cp.IPWhitelist = append([]string(nil), cfg.IPWhitelist...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtime = &cp
	return nil
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
