package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// HTTPClient 是基于 net/http 的最小 MCP 上游客户端。
//
// 支持对任意实现了 streamable HTTP（纯 POST JSON-RPC）的 MCP Server 执行
// initialize / tools/list / tools/call。若服务器以 SSE 片段响应，也会做
// 兼容解析，尽量接入既有的生态服务器。
type HTTPClient struct {
	endpoint string
	http     *http.Client
	headers  map[string]string
	version  string
	info     Implementation
	nextID   atomic.Int64
}

// Option 是客户端构造选项。
type Option func(*HTTPClient)

// WithHTTPClient 注入自定义 HTTP 客户端（超时、TLS、代理等）。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *HTTPClient) { c.http = hc }
}

// WithHeader 追加每次请求携带的额外 header，用于注入上游凭据等。
func WithHeader(key, value string) Option {
	return func(c *HTTPClient) { c.headers[key] = value }
}

// WithClientInfo 覆盖客户端身份（默认 mcp-conductor/0.1.0）。
func WithClientInfo(info Implementation) Option {
	return func(c *HTTPClient) { c.info = info }
}

// NewHTTPClient 创建上游客户端。
func NewHTTPClient(endpoint string, opts ...Option) *HTTPClient {
	c := &HTTPClient{
		endpoint: endpoint,
		http:     &http.Client{Timeout: 15 * time.Second},
		headers:  make(map[string]string),
		version:  DefaultProtocolVersion,
		info:     Implementation{Name: "mcp-conductor", Version: ServerVersion},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Initialize 执行 MCP 握手并返回服务器信息。
func (c *HTTPClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeRequestParams{
		ProtocolVersion: c.version,
		ClientInfo:      c.info,
		Capabilities:    Capabilities{Tools: &ToolCapabilities{}},
	}
	var out InitializeResult
	if err := c.call(ctx, "initialize", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTools 发现上游工具定义。
func (c *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	var out ListToolsResult
	if err := c.call(ctx, "tools/list", ListToolsRequestParams{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool 调用上游工具并返回结果。
func (c *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	var out CallToolResult
	if err := c.call(ctx, "tools/call", CallToolRequestParams{Name: name, Arguments: arguments}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// call 执行一次 JSON-RPC 调用并解码 result 到 out。
func (c *HTTPClient) call(ctx context.Context, method string, params any, out any) error {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("序列化请求参数失败: %w", err)
	}
	envelope := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", c.nextID.Add(1))),
		Method:  method,
		Params:  rawParams,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if c.version != "" {
		httpReq.Header.Set("MCP-Protocol-Version", c.version)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求上游失败: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("读取上游响应失败: %w", err)
	}
	// 兼容 SSE 片段响应：提取 data 行后再解析 JSON。
	if strings.Contains(httpResp.Header.Get("Content-Type"), "text/event-stream") {
		respBody = extractSSEPayload(respBody)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("上游返回非 2xx: %d %s", httpResp.StatusCode, truncate(string(respBody), 256))
	}

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("解析上游响应失败: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("上游调用失败 [%d] %s", resp.Error.Code, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("解码上游结果失败: %w", err)
	}
	return nil
}

// extractSSEPayload 从 SSE 文本中提取最后一个 data 行的载荷。
func extractSSEPayload(body []byte) []byte {
	for _, line := range bytes.Split(body, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			return bytes.TrimSpace(trimmed[len("data:"):])
		}
	}
	return body
}

// truncate 截断过长文本，防止错误信息刷屏。
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
