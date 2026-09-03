package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xmj128/mcp-conductor/internal/model"
)

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
	server := model.Server{
		Name:      "Mock",
		Endpoint:  upstream.URL,
		Transport: model.TransportStreamableHTTP,
	}
	ctx := context.Background()

	tools, err := adapter.Discover(ctx, server)
	if err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "search" {
		t.Fatalf("Discover 结果错误: %+v", tools)
	}

	contents, err := adapter.Call(ctx, server, "search", map[string]any{"q": "x"})
	if err != nil {
		t.Fatalf("Call 失败: %v", err)
	}
	if len(contents) == 0 || contents[0].Text == "" {
		t.Fatalf("Call 内容为空: %+v", contents)
	}
}

func TestAdapter_HealthByInitialize(t *testing.T) {
	upstream := mockMCPServer(t)
	defer upstream.Close()

	adapter := New()
	status, err := adapter.Check(context.Background(), model.Server{
		Name:      "Mock",
		Endpoint:  upstream.URL,
		Transport: model.TransportStreamableHTTP,
	})
	if err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	if status != model.ServerStatusHealthy {
		t.Fatalf("健康探测应返回 healthy，得到 %s", status)
	}
}
