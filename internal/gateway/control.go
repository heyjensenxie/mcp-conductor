package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/eval"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/observability"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// envelope 是控制面 API 的统一响应结构（PRD：code + message + request_id）。
type envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Data      any    `json:"data,omitempty"`
}

// writeEnvelope 输出统一信封响应。
func writeEnvelope(w http.ResponseWriter, status int, code, message, requestID string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = marshal(w, envelope{
		Code:      code,
		Message:   message,
		RequestID: requestID,
		Data:      data,
	})
}

// writeOK 输出成功信封。
func writeOK(w http.ResponseWriter, requestID string, data any) {
	writeEnvelope(w, http.StatusOK, "ok", "success", requestID, data)
}

// marshal 序列化 JSON 到响应体。
func marshal(w http.ResponseWriter, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// Control 是控制面 REST API 的处理器集合。
//
// 供 Vue3 Console 使用；网关/统一端点路径与这些路由在同一 mux 下，
// 由相同的认证/限流中间件保护。
type Control struct {
	registry *registry.Service
	store    storage.Store
	metrics  *observability.Metrics
	keyHash  func(token string) (string, error) // 见 NewControl
	// probeNow 由 registerControlRoutes 注入：注册/启用 Server 后触发即时健康
	// 巡检；nil（如单测直接构造）则不触发，交由周期巡检兜底。
	probeNow func(serverID string)
	// eval 由 registerControlRoutes 注入：MCP 评测服务（质量分 + 回归用例）；
	// nil（如单测直接构造且未注入）表示评测未启用。
	eval *eval.Service
	// keyCall 由 registerControlRoutes 注入：控制面「以某 API Key 身份试调用」
	// 能力（*MCPGateway 满足）；nil 表示未装配（handleKeyInvoke 返回 500）。
	keyCall KeyCallService
	// replay 由 registerControlRoutes 注入：把捕获的调用回放到其上游实例；
	// nil 表示未装配（handleReplayLog 返回 500）。
	replay ReplayService
	// trendRetentionMinutes 长程分钟桶趋势保留窗口（分钟）。由 app 按
	// observability.trend_retention_days 换算注入；NewControl 提供 7 天兜底。
	trendRetentionMinutes int
	// runtimeCache 由 registerControlRoutes 注入：运行期治理配置缓存（含 config.yaml
	// 回退种子），供 /api/runtime-config 读取/保存后失效；nil（如单测直接构造且未
	// 注入）时回退到空配置。
	runtimeCache *runtimeConfigCache
	// rateLimitEnabled 标识启动配置 ratelimit.enabled（运行期不可开关）：关闭时
	// 三级阈值不生效，Console 据此提示惰态。
	rateLimitEnabled bool
}

// NewControl 创建控制面处理器。
// keyHash 负责把 API Key 明文映射为落库哈希（由 app 注入 auth.KeyHash），
// 保证 Control 不接触 token_secret。
func NewControl(registry *registry.Service, store storage.Store, metrics *observability.Metrics, keyHash func(token string) (string, error)) *Control {
	return &Control{
		registry:              registry,
		store:                 store,
		metrics:               metrics,
		keyHash:               keyHash,
		trendRetentionMinutes: 7 * 24 * 60, // 默认 7 天；app 按配置覆盖
	}
}

// handleListServers 分页列出 Server（含水合实例），支持 q/enabled/health_status 筛选。
func (c *Control) handleListServers(w http.ResponseWriter, r *http.Request) {
	q, err := bindServerQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	servers, total, err := c.store.QueryServers(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	hydrated := make([]model.Server, 0, len(servers))
	for i := range servers {
		h, hErr := c.hydrateServer(r.Context(), servers[i])
		if hErr != nil {
			writeGatewayError(w, r, http.StatusInternalServerError, hErr)
			return
		}
		hydrated = append(hydrated, h)
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.Server]{Items: hydrated, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// handleCreateServer 注册逻辑 Server（含 seed 实例）并触发工具发现。
func (c *Control) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var in registry.CreateServerInput
	if err := decodeBody(r, &in); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	created, err := c.registry.CreateServer(r.Context(), in)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	// 注册即触发即时健康巡检，让列表健康状态无需等周期巡检。
	if c.probeNow != nil {
		c.probeNow(created.ID)
	}
	hydrated, err := c.hydrateServer(r.Context(), *created)
	if err != nil {
		writeGatewayError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeEnvelope(w, http.StatusCreated, "ok", "success", RequestIDFrom(r.Context()), hydrated)
}

// handleGetServer 读取单个逻辑 Server（含实例）。
func (c *Control) handleGetServer(w http.ResponseWriter, r *http.Request) {
	server, err := c.registry.GetServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	hydrated, err := c.hydrateServer(r.Context(), *server)
	if err != nil {
		writeGatewayError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), hydrated)
}

// handleUpdateServer 更新 Server 的可编辑字段（name/endpoint/transport 不可改，
// endpoint/transport 属于实例，见 handleUpdateInstance）。
func (c *Control) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	var patch registry.UpdateServerPatch
	if err := decodeBody(r, &patch); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	server, err := c.registry.UpdateServer(r.Context(), r.PathValue("id"), patch)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	hydrated, err := c.hydrateServer(r.Context(), *server)
	if err != nil {
		writeGatewayError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), hydrated)
}

// handleToggleServer 启用/禁用 Server。
func (c *Control) handleToggleServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeBody(r, &body); err != nil || body.Enabled == nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "缺少 enabled 字段"))
		return
	}
	server, err := c.registry.ToggleServer(r.Context(), r.PathValue("id"), *body.Enabled)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	// 重新启用后即时探活，避免停留在 Unknown 等待周期巡检。
	if *body.Enabled && c.probeNow != nil {
		c.probeNow(server.ID)
	}
	hydrated, err := c.hydrateServer(r.Context(), *server)
	if err != nil {
		writeGatewayError(w, r, http.StatusInternalServerError, err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), hydrated)
}

// handleDeleteServer 删除 Server。
func (c *Control) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	if err := c.registry.DeleteServer(r.Context(), r.PathValue("id")); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusNoContent, "ok", "success", RequestIDFrom(r.Context()), nil)
}

// handleTestServer 测试连接并重新发现工具。
func (c *Control) handleTestServer(w http.ResponseWriter, r *http.Request) {
	if err := c.registry.Rediscover(r.Context(), r.PathValue("id")); err != nil {
		// 只回传脱敏后的外层文案（errs.SafeMessage），不透出端点 query/userinfo
		// 或上游响应正文；错误码与 HTTP 状态语义保持不变。
		writeEnvelope(w, statusForError(err), string(errs.CodeOf(err)), errs.SafeMessage(err), RequestIDFrom(r.Context()), nil)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), map[string]string{"status": "ok"})
}

// handleServerRediscoverPlan 只读预演重新发现：返回相对当前登记的变更清单，
// 不落库、不改健康；由前端弹窗展示，人工确认后调用 handleTestServer 真正应用。
func (c *Control) handleServerRediscoverPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := c.registry.PlanRediscover(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEnvelope(w, statusForError(err), string(errs.CodeOf(err)), errs.SafeMessage(err), RequestIDFrom(r.Context()), nil)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), plan)
}

// handleListTools 分页列出全部聚合工具，支持 q/server_id/enabled 筛选
// （控制面列表包含已禁用工具，供运维启停）。
func (c *Control) handleListTools(w http.ResponseWriter, r *http.Request) {
	q, err := bindToolQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	tools, total, err := c.store.QueryTools(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.Tool]{Items: tools, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// handleListServerTools 分页列出指定 Server 的工具（server_id 取自路径）。
func (c *Control) handleListServerTools(w http.ResponseWriter, r *http.Request) {
	q, err := bindToolQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	q.ServerID = r.PathValue("id")
	tools, total, err := c.store.QueryTools(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.Tool]{Items: tools, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// handleUpdateTool 维护平台对外的工具名称、描述和输入 Schema；源定义仍由
// discovery 保存，可通过 reset_* 字段恢复。
func (c *Control) handleUpdateTool(w http.ResponseWriter, r *http.Request) {
	var patch registry.UpdateToolPatch
	if err := decodeBody(r, &patch); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	tool, err := c.registry.UpdateTool(r.Context(), r.PathValue("id"), patch)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), tool)
}

// handleToggleTool 启用/禁用单个工具（其余字段由发现过程拥有）。
func (c *Control) handleToggleTool(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeBody(r, &body); err != nil || body.Enabled == nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "缺少 enabled 字段"))
		return
	}
	tool, err := c.registry.ToggleTool(r.Context(), r.PathValue("id"), *body.Enabled)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), tool)
}

// routeInput 是创建路由的显式 DTO（防客户端伪造 id/时间戳）。
type routeInput struct {
	Name      string   `json:"name"`
	ServerID  string   `json:"server_id"`
	ToolNames []string `json:"tool_names"`
	Enabled   *bool    `json:"enabled"`
}

// handleCreateRoute 新增路由定义（引用 Server 与 gateway 工具均须存在）。
func (c *Control) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var in routeInput
	if err := decodeBody(r, &in); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := model.Now()
	route := &model.Route{Name: in.Name, ServerID: in.ServerID, ToolNames: in.ToolNames, Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	if err := c.validateRoute(r.Context(), route); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if err := c.store.CreateRoute(r.Context(), route); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusCreated, "ok", "success", RequestIDFrom(r.Context()), route)
}

// handleUpdateRoute 更新路由可编辑字段（按路径 id；name/server_id/tool_names/enabled 空值不改）。
func (c *Control) handleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	route, err := c.store.GetRoute(r.Context(), id)
	if err != nil {
		writeGatewayError(w, r, http.StatusNotFound, errs.Wrap(errs.CodeNotFound, err, "route %q 不存在", id))
		return
	}
	var patch struct {
		Name      *string   `json:"name"`
		ServerID  *string   `json:"server_id"`
		ToolNames *[]string `json:"tool_names"`
		Enabled   *bool     `json:"enabled"`
	}
	if err := decodeBody(r, &patch); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if patch.Name != nil {
		route.Name = *patch.Name
	}
	if patch.ServerID != nil {
		route.ServerID = *patch.ServerID
	}
	if patch.ToolNames != nil {
		route.ToolNames = *patch.ToolNames
	}
	if patch.Enabled != nil {
		route.Enabled = *patch.Enabled
	}
	route.UpdatedAt = model.Now()
	if err := c.validateRoute(r.Context(), route); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if err := c.store.UpdateRoute(r.Context(), route); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), route)
}

// handleToggleRoute 启用/禁用路由。
func (c *Control) handleToggleRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeBody(r, &body); err != nil || body.Enabled == nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "缺少 enabled 字段"))
		return
	}
	id := r.PathValue("id")
	route, err := c.store.GetRoute(r.Context(), id)
	if err != nil {
		writeGatewayError(w, r, http.StatusNotFound, errs.Wrap(errs.CodeNotFound, err, "route %q 不存在", id))
		return
	}
	route.Enabled = *body.Enabled
	route.UpdatedAt = model.Now()
	if route.Enabled {
		if err := c.validateRoute(r.Context(), route); err != nil {
			writeGatewayError(w, r, statusForError(err), err)
			return
		}
	}
	if err := c.store.UpdateRoute(r.Context(), route); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), route)
}

// handleDeleteRoute 删除路由。
func (c *Control) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	if err := c.store.DeleteRoute(r.Context(), r.PathValue("id")); err != nil {
		writeGatewayError(w, r, http.StatusNotFound, errs.Wrap(errs.CodeNotFound, err, "route %q 不存在", r.PathValue("id")))
		return
	}
	writeEnvelope(w, http.StatusNoContent, "ok", "success", RequestIDFrom(r.Context()), nil)
}

// handleListRoutes 分页列出路由，支持 q/server_id/enabled 筛选。
func (c *Control) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	q, err := bindRouteQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	routes, total, err := c.store.QueryRoutes(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.Route]{Items: routes, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// validateRoute 校验路由引用完整性。启用的覆盖路由还必须确保目标 Server 已发现
// 同名上游工具，且不与另一条启用覆盖路由争抢同一 gateway 工具，避免请求期才
// 暴露配置错误或依赖隐式创建时间决定优先级。
func (c *Control) validateRoute(ctx context.Context, route *model.Route) error {
	if strings.TrimSpace(route.Name) == "" {
		return errs.New(errs.CodeInvalidArgument, "route.name 不能为空")
	}
	if strings.TrimSpace(route.ServerID) == "" {
		return errs.New(errs.CodeInvalidArgument, "route.server_id 不能为空")
	}
	targetServer, err := c.store.GetServer(ctx, route.ServerID)
	if err != nil {
		return errs.Wrap(errs.CodeNotFound, err, "server %q 不存在", route.ServerID)
	}
	tools := make(map[string]*model.Tool, len(route.ToolNames))
	for _, name := range route.ToolNames {
		tool, err := c.store.GetToolByGatewayName(ctx, name)
		if err != nil {
			return errs.Wrap(errs.CodeNotFound, err, "工具 %q 未注册", name)
		}
		tools[name] = tool
		// 禁用规则可作为尚未就绪的草稿保存；重新启用时才要求目标已发现
		// 同名工具，避免把控制台配置错误延迟为上游 tools/call 失败。
		if route.Enabled {
			if _, err := c.store.GetToolBySource(ctx, route.ServerID, tool.OriginalName); err != nil {
				return errs.New(errs.CodeInvalidArgument,
					"路由目标 Server %q 未发现工具 %q", targetServer.Name, tool.OriginalName)
			}
		}
	}
	if !route.Enabled {
		return nil
	}
	routes, err := c.store.ListRoutes(ctx)
	if err != nil {
		return errs.Wrap(errs.CodeInternal, err, "读取路由列表失败")
	}
	for _, existing := range routes {
		if existing.ID == route.ID || !existing.Enabled {
			continue
		}
		for _, name := range existing.ToolNames {
			tool, overlaps := tools[name]
			if !overlaps {
				continue
			}
			// 指向工具原属 Server 的 Route 是恒等规则，Resolver 不会把它作为
			// 覆盖目标，因此不产生真实的竞争。
			if existing.ServerID == tool.ServerID {
				continue
			}
			return errs.New(errs.CodeInvalidArgument,
				"工具 %q 已被启用路由 %q 覆盖，请先禁用或修改该路由", name, existing.Name)
		}
	}
	return nil
}

// handleCreateCredential 新增凭证：接收可选 value 敏感值（值不下发 API）。
// api_key 须提供注入 header；static_token 固定注入 Authorization: Bearer。
func (c *Control) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Header string `json:"header"`
		Value  string `json:"value"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if input.Name == "" {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "缺少 credential.name"))
		return
	}
	credential := &model.Credential{
		ServerID: r.PathValue("id"),
		Name:     input.Name,
		Kind:     model.CredentialKind(input.Kind),
		Header:   input.Header,
		Value:    input.Value,
	}
	switch credential.Kind {
	case "", model.CredentialAPIKey, model.CredentialStaticToken:
	default:
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "credential.kind 仅支持 static_token / api_key"))
		return
	}
	if credential.Kind != model.CredentialAPIKey {
		credential.Kind = model.CredentialStaticToken
	}
	if credential.Kind == model.CredentialAPIKey && credential.Header == "" {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "api_key 类型须提供注入 header"))
		return
	}
	if err := c.store.CreateCredential(r.Context(), credential); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	// value 由 model.Credential.Value 的 json:"-" 保证不外发，仅返回元数据。
	writeOK(w, RequestIDFrom(r.Context()), credential)
}

// handleListCredentials 分页列出指定 Server 的凭证元数据，支持 q/kind/has_value
// 筛选。列表语义为元数据（不解密值，Value json:"-" 不外发）。
func (c *Control) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	q, err := bindCredentialQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	q.ServerID = r.PathValue("id")
	credentials, total, err := c.store.QueryCredentials(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.Credential]{Items: credentials, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// handleUpdateCredential 更新指定 Server 下某个凭证的元数据；空值字段表示
// 不改动，空 value 保留原凭证值。返回值沿用 json:"-" 不外发明文。
func (c *Control) handleUpdateCredential(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	credID := r.PathValue("credId")

	existing, err := c.findCredential(r.Context(), serverID, credID)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	var input struct {
		Name   string `json:"name"`
		Kind   string `json:"kind"`
		Header string `json:"header"`
		Value  string `json:"value"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}

	// 合并当前值用于校验（api_key 类型须有注入 header）。
	merged := *existing
	if input.Name != "" {
		merged.Name = input.Name
	}
	if input.Kind != "" {
		merged.Kind = model.CredentialKind(input.Kind)
	}
	if input.Header != "" {
		merged.Header = input.Header
	}
	switch merged.Kind {
	case "", model.CredentialAPIKey, model.CredentialStaticToken:
	default:
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "credential.kind 仅支持 static_token / api_key"))
		return
	}
	if merged.Kind == model.CredentialAPIKey && merged.Header == "" {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "api_key 类型须提供注入 header"))
		return
	}

	upd := &model.Credential{ID: credID, ServerID: serverID}
	if input.Name != "" {
		upd.Name = input.Name
	}
	if input.Kind != "" {
		upd.Kind = model.CredentialKind(input.Kind)
	}
	if input.Header != "" {
		upd.Header = input.Header
	}
	if input.Value != "" {
		upd.Value = input.Value
	}
	if err := c.store.UpdateCredential(r.Context(), upd); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	fresh, err := c.findCredential(r.Context(), serverID, credID)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), fresh)
}

// handleDeleteCredential 删除指定 Server 下某个凭证（revoke）。
func (c *Control) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	credID := r.PathValue("credId")
	// 归属校验：确保凭证属于该 Server，避免跨 Server 误删。
	if _, err := c.findCredential(r.Context(), serverID, credID); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if err := c.store.DeleteCredential(r.Context(), credID); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusNoContent, "ok", "success", RequestIDFrom(r.Context()), nil)
}

// findCredential 返回指定 Server 下 ID 匹配的凭证；不存在返回 not_found。
func (c *Control) findCredential(ctx context.Context, serverID, credID string) (*model.Credential, error) {
	creds, err := c.store.ListCredentialsByServer(ctx, serverID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取凭证失败")
	}
	for i := range creds {
		if creds[i].ID == credID {
			return &creds[i], nil
		}
	}
	return nil, errs.New(errs.CodeNotFound, "credential %q 不存在", credID)
}

// ---- AccessKey ----

// handleListKeys 分页列出全部 API Key（不含 KeyHash/Secret），支持 q/enabled 筛选。
func (c *Control) handleListKeys(w http.ResponseWriter, r *http.Request) {
	q, err := bindAccessKeyQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	keys, total, err := c.store.QueryAccessKeys(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.AccessKey]{Items: keys, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// createKeyResponse 是创建 API Key 的响应：普通 AccessKey 字段 + 仅此一次的
// 明文 Secret。模型字段已 json:"-"，故序列化需在此显式补出一次。
type createKeyResponse struct {
	model.AccessKey
	Secret string `json:"secret"`
}

// handleCreateKey 创建 API Key：生成随机明文密钥并返回（仅此一次），
// 落库仅存哈希；subject 唯一。Grants 为白名单授权 + 调用配置。
func (c *Control) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string `json:"name"`
		Subject string `json:"subject"`
		QPS     int    `json:"qps"`
		Burst   int    `json:"burst"`
		// WindowSeconds 该 key 专属滑动窗口（秒，0=跟随全局；1..3600 覆盖）。
		WindowSeconds int               `json:"window_seconds"`
		Grants        []model.ToolGrant `json:"grants"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if input.Name == "" || input.Subject == "" {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "name 与 subject 不能为空"))
		return
	}
	if input.WindowSeconds < 0 || input.WindowSeconds > 3600 {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.New(errs.CodeInvalidArgument, "key.window_seconds 须在 0..3600"))
		return
	}
	// key.qps：>0 专属 / 0 跟随默认 / -1 不设 key 上限；其余负数非法。
	if input.QPS < -1 {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.New(errs.CodeInvalidArgument, "key.qps 仅支持 -1（不设 key 上限）、0（跟随默认）或 >0"))
		return
	}
	if _, err := c.store.GetAccessKeyBySubject(r.Context(), input.Subject); err == nil {
		writeGatewayError(w, r, http.StatusConflict, errs.New(errs.CodeInvalidArgument, "主题 %q 已存在", input.Subject))
		return
	}
	secret := randomHex(32)
	keyHash, err := c.keyHash(secret)
	if err != nil {
		writeGatewayError(w, r, http.StatusInternalServerError, errs.Wrap(errs.CodeInternal, err, "生成密钥哈希失败"))
		return
	}
	now := model.Now()
	key := &model.AccessKey{
		Name:          input.Name,
		Subject:       input.Subject,
		Enabled:       true,
		QPS:           input.QPS,
		Burst:         input.Burst,
		WindowSeconds: input.WindowSeconds,
		Grants:        input.Grants,
		KeyHash:       keyHash,
		Secret:        secret, // 明文仅本次响应下发
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := c.store.CreateAccessKey(r.Context(), key); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusCreated, "ok", "success", RequestIDFrom(r.Context()),
		createKeyResponse{AccessKey: *key, Secret: secret})
}

// handleGetKey 读取单个 API Key。
func (c *Control) handleGetKey(w http.ResponseWriter, r *http.Request) {
	key, err := c.store.GetAccessKey(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), key)
}

// handleUpdateKey 更新 API Key：名称/启用/配额/白名单；不重设密钥明文。
func (c *Control) handleUpdateKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
		QPS     *int    `json:"qps"`
		Burst   *int    `json:"burst"`
		// WindowSeconds 该 key 专属滑动窗口（秒，0=跟随全局；1..3600 覆盖）。
		WindowSeconds *int               `json:"window_seconds"`
		Grants        *[]model.ToolGrant `json:"grants"`
	}
	if err := decodeBody(r, &input); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if input.WindowSeconds != nil && (*input.WindowSeconds < 0 || *input.WindowSeconds > 3600) {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.New(errs.CodeInvalidArgument, "key.window_seconds 须在 0..3600"))
		return
	}
	// key.qps：>0 专属 / 0 跟随默认 / -1 不设 key 上限；其余负数非法。
	if input.QPS != nil && *input.QPS < -1 {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.New(errs.CodeInvalidArgument, "key.qps 仅支持 -1（不设 key 上限）、0（跟随默认）或 >0"))
		return
	}
	key, err := c.store.GetAccessKey(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if input.Name != nil {
		key.Name = *input.Name
	}
	if input.Enabled != nil {
		key.Enabled = *input.Enabled
	}
	if input.QPS != nil {
		key.QPS = *input.QPS
	}
	if input.Burst != nil {
		key.Burst = *input.Burst
	}
	if input.WindowSeconds != nil {
		key.WindowSeconds = *input.WindowSeconds
	}
	if input.Grants != nil {
		key.Grants = *input.Grants
	}
	key.UpdatedAt = model.Now()
	if err := c.store.UpdateAccessKey(r.Context(), key); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), key)
}

// handleDeleteKey 删除 API Key（grants 由存储级联清理）。
func (c *Control) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	if err := c.store.DeleteAccessKey(r.Context(), r.PathValue("id")); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusNoContent, "ok", "success", RequestIDFrom(r.Context()), nil)
}

// handleRotateKeySecret 轮换 API Key 密钥：重新随机生成明文密钥并在本响应
// 返回一次（与创建同契约），落库仅存新哈希，旧密钥即刻失效。
// 名称/启用/配额/白名单等其余配置保持不变；沿用既有 Get→Update 全量写回，
// 不引入部分更新语义。明文仍不入库、不随后续任何接口回读。
func (c *Control) handleRotateKeySecret(w http.ResponseWriter, r *http.Request) {
	key, err := c.store.GetAccessKey(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	secret := randomHex(32)
	keyHash, err := c.keyHash(secret)
	if err != nil {
		writeGatewayError(w, r, http.StatusInternalServerError, errs.Wrap(errs.CodeInternal, err, "生成密钥哈希失败"))
		return
	}
	key.KeyHash = keyHash
	key.UpdatedAt = model.Now()
	if err := c.store.UpdateAccessKey(r.Context(), key); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusOK, "ok", "success", RequestIDFrom(r.Context()),
		createKeyResponse{AccessKey: *key, Secret: secret})
}

// randomHex 生成 n 字节随机十六进制字符串（如密钥明文）。
func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// handleMetrics 返回调用指标快照。
// 默认（tool）不含按 Server/实例聚合的行（server:/instance: 前缀，避免污染工具维
// 语义）；?scope=server 只返回 Server 维行（Servers 列表）；?scope=instance 只返回
// 实例维行（可配 &server_id= 收敛到某 Server 的实例，供 Server 详情实例表展示）。
func (c *Control) handleMetrics(w http.ResponseWriter, r *http.Request) {
	all := c.metrics.SnapshotAll()
	scope := r.URL.Query().Get("scope")
	serverID := strings.TrimSpace(r.URL.Query().Get("server_id"))
	out := make([]observability.Snapshot, 0, len(all))
	for _, s := range all {
		isServer := strings.HasPrefix(s.Key, observability.ServerDimPrefix)
		isInstance := strings.HasPrefix(s.Key, observability.InstanceDimPrefix)
		switch scope {
		case "server":
			if isServer {
				out = append(out, s)
			}
		case "instance":
			if isInstance && (serverID == "" || strings.HasPrefix(s.Key, observability.InstanceDimPrefix+serverID+":")) {
				out = append(out, s)
			}
		default: // "" 或 "tool"：工具维 = 非 server:/instance: 前缀
			if !isServer && !isInstance {
				out = append(out, s)
			}
		}
	}
	writeOK(w, RequestIDFrom(r.Context()), out)
}

// handleLogs 分页返回调用日志，支持 server_id/instance_id/q/status/from/to 筛选。
// 不再提供旧 ?limit= 截断语义：控制台通过 page/page_size 取页（Dashboard 等
// 全量场景传较大 page_size）。
func (c *Control) handleLogs(w http.ResponseWriter, r *http.Request) {
	q, err := bindTrafficQuery(r)
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, err)
		return
	}
	logs, total, err := c.store.QueryTraffic(r.Context(), q)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), pageData[model.TrafficSample]{Items: logs, Total: total, Page: q.Page, PageSize: q.PageSize})
}

// decodeBody 解码 JSON 请求体。
func decodeBody(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}

// statusForError 把统一错误码映射为 HTTP 状态码。
func statusForError(err error) int {
	switch {
	case errs.Is(err, errs.CodeInvalidArgument):
		return http.StatusBadRequest
	case errs.Is(err, errs.CodeNotFound), errs.Is(err, errs.CodeRoute):
		return http.StatusNotFound
	case errs.Is(err, errs.CodeAuthentication):
		return http.StatusUnauthorized
	case errs.Is(err, errs.CodeAuthorization):
		return http.StatusForbidden
	case errs.Is(err, errs.CodeRateLimit):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
