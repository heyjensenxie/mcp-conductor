package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// TestCallAsKey_grantedMergesConfigAndSkipsTelemetry 验证「以 Key 身份试调用」走
// 完整数据面（grant 参数/头合并 + 均衡选实例）且**不写 metrics/调用日志**。
func TestCallAsKey_grantedMergesConfigAndSkipsTelemetry(t *testing.T) {
	caller := &fakeCaller{}
	g, store, metrics := seedGatewayFull(t, caller)
	key := &model.AccessKey{
		ID: "key-1", Name: "Partner", Subject: "partner-a", Enabled: true,
		Grants: []model.ToolGrant{{
			GatewayName: "mock.search",
			Headers:     map[string]string{"X-Tenant": "p"},
			DefaultArgs: map[string]any{"tenant": "t-1"},
		}},
	}

	res := g.CallAsKey(context.Background(), key, "mock.search", map[string]any{"q": "x"})
	if !res.Allowed || res.IsError {
		t.Fatalf("应放行且成功: %+v", res)
	}
	if res.ServerID != "srv-1" || res.InstanceID != "inst-1" {
		t.Fatalf("应归属 srv-1/inst-1: %+v", res)
	}
	if res.Content != "ok" {
		t.Fatalf("content 应回显上游文本: %+v", res)
	}
	// grant 的 default_args 合并 + 工具级 header 传递（与真实数据面一致）。
	if caller.capturedArgs["tenant"] != "t-1" || caller.capturedArgs["q"] != "x" {
		t.Fatalf("grant default_args 未合并: %+v", caller.capturedArgs)
	}
	if caller.capturedHdrs["X-Tenant"] != "p" {
		t.Fatalf("grant headers 未传递: %+v", caller.capturedHdrs)
	}

	// 诊断不写遥测。
	if len(metrics.SnapshotAll()) != 0 {
		t.Fatalf("试调用不应写 metrics: %+v", metrics.SnapshotAll())
	}
	logs, err := store.RecentTraffic(context.Background(), 10)
	if err != nil || len(logs) != 0 {
		t.Fatalf("试调用不应写调用日志: logs=%+v err=%v", logs, err)
	}
}

// TestCallAsKey_denied 验证未授权工具 → Allowed=false + authorization_error，且不写遥测。
func TestCallAsKey_denied(t *testing.T) {
	g, store, metrics := seedGatewayFull(t, nil)
	key := &model.AccessKey{
		ID: "key-1", Subject: "partner-a", Enabled: true,
		Grants: []model.ToolGrant{{GatewayName: "mock.detail"}}, // 未授权 mock.search
	}

	res := g.CallAsKey(context.Background(), key, "mock.search", nil)
	if res.Allowed {
		t.Fatalf("未授权工具应 Allowed=false: %+v", res)
	}
	if res.ErrorCode != "authorization_error" {
		t.Fatalf("错误码应为 authorization_error: %+v", res)
	}
	if len(metrics.SnapshotAll()) != 0 {
		t.Fatalf("试调用不应写 metrics: %+v", metrics.SnapshotAll())
	}
	logs, _ := store.RecentTraffic(context.Background(), 10)
	if len(logs) != 0 {
		t.Fatalf("试调用不应写调用日志: %+v", logs)
	}
}

// TestCallAsKey_noAvailableInstance 验证无可拨测实例 → Allowed=true + route_error。
func TestCallAsKey_noAvailableInstance(t *testing.T) {
	g, store, _ := seedGatewayFull(t, nil)
	// 摘除唯一实例。
	if err := store.UpdateInstance(context.Background(), &model.Instance{
		ID: "inst-1", ServerID: "srv-1", Endpoint: "http://localhost:9000/mcp",
		Transport: model.TransportStreamableHTTP, Enabled: false,
		HealthStatus: model.ServerStatusUnknown, UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}
	key := &model.AccessKey{ID: "key-1", Subject: "p", Enabled: true, Grants: []model.ToolGrant{{GatewayName: "*"}}}

	res := g.CallAsKey(context.Background(), key, "mock.search", nil)
	if !res.Allowed || !res.IsError || res.ErrorCode != "route_error" {
		t.Fatalf("无可拨测实例应 route_error: %+v", res)
	}
}

// stubKeyCall 是 handler 测试用的固定「按 Key 试调用」替身。
type stubKeyCall struct{ res *KeyCallResult }

func (s *stubKeyCall) CallAsKey(context.Context, *model.AccessKey, string, map[string]any) *KeyCallResult {
	return s.res
}

// seedKeyInvokeControl 建带一枚 key 的 Control（keyCall 未装配，供 handler 注入）。
func seedKeyInvokeControl(t *testing.T) (*Control, *memory.Store) {
	t.Helper()
	store := memory.New()
	now := time.Now().UTC()
	if err := store.CreateAccessKey(context.Background(), &model.AccessKey{
		ID: "key-1", Name: "Partner A", Subject: "partner-a", Enabled: true,
		Grants:    []model.ToolGrant{{GatewayName: "mock.search"}},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateAccessKey: %v", err)
	}
	return NewControl(nil, store, nil, nil), store
}

// TestHandleKeyInvoke_errorPaths 覆盖 handler 的 404/403/400 契约。
func TestHandleKeyInvoke_errorPaths(t *testing.T) {
	ctrl, _ := seedKeyInvokeControl(t)
	ctrl.keyCall = &stubKeyCall{res: &KeyCallResult{Allowed: true, Content: "ok"}}

	invoke := func(keyID, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/keys/"+keyID+"/invoke", strings.NewReader(body))
		req.SetPathValue("id", keyID)
		ctrl.handleKeyInvoke(rec, req)
		return rec
	}

	// 未知 key：存储层「不存在」目前是普通错误（非 errs.CodeNotFound），与既有
	// /api/keys 端点一致默认 500；给 AccessKey 存取层补错误码映射是全局改进点。
	if rec := invoke("ghost", `{"gateway_tool":"mock.search"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("未知 key 应与既有 key 端点一致的错误状态，得到 %d", rec.Code)
	}
	if rec := invoke("key-1", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 gateway_tool 应 400，得到 %d", rec.Code)
	}

	// 禁用 key → 403。
	store := ctrl.store.(*memory.Store)
	if err := store.UpdateAccessKey(context.Background(), &model.AccessKey{
		ID: "key-1", Name: "Partner A", Subject: "partner-a", Enabled: false,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpdateAccessKey: %v", err)
	}
	if rec := invoke("key-1", `{"gateway_tool":"mock.search"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("禁用 key 应 403，得到 %d: %s", rec.Code, rec.Body.String())
	}

	// 授权拒绝（keyCall 返回 Allowed=false）→ 403。
	if err := store.UpdateAccessKey(context.Background(), &model.AccessKey{
		ID: "key-1", Name: "Partner A", Subject: "partner-a", Enabled: true,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("恢复 key: %v", err)
	}
	ctrl.keyCall = &stubKeyCall{res: &KeyCallResult{ErrorCode: "authorization_error", Message: "无权调用 mock.detail"}}
	if rec := invoke("key-1", `{"gateway_tool":"mock.detail"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("授权拒绝应 403，得到 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleKeyInvoke_ok 验证放行时 200 信封携带 KeyCallResult。
func TestHandleKeyInvoke_ok(t *testing.T) {
	ctrl, _ := seedKeyInvokeControl(t)
	ctrl.keyCall = &stubKeyCall{res: &KeyCallResult{Allowed: true, ServerID: "srv-1", InstanceID: "inst-1", LatencyMS: 5, Content: "hello"}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/keys/key-1/invoke", strings.NewReader(`{"gateway_tool":"mock.search","arguments":{"q":"x"}}`))
	req.SetPathValue("id", "key-1")
	ctrl.handleKeyInvoke(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var res KeyCallResult
	if err := json.Unmarshal(decodeEnvelope(t, rec).Data, &res); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if !res.Allowed || res.InstanceID != "inst-1" || res.Content != "hello" {
		t.Fatalf("结果异常: %+v", res)
	}

	// 未装配 keyCall → 500。
	plain := NewControl(nil, memory.New(), nil, nil)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/keys/key-1/invoke", strings.NewReader(`{"gateway_tool":"mock.search"}`))
	req.SetPathValue("id", "key-1")
	plain.handleKeyInvoke(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未装配应 500，得到 %d", rec.Code)
	}
}
