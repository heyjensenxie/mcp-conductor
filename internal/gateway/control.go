package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/observability"
	"github.com/xmj128/mcp-conductor/internal/registry"
	"github.com/xmj128/mcp-conductor/internal/storage"
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
}

// NewControl 创建控制面处理器。
func NewControl(registry *registry.Service, store storage.Store, metrics *observability.Metrics) *Control {
	return &Control{registry: registry, store: store, metrics: metrics}
}

// handleListServers 列出全部 Server。
func (c *Control) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := c.registry.ListServers(r.Context())
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), servers)
}

// handleCreateServer 注册 Server 并触发工具发现。
func (c *Control) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var server model.Server
	if err := decodeBody(r, &server); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	created, err := c.registry.CreateServer(r.Context(), &server)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusCreated, "ok", "success", RequestIDFrom(r.Context()), created)
}

// handleGetServer 读取单个 Server。
func (c *Control) handleGetServer(w http.ResponseWriter, r *http.Request) {
	server, err := c.registry.GetServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), server)
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
	writeOK(w, RequestIDFrom(r.Context()), server)
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
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), map[string]string{"status": "ok"})
}

// handleListTools 列出全部聚合工具。
func (c *Control) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools, err := c.registry.ListTools(r.Context())
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), tools)
}

// handleListServerTools 列出指定 Server 的工具。
func (c *Control) handleListServerTools(w http.ResponseWriter, r *http.Request) {
	tools, err := c.registry.ListServerTools(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), tools)
}

// handleCreateRoute 新增路由定义。
func (c *Control) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var route model.Route
	if err := decodeBody(r, &route); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if err := c.store.CreateRoute(r.Context(), &route); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), route)
}

// handleListRoutes 列出全部路由。
func (c *Control) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := c.store.ListRoutes(r.Context())
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), routes)
}

// handleCreatePolicy 新增权限策略。
func (c *Control) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var policy model.Policy
	if err := decodeBody(r, &policy); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if err := c.store.CreatePolicy(r.Context(), &policy); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), policy)
}

// handleListPolicies 列出全部策略。
func (c *Control) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := c.store.ListPolicies(r.Context())
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), policies)
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

// handleListCredentials 列出指定 Server 的凭证元数据。
func (c *Control) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	credentials, err := c.store.ListCredentialsByServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), credentials)
}

// handleMetrics 返回调用指标快照。
func (c *Control) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeOK(w, RequestIDFrom(r.Context()), c.metrics.SnapshotAll())
}

// handleLogs 返回最近的调用日志（Request Log）。
func (c *Control) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := c.store.RecentTraffic(r.Context(), 100)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), logs)
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
