package mcp

import (
	"context"
	"encoding/json"
	"net/http"
)

// ToolService 是统一端点对外依赖的最小能力：列出聚合工具、按名调用并路由。
// 由 Gateway 实现，负责聚合上游、鉴权、限流与审计。
type ToolService interface {
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error)
}

// ServerVersion 是网关自身在 initialize 中暴露的版本标识。
const ServerVersion = "0.1.0"

// Handler 是 MCP 统一端点（streamable HTTP 无状态模式）的 HTTP 处理器。
//
// GET 返回 405 表明本端点不支持 SSE 流，客户端按规范回退到纯 POST 调用；
// POST 处理全部 JSON-RPC 请求。认证、限流等由 Gateway 中间件在进入本
// 处理器之前完成。
type Handler struct {
	service ToolService
}

// NewHandler 创建统一端点处理器。
func NewHandler(service ToolService) *Handler {
	return &Handler{service: service}
}

// ServeHTTP 实现 http.Handler。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// 无状态模式：不支持 SSE，返回 405 引导客户端走纯 POST。
		w.Header().Set("Allow", "POST")
		http.Error(w, "streamable HTTP 仅支持 POST（MVP 未启用 SSE）", http.StatusMethodNotAllowed)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
	}
}

// handlePost 解码并分发单个 JSON-RPC 请求。
func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeResponse(w, nil, nil, &RPCError{Code: codeParseError, Message: "无效的 JSON-RPC 请求"})
		return
	}
	if req.JSONRPC != "2.0" {
		h.writeResponse(w, req.ID, nil, &RPCError{Code: codeInvalidRequest, Message: "jsonrpc 必须为 2.0"})
		return
	}

	// 通知（无 id）不需要响应。
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := h.dispatch(r.Context(), req.Method, req.Params)
	h.writeResponse(w, req.ID, result, rpcErr)
}

// dispatch 按方法分发到对应处理器。
func (h *Handler) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "initialize":
		return h.handleInitialize(params)
	case "tools/list":
		return h.handleListTools(ctx)
	case "tools/call":
		return h.handleCallTool(ctx, params)
	default:
		return nil, &RPCError{Code: codeMethodNotFound, Message: "不支持的方法: " + method}
	}
}

// handleInitialize 协商协议版本并返回服务器信息。
func (h *Handler) handleInitialize(params json.RawMessage) (any, *RPCError) {
	var p InitializeRequestParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &RPCError{Code: codeInvalidParams, Message: "initialize 参数无效"}
		}
	}
	version := DefaultProtocolVersion
	if contains(SupportedProtocolVersions, p.ProtocolVersion) {
		version = p.ProtocolVersion
	}
	return InitializeResult{
		ProtocolVersion: version,
		Capabilities:    Capabilities{Tools: &ToolCapabilities{}},
		ServerInfo:      Implementation{Name: "mcp-conductor", Version: ServerVersion},
	}, nil
}

// handleListTools 返回聚合后的全部工具。
func (h *Handler) handleListTools(ctx context.Context) (any, *RPCError) {
	tools, err := h.service.ListTools(ctx)
	if err != nil {
		return nil, &RPCError{Code: codeInternalError, Message: "列出工具失败: " + err.Error()}
	}
	return ListToolsResult{Tools: tools}, nil
}

// handleCallTool 路由工具调用。
func (h *Handler) handleCallTool(ctx context.Context, params json.RawMessage) (any, *RPCError) {
	var p CallToolRequestParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &RPCError{Code: codeInvalidParams, Message: "tools/call 参数无效"}
	}
	if p.Name == "" {
		return nil, &RPCError{Code: codeInvalidParams, Message: "缺少 tool name"}
	}
	result, err := h.service.CallTool(ctx, p.Name, p.Arguments)
	if err != nil {
		// 调用失败统一以 isError 结果返回，而非 JSON-RPC 错误。
		return CallToolResult{
			Content: []ContentBlock{{Type: "text", Text: err.Error()}},
			IsError: true,
		}, nil
	}
	return result, nil
}

// writeResponse 输出 JSON-RPC 响应；result 与 rpcErr 二选一。
func (h *Handler) writeResponse(w http.ResponseWriter, id *json.RawMessage, result any, rpcErr *RPCError) {
	var idVal any
	if id != nil {
		_ = json.Unmarshal(*id, &idVal)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      idVal,
		Result:  result,
		Error:   rpcErr,
	})
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
