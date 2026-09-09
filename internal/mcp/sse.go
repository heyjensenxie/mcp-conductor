package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// SSEClient 实现 MCP 旧版 HTTP+SSE 双通道传输。服务端通过 GET 事件流下发
// 消息 POST 地址和 JSON-RPC 响应；客户端把请求与通知发送到该消息地址。
type SSEClient struct {
	endpoint string
	http     *http.Client
	headers  map[string]string
	version  string
	info     Implementation
	nextID   atomic.Int64

	mu          sync.Mutex
	streamBody  io.ReadCloser
	stream      *bufio.Scanner
	messageURL  string
	started     bool
	initialized bool
	closed      bool
	streamStop  context.CancelFunc
}

// NewSSEClient 创建旧版 MCP SSE 客户端。Option 与 HTTPClient 共用，使 Header、
// 客户端身份和自定义 http.Client 在两种 HTTP 传输中保持一致。
func NewSSEClient(endpoint string, opts ...Option) *SSEClient {
	base := NewHTTPClient(endpoint, opts...)
	return &SSEClient{endpoint: base.endpoint, http: base.http, headers: base.headers, version: base.version, info: base.info}
}

func (c *SSEClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initializeLocked(ctx)
}

func (c *SSEClient) ListTools(ctx context.Context) ([]Tool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSessionLocked(ctx); err != nil {
		return nil, err
	}
	var out ListToolsResult
	if err := c.requestLocked(ctx, "tools/list", ListToolsRequestParams{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

func (c *SSEClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSessionLocked(ctx); err != nil {
		return nil, err
	}
	var out CallToolResult
	if err := c.requestLocked(ctx, "tools/call", CallToolRequestParams{Name: name, Arguments: arguments}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Close 关闭长连接并取消其 Context；可重复调用。
func (c *SSEClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.streamStop != nil {
		c.streamStop()
	}
	if c.streamBody != nil {
		return c.streamBody.Close()
	}
	return nil
}

func (c *SSEClient) ensureSessionLocked(ctx context.Context) error {
	if c.initialized {
		return nil
	}
	_, err := c.initializeLocked(ctx)
	return err
}

func (c *SSEClient) initializeLocked(ctx context.Context) (*InitializeResult, error) {
	if err := c.startLocked(ctx); err != nil {
		return nil, err
	}
	params := InitializeRequestParams{ProtocolVersion: c.version, ClientInfo: c.info, Capabilities: Capabilities{Tools: &ToolCapabilities{}}}
	var out InitializeResult
	if err := c.requestLocked(ctx, "initialize", params, &out); err != nil {
		return nil, err
	}
	if err := c.notifyLocked(ctx, "notifications/initialized"); err != nil {
		return nil, err
	}
	c.initialized = true
	return &out, nil
}

// startLocked 建立 SSE 流并等待服务端 endpoint 事件。
func (c *SSEClient) startLocked(ctx context.Context) error {
	if c.started {
		return nil
	}
	if c.closed {
		return fmt.Errorf("SSE 客户端已关闭")
	}
	streamCtx, stop := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		stop()
		return fmt.Errorf("构造 SSE 连接请求失败: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	c.applyHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		stop()
		return fmt.Errorf("建立 SSE 连接失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 257))
		stop()
		return &UpstreamHTTPError{Status: resp.StatusCode, Body: truncate(string(body), 256)}
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		defer resp.Body.Close()
		stop()
		return fmt.Errorf("SSE 上游响应类型无效: %q", resp.Header.Get("Content-Type"))
	}
	c.streamBody = resp.Body
	c.streamStop = stop
	c.stream = bufio.NewScanner(resp.Body)
	c.stream.Buffer(make([]byte, 64<<10), stdioMaxMessage)
	for {
		event, data, err := c.readEventLocked(ctx)
		if err != nil {
			_ = resp.Body.Close()
			stop()
			return fmt.Errorf("读取 SSE endpoint 失败: %w", err)
		}
		if event != "endpoint" {
			continue
		}
		messageURL, err := resolveSSEMessageURL(c.endpoint, string(data))
		if err != nil {
			_ = resp.Body.Close()
			stop()
			return err
		}
		c.messageURL = messageURL
		c.started = true
		return nil
	}
}

func (c *SSEClient) requestLocked(ctx context.Context, method string, params any, out any) error {
	id := json.RawMessage(fmt.Sprintf("%d", c.nextID.Add(1)))
	body, err := marshalSSEEnvelope(id, method, params)
	if err != nil {
		return err
	}
	if err := c.postLocked(ctx, body); err != nil {
		return err
	}
	for {
		_, data, err := c.readEventLocked(ctx)
		if err != nil {
			return fmt.Errorf("读取 SSE 应答失败: %w", err)
		}
		var resp struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("解析 SSE 应答失败: %w", err)
		}
		if resp.Method != "" || string(resp.ID) != string(id) {
			continue
		}
		if resp.Error != nil {
			return &RPCErrorResponse{Code: resp.Error.Code, Message: resp.Error.Message}
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("解码 SSE 应答失败: %w", err)
		}
		return nil
	}
}

func (c *SSEClient) notifyLocked(ctx context.Context, method string) error {
	body, err := marshalSSEEnvelope(nil, method, nil)
	if err != nil {
		return err
	}
	return c.postLocked(ctx, body)
}

func marshalSSEEnvelope(id json.RawMessage, method string, params any) ([]byte, error) {
	envelope := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method"`
		Params  any             `json:"params,omitempty"`
	}{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("序列化 SSE 请求失败: %w", err)
	}
	return body, nil
}

func (c *SSEClient) postLocked(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.messageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造 SSE 消息请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	c.applyHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("发送 SSE 消息失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 257))
		return &UpstreamHTTPError{Status: resp.StatusCode, Body: truncate(string(responseBody), 256)}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return nil
}

func (c *SSEClient) applyHeaders(req *http.Request) {
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
}

// readEventLocked 读取一个完整 SSE 事件，支持多行 data 与 CRLF。
func (c *SSEClient) readEventLocked(ctx context.Context) (string, []byte, error) {
	var event string
	var data []string
	for c.stream.Scan() {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		line := strings.TrimSuffix(c.stream.Text(), "\r")
		if line == "" {
			if len(data) == 0 {
				continue
			}
			return event, []byte(strings.Join(data, "\n")), nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if err := c.stream.Err(); err != nil {
		return "", nil, err
	}
	return "", nil, io.ErrUnexpectedEOF
}

// resolveSSEMessageURL 要求消息地址与 SSE 地址同源，避免凭据 Header 外泄。
func resolveSSEMessageURL(streamURL, endpoint string) (string, error) {
	base, err := url.Parse(streamURL)
	if err != nil {
		return "", fmt.Errorf("SSE 地址无效: %w", err)
	}
	reference, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("SSE 消息地址无效: %w", err)
	}
	resolved := base.ResolveReference(reference)
	if !strings.EqualFold(resolved.Scheme, base.Scheme) || !strings.EqualFold(resolved.Host, base.Host) {
		return "", fmt.Errorf("SSE 消息地址必须与连接地址同源")
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("SSE 消息地址协议无效: %q", resolved.Scheme)
	}
	return resolved.String(), nil
}
