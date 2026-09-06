package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
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
