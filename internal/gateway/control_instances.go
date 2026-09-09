package gateway

import (
	"context"
	"net/http"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
)

// hydrateServer 读取 Server 的实例列表并填充到响应对象（Instances 仅水合不落库）。
func (c *Control) hydrateServer(ctx context.Context, server model.Server) (model.Server, error) {
	instances, err := c.store.ListInstancesByServer(ctx, server.ID)
	if err != nil {
		return server, err
	}
	server.Instances = instances
	return server, nil
}

// hydrateServers 批量水合 Server 实例。列表页若逐个调用 hydrateServer 会产生
// N+1 存储查询；一次读取全部实例后按 server_id 分组，保持实例原有排序。
func (c *Control) hydrateServers(ctx context.Context, servers []model.Server) ([]model.Server, error) {
	instances, err := c.store.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	byServer := make(map[string][]model.Instance, len(servers))
	for _, instance := range instances {
		byServer[instance.ServerID] = append(byServer[instance.ServerID], instance)
	}
	hydrated := make([]model.Server, len(servers))
	for i, server := range servers {
		server.Instances = byServer[server.ID]
		hydrated[i] = server
	}
	return hydrated, nil
}

// handleListInstances 列出指定 Server 的实例（按 (created_at, id) 升序）。
func (c *Control) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := c.registry.ListInstances(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), instances)
}

// handleCreateInstance 为 Server 新增实例（启用的首个之外的候选端点）并触发即时探活。
func (c *Control) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint  string   `json:"endpoint"`
		Transport string   `json:"transport"`
		Args      []string `json:"args"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	instance, err := c.registry.AddInstance(r.Context(), r.PathValue("id"), body.Endpoint, model.Transport(body.Transport), body.Args)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if c.probeNow != nil {
		c.probeNow(r.PathValue("id"))
	}
	writeEnvelope(w, http.StatusCreated, "ok", "success", RequestIDFrom(r.Context()), instance)
}

// handleUpdateInstance 更新实例可编辑字段（endpoint/transport），变更后复位健康并即时探活。
func (c *Control) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	var patch registry.UpdateInstancePatch
	if err := decodeBody(r, &patch); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	instance, err := c.registry.UpdateInstance(r.Context(), r.PathValue("id"), r.PathValue("iid"), patch)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if c.probeNow != nil {
		c.probeNow(r.PathValue("id"))
	}
	writeOK(w, RequestIDFrom(r.Context()), instance)
}

// handleToggleInstance 启用/禁用单个实例（摘除/恢复，独立于 Server 级启停）。
func (c *Control) handleToggleInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeBody(r, &body); err != nil || body.Enabled == nil {
		writeGatewayError(w, r, http.StatusBadRequest, errs.New(errs.CodeInvalidArgument, "缺少 enabled 字段"))
		return
	}
	instance, err := c.registry.ToggleInstance(r.Context(), r.PathValue("id"), r.PathValue("iid"), *body.Enabled)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	// 重新启用后即时探活，避免停留在 Unknown 等待周期巡检。
	if *body.Enabled && c.probeNow != nil {
		c.probeNow(r.PathValue("id"))
	}
	writeOK(w, RequestIDFrom(r.Context()), instance)
}

// handleTestInstance 对单个实例执行同步健康探测并回写状态；失败仍以 200 返回
// unhealthy 的实例（信封 code 非 "ok"、消息为脱敏诊断，供前端提示并据 code
// 区分超时/上游错误），避免控制台把失败误显示为成功。
func (c *Control) handleTestInstance(w http.ResponseWriter, r *http.Request) {
	instance, probeErr := c.registry.TestInstance(r.Context(), r.PathValue("id"), r.PathValue("iid"))
	if probeErr != nil {
		writeEnvelope(w, http.StatusOK, string(errs.CodeOf(probeErr)), errs.SafeMessage(probeErr), RequestIDFrom(r.Context()), instance)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), instance)
}

// handleDeleteInstance 删除单个实例（Server 至少保留一个实例，见 registry.DeleteInstance）。
func (c *Control) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	if err := c.registry.DeleteInstance(r.Context(), r.PathValue("id"), r.PathValue("iid")); err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeEnvelope(w, http.StatusNoContent, "ok", "success", RequestIDFrom(r.Context()), nil)
}
