package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestSSEClient_LegacyLifecycle 验证旧版 MCP SSE 的双通道生命周期：先 GET
// 建立事件流并取得消息地址，再把 JSON-RPC POST 到该地址，响应经事件流返回。
func TestSSEClient_LegacyLifecycle(t *testing.T) {
	var (
		mu          sync.Mutex
		getHeader   string
		postHeader  string
		initialized bool
	)
	responses := make(chan []byte, 4)

	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			mu.Lock()
			getHeader = r.Header.Get("Authorization")
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("测试服务不支持 flush")
			}
			fmt.Fprint(w, "event: endpoint\ndata: /messages?sessionId=test-session\n\n")
			flusher.Flush()
			for {
				select {
				case payload := <-responses:
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
		case "/messages":
			mu.Lock()
			postHeader = r.Header.Get("Authorization")
			mu.Unlock()
			var req struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Method  string          `json:"method"`
			}
			if err := json.NewDecoder(bufio.NewReader(r.Body)).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			switch req.Method {
			case "initialize":
				responses <- []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","serverInfo":{"name":"legacy-sse","version":"1"}}}`, req.ID))
			case "notifications/initialized":
				mu.Lock()
				initialized = true
				mu.Unlock()
			case "tools/list":
				responses <- []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"chart"}]}}`, req.ID))
			default:
				http.Error(w, "unexpected method", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client := NewSSEClient(upstream.URL+"/sse", WithHeader("Authorization", "Bearer test-token"))
	defer client.Close()
	ctx := context.Background()
	init, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("SSE initialize 失败: %v", err)
	}
	if init.ServerInfo.Name != "legacy-sse" {
		t.Fatalf("serverInfo 不正确: %+v", init.ServerInfo)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("SSE tools/list 失败: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "chart" {
		t.Fatalf("工具列表不正确: %+v", tools)
	}

	mu.Lock()
	defer mu.Unlock()
	if getHeader != "Bearer test-token" || postHeader != "Bearer test-token" {
		t.Fatalf("鉴权头未同时注入 GET/POST: get=%q post=%q", getHeader, postHeader)
	}
	if !initialized {
		t.Fatal("initialize 成功后未发送 notifications/initialized")
	}
}

func TestResolveSSEMessageURL(t *testing.T) {
	t.Run("相对地址解析为同源消息地址", func(t *testing.T) {
		got, err := resolveSSEMessageURL("https://example.com/api/sse", "/messages?sessionId=1")
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if got != "https://example.com/messages?sessionId=1" {
			t.Fatalf("消息地址错误: %q", got)
		}
	})

	t.Run("拒绝跨源地址以免泄露凭据", func(t *testing.T) {
		if _, err := resolveSSEMessageURL("https://example.com/sse", "https://attacker.example/messages"); err == nil {
			t.Fatal("跨源消息地址应被拒绝")
		}
	})
}
