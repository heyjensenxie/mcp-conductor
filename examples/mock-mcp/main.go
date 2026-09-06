// command mock-mcp 是用于演示与本地联调的示例 MCP Server。
//
// 提供 search / detail 两个回显工具，监听 9000 端口（可用 MOCK_PORT 覆盖），
// 端点路径为 /mcp。配合后端的最小闭环演示：
//
//	go run ./examples/mock-mcp
//	（在后端 Console 注册 http://localhost:9000/mcp 并观察工具聚合与调用路由）
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
)

// mockService 以固定工具列表模拟上游 MCP Server，并对 tools/call 做回显。
type mockService struct{}

// ListTools 返回固定工具集（含输入 Schema，验证 Gateway 聚合透传）。
func (mockService) ListTools(_ context.Context) ([]mcp.Tool, error) {
	strProps := map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}
	return []mcp.Tool{
		{Name: "search", Description: "查询策略列表", InputSchema: strProps},
		{Name: "detail", Description: "查看策略详情", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}}},
	}, nil
}

// CallTool 回显方法名与参数，模拟一次真实返回。
func (mockService) CallTool(_ context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.ContentBlock{{Type: "text", Text: fmt.Sprintf("[%s] 收到参数: %v", name, arguments)}},
	}, nil
}

func main() {
	port := os.Getenv("MOCK_PORT")
	if port == "" {
		port = "9000"
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewHandler(mockService{}))

	addr := ":" + port
	slog.Info("mock MCP Server 监听中", "addr", addr, "endpoint", "/mcp")
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("mock server 退出", "error", err)
		os.Exit(1)
	}
}
