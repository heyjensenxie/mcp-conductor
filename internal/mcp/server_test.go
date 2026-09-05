package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeService 是测试用工具服务。
type fakeService struct{}

func (fakeService) ListTools(_ context.Context) ([]Tool, error) {
	return []Tool{{Name: "university.search"}, {Name: "course.search"}}, nil
}

func (fakeService) CallTool(_ context.Context, name string, _ map[string]any) (*CallToolResult, error) {
	return &CallToolResult{Content: []ContentBlock{{Type: "text", Text: "hi " + name}}}, nil
}

// doPost 便捷构造 POST JSON-RPC 请求。
func doPost(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	return out
}

func TestHandler_Initialize(t *testing.T) {
	handler := NewHandler(fakeService{})
	rec := doPost(t, handler, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-24","clientInfo":{"name":"test","version":"1"},"capabilities":{}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize 应返回 200，得到 %d", rec.Code)
	}
	resp := decodeResp(t, rec)
	if resp["error"] != nil {
		t.Fatalf("initialize 不应报错: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != "2025-11-24" {
		t.Fatalf("协议版本协商错误: %v", result["protocolVersion"])
	}
}

// TestHandler_Initialize_ProtocolNegotiation 覆盖向下协商：旧客户端（2024-11-05）
// 不得被回一个 2025-11-24（否则客户端报 "Server's protocol version is not supported"）。
func TestHandler_Initialize_ProtocolNegotiation(t *testing.T) {
	handler := NewHandler(fakeService{})
	cases := []struct{ req, want string }{
		{"2025-11-25", "2025-11-25"}, // Cherry Studio 2.0.9 请求的新修订版
		{"2025-11-24", "2025-11-24"},
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		{"2026-03-01", "2025-11-25"}, // 未实现/未验证的未来版本：只回我们实现的最高版，不回显
	}
	for _, c := range cases {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"clientInfo":{"name":"t","version":"1"},"capabilities":{}}}`, c.req)
		resp := decodeResp(t, doPost(t, handler, body))
		if resp["error"] != nil {
			t.Fatalf("请求 %s 不应报错: %v", c.req, resp["error"])
		}
		got := resp["result"].(map[string]any)["protocolVersion"]
		if got != c.want {
			t.Fatalf("请求 %s 应协商到 %s，得到 %v", c.req, c.want, got)
		}
	}
}

func TestHandler_ListTools(t *testing.T) {
	handler := NewHandler(fakeService{})
	rec := doPost(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	resp := decodeResp(t, rec)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("应返回 2 个聚合工具，得到 %d", len(tools))
	}
	first := tools[0].(map[string]any)
	if first["name"] != "university.search" {
		t.Fatalf("工具名应为 namespaced 名，得到 %v", first["name"])
	}
}

func TestHandler_CallTool(t *testing.T) {
	handler := NewHandler(fakeService{})
	rec := doPost(t, handler, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"university.search","arguments":{"q":"x"}}}`)
	resp := decodeResp(t, rec)
	if resp["error"] != nil {
		t.Fatalf("tools/call 不应报错: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["isError"] == true {
		t.Fatal("正常调用不应标记 isError")
	}
	text := result["content"].([]any)[0].(map[string]any)["text"]
	if text != "hi university.search" {
		t.Fatalf("内容回传错误: %v", text)
	}
}

func TestHandler_NotificationNoResponse(t *testing.T) {
	handler := NewHandler(fakeService{})
	rec := doPost(t, handler, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("通知应返回 202，得到 %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("通知不应有响应体: %q", rec.Body.String())
	}
}

func TestHandler_UnknownMethod(t *testing.T) {
	handler := NewHandler(fakeService{})
	rec := doPost(t, handler, `{"jsonrpc":"2.0","id":9,"method":"resources/list","params":{}}`)
	resp := decodeResp(t, rec)
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != float64(codeMethodNotFound) {
		t.Fatalf("未知方法应返回 method_not_found，得到 %v", errObj["code"])
	}
}

func TestHandler_GetMethodNotAllowed(t *testing.T) {
	handler := NewHandler(fakeService{})
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 应返回 405（无状态模式），得到 %d", rec.Code)
	}
}
