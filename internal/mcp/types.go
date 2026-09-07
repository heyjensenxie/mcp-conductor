// Package mcp 实现 MCP（Model Context Protocol）协议类型与最小 JSON-RPC 2.0 传输。
//
// 范围：MVP 第一版只实现 initialize / ping / tools/list / tools/call 四个方法，
// 采用 streamable HTTP 的无状态模式（GET 返回 405 表示不支持 SSE，客户端
// 走纯 POST JSON-RPC），符合规范且便于在 Gateway 链路中插入中间件与审计。
package mcp

import "encoding/json"

// SupportedProtocolVersions 是网关声明支持并愿意协商的协议版本（升序）。
// 只列出真正实现其 wire 语义的版本（Legacy：initialize 握手时代）。
// 官方 2026-07-28 及以后为无状态 Modern 时代（无 initialize、server/discover、
// MRTR 等架构级差异），本网关尚未实现，因此不得列入、也不得回显。
var SupportedProtocolVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-24", "2025-11-25"}

// DefaultProtocolVersion 是网关作为客户端（连上游 MCP Server）时请求的首选版本。
const DefaultProtocolVersion = "2025-11-25"

// pickProtocolVersion 协商 MCP 协议版本（能力白名单，不做未知版本回显）。
//
// 规则：
//   - 客户端请求精确命中已实现版本 → 原样应答；
//   - 客户端请求为空 → 应答我们最新的版本；
//   - 客户端请求未知但晚于某个已实现版本 → 应答“已实现且不高于请求”的最高版本
//     （向下协商，只在我们真正实现的集合内选）；
//   - 其余（早于最早实现版本）→ 应答最早支持版本。
//
// 绝不回显集合外的未知版本：声明某版本 = 真正实现了该版本的 wire 语义；
// 2026-07-28 及以后的 Modern 客户端需要独立协议适配，而非追加版本字符串。
func pickProtocolVersion(requested string) string {
	if requested == "" {
		return SupportedProtocolVersions[len(SupportedProtocolVersions)-1]
	}
	// 从新到旧：精确命中直接回；请求比 v 新但不在集合内时，v 即“已实现且不高于请求”的最高版。
	for i := len(SupportedProtocolVersions) - 1; i >= 0; i-- {
		v := SupportedProtocolVersions[i]
		if requested == v {
			return v
		}
		if requested > v {
			return v
		}
	}
	// 请求早于我们最早实现的版本。
	return SupportedProtocolVersions[0]
}

// JSON-RPC 2.0 标准错误码。
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// ---- JSON-RPC 2.0 信封 ----

// JSONRPCRequest 是客户端发起的请求/通知信封。
type JSONRPCRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// JSONRPCResponse 是服务端响应信封。
// Result 与 Error 二者取其一（null 表示成功且无负载）。
type JSONRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError 是 JSON-RPC 错误对象。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---- MCP 领域类型 ----

// Implementation 描述 MCP 过程中的名称与版本。
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Capabilities 是 MCP 能力声明（MVP 仅声明最小能力位）。
type Capabilities struct {
	Tools *ToolCapabilities `json:"tools,omitempty"`
}

// ToolCapabilities 声明工具能力。
type ToolCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// Tool 是对外暴露的工具定义。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// ContentBlock 是 tools/call 结果中的内容块。
type ContentBlock struct {
	Type string `json:"type"` // text | image | resource ...
	Text string `json:"text,omitempty"`
}

// ---- initialize ----

// InitializeRequestParams 是 initialize 请求参数。
type InitializeRequestParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ClientInfo      Implementation `json:"clientInfo"`
	Capabilities    Capabilities   `json:"capabilities,omitempty"`
}

// InitializeResult 是 initialize 响应。
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ServerInfo      Implementation `json:"serverInfo"`
	Capabilities    Capabilities   `json:"capabilities,omitempty"`
	Instructions    string         `json:"instructions,omitempty"`
}

// ---- tools/list ----

// CallToolResult 是 tools/call 响应。
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ListToolsRequestParams 是 tools/list 请求参数。
type ListToolsRequestParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListToolsResult 是 tools/list 响应。
type ListToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ---- tools/call ----

// CallToolRequestParams 是 tools/call 请求参数。
type CallToolRequestParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}
