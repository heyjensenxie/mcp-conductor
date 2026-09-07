package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// stdio 客户端测试采用标准 helper-process 模式：主测试进程把 "-test.run=..." 与
// MCP_HELPER_STDIO 传给自身，子在 TestStdioServerHelper 中以同一二进制运行一段
// 独立逻辑（充当最小 stdio MCP Server），跨平台一致且不依赖外部程序。

// TestStdioServerHelper 是 stdio 测试里被子进程执行的 helper：以 stdio 方式服务
// 一套 MCP 工具，供 StdioClient 连接。仅当 MCP_HELPER_STDIO 被设置时运行。
// -mode 可选："iserror" 让 tools/call 对 "fail" 返回 isError 业务失败。
func TestStdioServerHelper(t *testing.T) {
	if os.Getenv("MCP_HELPER_STDIO") != "1" {
		t.Skip("helper process: 仅当作为子进程运行时执行")
	}
	mode := ""
	for i, a := range os.Args {
		if a == "-mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	svc := HelperService{mode: mode}
	if err := ServeStdio(context.Background(), svc, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "helper serve error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// HelperService 提供与演示上游一致的回显工具，并按 mode 注入故障行为。
type HelperService struct {
	mode string
}

func (h HelperService) ListTools(_ context.Context) ([]Tool, error) {
	return []Tool{
		{Name: "search", Description: "查询"},
		{Name: "detail", Description: "详情"},
	}, nil
}

func (h HelperService) CallTool(_ context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	if name == "fail" {
		return &CallToolResult{Content: []ContentBlock{{Type: "text", Text: "boom"}}, IsError: true}, nil
	}
	return &CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("[%s] %v", name, arguments)}},
	}, nil
}

// newStdioClient 创建一个连到 helper 子进程的 StdioClient（注入 helper 环境标记）。
func newStdioClient(t *testing.T, mode string) *StdioClient {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	args := []string{"-test.run=TestStdioServerHelper"}
	if mode != "" {
		args = append(args, "-mode", mode)
	}
	return NewStdioClient(self, args).WithStdioEnv("MCP_HELPER_STDIO=1")
}

// startHelper 启动 helper（通过一次带短超时的 ListTools 触发惰性 spawn 并确认就绪）。
func startHelper(t *testing.T, mode string) *StdioClient {
	t.Helper()
	c := newStdioClient(t, mode)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.ListTools(ctx); err != nil {
		t.Fatalf("helper 启动失败: %v", err)
	}
	return c
}

func TestStdioClient_InitializeListCall(t *testing.T) {
	c := startHelper(t, "")
	defer c.Close()
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("再次 Initialize 失败: %v", err)
	}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools 失败: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "search" {
		t.Fatalf("ListTools 结果错误: %+v", tools)
	}
	res, err := c.CallTool(context.Background(), "search", map[string]any{"q": "hi"})
	if err != nil {
		t.Fatalf("CallTool 失败: %v", err)
	}
	if res.IsError || len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "[search]") {
		t.Fatalf("CallTool 内容错误: %+v", res.Content)
	}
}

// TestStdioClient_ImplicitInitialize 验证不显式 Initialize 也能直接调用（自动握手）。
func TestStdioClient_ImplicitInitialize(t *testing.T) {
	c := startHelper(t, "")
	defer c.Close()
	res, err := c.CallTool(context.Background(), "search", map[string]any{})
	if err != nil {
		t.Fatalf("直接 Call 应自动握手，得到 %v", err)
	}
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("Call 结果异常: %+v", res)
	}
}

// TestStdioClient_IsErrorCall 验证 isError 结果（业务失败）被透传且不报传输错误。
func TestStdioClient_IsErrorCall(t *testing.T) {
	c := startHelper(t, "")
	defer c.Close()
	res, err := c.CallTool(context.Background(), "fail", nil)
	if err != nil {
		t.Fatalf("isError 结果不应作为传输错误返回，得到 %v", err)
	}
	if !res.IsError {
		t.Fatal("应透传 isError=true 的上游业务失败")
	}
}

// TestStdioClient_JsonRPCError 验证上游以 JSON-RPC error（如未知方法）拒绝时，
// 客户端返回 *RPCErrorResponse（业务失败，供上层区分传输故障）。
func TestStdioClient_JsonRPCError(t *testing.T) {
	c := startHelper(t, "")
	defer c.Close()
	var out struct{}
	err := c.requestLocked(context.Background(), "no/such-method", struct{}{}, &out)
	if err == nil {
		t.Fatal("未知方法应返回 JSON-RPC 错误")
	}
	var rpcErr *RPCErrorResponse
	if !errors.As(err, &rpcErr) {
		t.Fatalf("应返回 *RPCErrorResponse，得到 %T: %v", err, err)
	}
}

// TestStdioClient_CloseIdempotent 验证 Close 可重复调用且不报错。
func TestStdioClient_CloseIdempotent(t *testing.T) {
	c := newStdioClient(t, "")
	if err := c.Close(); err != nil { // 未启动即 Close 也安全
		t.Fatalf("未启动 Close 失败: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("重复 Close 应幂等: %v", err)
	}
}

func TestStdioClient_EmptyCommand(t *testing.T) {
	c := NewStdioClient("", nil)
	defer c.Close()
	if _, err := c.Initialize(context.Background()); err == nil {
		t.Fatal("空命令应返回错误")
	}
}
