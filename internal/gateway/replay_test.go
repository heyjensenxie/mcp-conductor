package gateway

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
	"github.com/heyjensenxie/mcp-conductor/internal/storage/memory"
)

// recordingCaller 记录最近一次直连调用入参，供断言回放的目标实例/工具/参数。
type recordingCaller struct {
	gotServer   model.Server
	gotInstance model.Instance
	gotTool     string
	gotArgs     map[string]any
	failErr     error
}

func (f *recordingCaller) Call(_ context.Context, server model.Server, instance model.Instance, tool string, arguments map[string]any, _ map[string]string) ([]registry.CallContent, error) {
	f.gotServer = server
	f.gotInstance = instance
	f.gotTool = tool
	f.gotArgs = arguments
	if f.failErr != nil {
		return nil, f.failErr
	}
	return []registry.CallContent{{Type: "text", Text: "pong"}}, nil
}

// replayFixture 种子：一个 Server + 两个实例 + 工具 + 含入参的调用行。
func replayFixture(t *testing.T) (*memory.Store, string, int64) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()

	srv := &model.Server{ID: "srv-1", Name: "replay", Enabled: true}
	if err := store.CreateServer(ctx, srv); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	for _, iid := range []string{"i1", "i2"} {
		inst := &model.Instance{
			ID: iid, ServerID: srv.ID, Endpoint: "http://localhost/mcp",
			Transport: model.TransportStreamableHTTP, Enabled: true, HealthStatus: model.ServerStatusUnknown,
		}
		if err := store.CreateInstance(ctx, inst); err != nil {
			t.Fatalf("seed instance: %v", err)
		}
	}
	if err := store.UpsertTool(ctx, &model.Tool{
		ID: "t1", ServerID: srv.ID, OriginalName: "real.search", GatewayName: "mock.search", Enabled: true,
	}); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
	if err := store.AppendTraffic(ctx, model.TrafficSample{
		RequestID: "req-1", ServerID: srv.ID, InstanceID: "i1", Tool: "mock.search",
		Status: "success", LatencyMS: 5, Timestamp: model.Now(),
		RequestArgs: map[string]any{"q": "hello"},
	}); err != nil {
		t.Fatalf("seed traffic: %v", err)
	}
	return store, srv.ID, 1
}

func TestReplayServiceHappyPath(t *testing.T) {
	store, srvID, id := replayFixture(t)
	caller := &recordingCaller{}
	svc := NewReplayService(store, caller, time.Second)

	res, err := svc.Replay(t.Context(), id, 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Content != "pong" || res.IsError || res.ServerID != srvID || res.InstanceID != "i1" {
		t.Fatalf("回放结果异常: %+v", res)
	}
	// 应把 gateway 名还原为 original_name 直连原实例，并带上捕获入参。
	if caller.gotTool != "real.search" || caller.gotInstance.ID != "i1" ||
		!reflect.DeepEqual(caller.gotArgs, map[string]any{"q": "hello"}) {
		t.Fatalf("直连调用参数异常: tool=%s inst=%s args=%v", caller.gotTool, caller.gotInstance.ID, caller.gotArgs)
	}
}

func TestReplayServiceStructuralFailures(t *testing.T) {
	store, _, id := replayFixture(t)
	svc := NewReplayService(store, &recordingCaller{}, time.Second)

	// 行不存在 → not_found。
	if _, err := svc.Replay(t.Context(), 999, 0); !errs.Is(err, errs.CodeNotFound) {
		t.Fatalf("缺失行应 not_found，得到 %v", err)
	}
	// 追加无入参行 → invalid_argument。
	if err := store.AppendTraffic(t.Context(), model.TrafficSample{
		RequestID: "req-2", ServerID: "srv-1", Tool: "mock.search", Status: "success", LatencyMS: 1,
		Timestamp: model.Now(),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := svc.Replay(t.Context(), id+1, 0); !errs.Is(err, errs.CodeInvalidArgument) {
		t.Fatalf("无入参应 invalid_argument，得到 %v", err)
	}
}

func TestReplayServiceInstanceFallback(t *testing.T) {
	store, srvID, id := replayFixture(t)
	ctx := context.Background()
	// 命中实例 i1 变为不可调（禁用）→ 应回退到 i2。
	if err := store.UpdateInstance(ctx, &model.Instance{
		ID: "i1", ServerID: srvID, Endpoint: "http://localhost/mcp",
		Transport: model.TransportStreamableHTTP, Enabled: false, HealthStatus: model.ServerStatusUnhealthy,
	}); err != nil {
		t.Fatalf("disable i1: %v", err)
	}
	caller := &recordingCaller{}
	svc := NewReplayService(store, caller, time.Second)
	res, err := svc.Replay(ctx, id, 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.IsError || caller.gotInstance.ID != "i2" {
		t.Fatalf("应回退到可调实例 i2，得到 inst=%s res=%+v", caller.gotInstance.ID, res)
	}

	// 两个实例均不可调 → is_error route。
	_ = store.UpdateInstance(ctx, &model.Instance{
		ID: "i2", ServerID: srvID, Endpoint: "http://localhost/mcp",
		Transport: model.TransportStreamableHTTP, Enabled: false, HealthStatus: model.ServerStatusUnhealthy,
	})
	res, err = svc.Replay(ctx, id, 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !res.IsError || res.ErrorCode != string(errs.CodeRoute) {
		t.Fatalf("无可调实例应 is_error route，得到 %+v", res)
	}
}
