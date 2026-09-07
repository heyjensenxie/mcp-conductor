package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
)

// instanceFor 构造挂在逻辑 Server "srv-1" 下的一个启用实例。
func instanceFor(endpoint string) model.Instance {
	return model.Instance{
		ServerID:     "srv-1",
		Endpoint:     endpoint,
		Transport:    model.TransportStreamableHTTP,
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
	}
}

// stdioInstanceFor 构造一个挂在 "srv-1" 下的 stdio 实例（command 在测试里填充）。

// TestAdapter_StdioDiscoverAndCheck 验证 stdio 实例经 Adapter 走子进程传输完成
// 发现与健康探测（helper-process 模式，与 internal/mcp 测试共用 helper 测试名）。
func TestAdapter_StdioDiscoverAndCheck(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	instance := model.Instance{
		ServerID:     "srv-1",
		Endpoint:     self,
		Transport:    model.TransportStdio,
		Args:         []string{"-test.run=TestStdioServerHelperProc"},
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
	}
	server := model.Server{ID: "srv-1", Name: "MockStdio"}
	ctx := context.Background()

	// 注入 helper 标记 env，使被 Spawn 的子进程进入 helper 分支而不是等待 stdin。
	adapter := New().WithStdioEnv("MCP_HELPER_STDIO=1")

	status, err := adapter.Check(ctx, server, instance)
	if err != nil {
		t.Fatalf("stdio Check 失败: %v", err)
	}
	if status != model.ServerStatusHealthy {
		t.Fatalf("stdio 健康探测应返回 healthy，得到 %s", status)
	}
	tools, err := adapter.Discover(ctx, server, instance)
	if err != nil {
		t.Fatalf("stdio Discover 失败: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "search" {
		t.Fatalf("stdio Discover 结果错误: %+v", tools)
	}
}

// TestAdapter_StdioCall 验证 stdio 实例经 Adapter 完成工具调用。
func TestAdapter_StdioCall(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	instance := model.Instance{
		ServerID:     "srv-1",
		Endpoint:     self,
		Transport:    model.TransportStdio,
		Args:         []string{"-test.run=TestStdioServerHelperProc"},
		Enabled:      true,
		HealthStatus: model.ServerStatusUnknown,
	}
	adapter := New().WithStdioEnv("MCP_HELPER_STDIO=1")
	contents, err := adapter.Call(context.Background(), model.Server{ID: "srv-1"}, instance,
		"search", map[string]any{"q": "hi"}, nil)
	if err != nil {
		t.Fatalf("stdio Call 失败: %v", err)
	}
	if len(contents) == 0 || contents[0].Text == "" {
		t.Fatalf("stdio Call 内容为空: %+v", contents)
	}
}

// TestStdioServerHelperProc 是同一测试二进制里被子进程运行的最小 stdio MCP Server，
// 供 mcpclient 的 stdio 测试使用（父进程因未设 MCP_HELPER_STDIO 直接跳过）。
func TestStdioServerHelperProc(t *testing.T) {
	if os.Getenv("MCP_HELPER_STDIO") != "1" {
		t.Skip("helper process: 仅当作为子进程运行时执行")
	}
	if err := mcp.ServeStdio(context.Background(), stdioHelperService{}, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "helper serve error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// stdioHelperService 提供与演示上游一致的回显工具。
type stdioHelperService struct{}

func (stdioHelperService) ListTools(_ context.Context) ([]mcp.Tool, error) {
	return []mcp.Tool{{Name: "search", Description: "查询"}, {Name: "detail", Description: "详情"}}, nil
}

func (stdioHelperService) CallTool(_ context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: "echo: " + name}}}, nil
}

// mockMCPServer 实现 initialize / tools/list / tools/call 的最小模拟上游，
// 用于验证 Adapter 的握手、发现与调用链路。
func mockMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeResult := func(result string) {
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(req.ID), result)))
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			writeResult(`{"protocolVersion":"2025-11-24","serverInfo":{"name":"mock","version":"1.0"},"capabilities":{"tools":{"listChanged":false}}}`)
		case "tools/list":
			writeResult(`{"tools":[{"name":"search","description":"查询"},{"name":"detail","description":"详情"}]}`)
		case "tools/call":
			writeResult(`{"content":[{"type":"text","text":"echo: ok"}],"isError":false}`)
		default:
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"unknown"}}`, string(req.ID))))
		}
	}))
}

func TestAdapter_DiscoverAndCall(t *testing.T) {
	upstream := mockMCPServer(t)
	defer upstream.Close()

	adapter := New()
	server := model.Server{ID: "srv-1", Name: "Mock"}
	instance := instanceFor(upstream.URL)
	ctx := context.Background()

	tools, err := adapter.Discover(ctx, server, instance)
	if err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "search" {
		t.Fatalf("Discover 结果错误: %+v", tools)
	}

	contents, err := adapter.Call(ctx, server, instance, "search", map[string]any{"q": "x"}, nil)
	if err != nil {
		t.Fatalf("Call 失败: %v", err)
	}
	if len(contents) == 0 || contents[0].Text == "" {
		t.Fatalf("Call 内容为空: %+v", contents)
	}
}

// TestAdapter_ExtraHeadersOfferedPerTool 验证按工具附加的 header 随调用发送，
// 且与 Server 级凭据同名时以工具级为准。
func TestAdapter_ExtraHeadersOfferedPerTool(t *testing.T) {
	var gotToolHeader, gotServerHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToolHeader = r.Header.Get("X-Tenant")
		gotServerHeader = r.Header.Get("X-Upstream-Key")
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"ok"}]}}`, string(req.ID))))
	}))
	defer upstream.Close()

	adapter := New().WithHeaderFor(func(_ context.Context, server model.Server) map[string]string {
		// Server 级凭据也声明 X-Tenant，验证工具级覆盖。
		return map[string]string{"X-Upstream-Key": "k-abcd", "X-Tenant": "server-tenant"}
	})

	if _, err := adapter.Call(context.Background(), model.Server{ID: "srv-1"}, instanceFor(upstream.URL),
		"search", map[string]any{"q": "x"}, map[string]string{"X-Tenant": "tool-tenant"}); err != nil {
		t.Fatalf("Call 失败: %v", err)
	}
	if gotServerHeader != "k-abcd" {
		t.Fatalf("Server 级凭据应注入，得到 %q", gotServerHeader)
	}
	if gotToolHeader != "tool-tenant" {
		t.Fatalf("工具级 header 应覆盖 Server 级，得到 %q", gotToolHeader)
	}
}

func TestAdapter_HealthByInitialize(t *testing.T) {
	upstream := mockMCPServer(t)
	defer upstream.Close()

	adapter := New()
	status, err := adapter.Check(context.Background(), model.Server{ID: "srv-1", Name: "Mock"}, instanceFor(upstream.URL))
	if err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	if status != model.ServerStatusHealthy {
		t.Fatalf("健康探测应返回 healthy，得到 %s", status)
	}
}

// TestAdapter_InjectsCredentialHeaders 验证 WithHeaderFor 注入的凭据头
// 会随认识（initialize）请求发送到上游。
func TestAdapter_InjectsCredentialHeaders(t *testing.T) {
	var gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Upstream-Key")
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-24","serverInfo":{"name":"mock","version":"1"}}}`, string(req.ID))))
	}))
	defer upstream.Close()

	adapter := New().WithHeaderFor(func(_ context.Context, server model.Server) map[string]string {
		if server.ID == "srv-1" {
			return map[string]string{"X-Upstream-Key": "k-abcd"}
		}
		return nil
	})

	if _, err := adapter.Discover(context.Background(), model.Server{ID: "srv-1"}, instanceFor(upstream.URL)); err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}
	if gotHeader != "k-abcd" {
		t.Fatalf("上游应收到注入的凭据 header，得到 %q", gotHeader)
	}
}

// TestAdapter_Probe 验证评测探测返回协议握手结果 + 上游工具定义。
func TestAdapter_Probe(t *testing.T) {
	upstream := mockMCPServer(t)
	defer upstream.Close()

	adapter := New()
	res, err := adapter.Probe(context.Background(), model.Server{ID: "srv-1", Name: "Mock"}, instanceFor(upstream.URL))
	if err != nil {
		t.Fatalf("Probe 失败: %v", err)
	}
	if res.ProtocolVersion != "2025-11-24" {
		t.Fatalf("应返回协商后的协议版本，得到 %q", res.ProtocolVersion)
	}
	if res.ServerInfo.Name != "mock" || res.ServerInfo.Version != "1.0" {
		t.Fatalf("serverInfo 不正确: %+v", res.ServerInfo)
	}
	if res.Capabilities.Tools == nil {
		t.Fatal("应声明 tools 能力位")
	}
	if len(res.Tools) != 2 || res.Tools[0].Name != "search" {
		t.Fatalf("应返回 2 个上游工具定义: %+v", res.Tools)
	}
}

// TestAdapter_CallTimeoutMapsToCodeTimeout 验证慢上游 + 网关超时被归类为
// CodeTimeout（而非笼统的 CodeUpstream）。
func TestAdapter_CallTimeoutMapsToCodeTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // 慢于调用方超时
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{}}`, string(req.ID))))
	}))
	defer upstream.Close()

	adapter := New()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := adapter.Call(ctx, model.Server{ID: "srv-1"}, instanceFor(upstream.URL), "search", map[string]any{}, nil)
	if err == nil {
		t.Fatal("慢上游应返回错误")
	}
	if code := errs.CodeOf(err); code != errs.CodeTimeout {
		t.Fatalf("错误码应归类为 timeout_error，得到 %s", code)
	}
}

// TestAdapter_JsonRPCErrorIsBusinessFailure 验证：上游以 JSON-RPC error 响应拒绝
// 本次调用（参数校验 / 业务异常，如 -32602 Invalid params）应归类为 ToolFailedError
// （业务失败、实例健康），而**不是**传输/实例故障——网关据此不会触发实例冷却。
func TestAdapter_JsonRPCErrorIsBusinessFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"invalid params: missing q"}}`, string(req.ID))))
	}))
	defer upstream.Close()

	_, err := New().Call(context.Background(), model.Server{ID: "srv-1"}, instanceFor(upstream.URL),
		"search", map[string]any{}, nil)
	if err == nil {
		t.Fatal("JSON-RPC error 响应应作为错误返回")
	}
	var toolFail *registry.ToolFailedError
	if !errors.As(err, &toolFail) {
		t.Fatalf("参数校验/业务拒绝应归类为 ToolFailedError（实例健康），得到: %v", err)
	}
	// 底层仍透传统一错误码，供调用日志/指标按一次失败调用记录。
	if code := errs.CodeOf(err); code != errs.CodeUpstream {
		t.Fatalf("错误码应透传为 upstream_error，得到 %s", code)
	}
	// 与传输层失败区分：JSON-RPC 拒绝不应是 timeout_error（实例故障语义）。
	if errs.IsTimeout(err) {
		t.Fatal("业务拒绝不应被归类为超时/实例故障")
	}
}

// TestAdapter_HTTPStatusClassification 验证 HTTP 4xx（客户端/参数被拒）归类为
// ToolFailedError（业务失败、实例健康）；HTTP 5xx 仍视为实例/传输级故障（不包
// ToolFailedError），网关据此决定是否冷却。
func TestAdapter_HTTPStatusClassification(t *testing.T) {
	serve := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"boom"}}`))
		}))
	}

	cases := []struct {
		name         string
		status       int
		wantToolFail bool
	}{
		{"HTTP 400 参数被拒应视为业务失败", http.StatusBadRequest, true},
		{"HTTP 422 参数被拒应视为业务失败", http.StatusUnprocessableEntity, true},
		{"HTTP 500 仍视为实例故障", http.StatusInternalServerError, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := serve(tc.status)
			defer upstream.Close()
			_, err := New().Call(context.Background(), model.Server{ID: "srv-1"}, instanceFor(upstream.URL),
				"search", map[string]any{}, nil)
			if err == nil {
				t.Fatal("非 2xx 应返回错误")
			}
			var toolFail *registry.ToolFailedError
			got := errors.As(err, &toolFail)
			if got != tc.wantToolFail {
				t.Fatalf("status=%d ToolFailedError=%v, 期望 %v (%v)", tc.status, got, tc.wantToolFail, err)
			}
		})
	}
}
