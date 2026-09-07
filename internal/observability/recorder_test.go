package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// recorderStore 是 Record 的最小存储替身（丢弃采样，专注日志属性断言）。
type recorderStore struct{}

func (recorderStore) AppendTraffic(context.Context, model.TrafficSample) error { return nil }

// TestRecorderLogIncludesInstanceID 验证 tool_call 结构化日志携带命中实例，
// 供调用归属排障使用。
func TestRecorderLogIncludesInstanceID(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(prev)

	r := NewRecorder(recorderStore{}, 1.0)
	r.Record(context.Background(), model.TrafficSample{
		RequestID:  "req-1",
		ServerID:   "srv-1",
		InstanceID: "inst-1",
		Tool:       "mock.search",
		Client:     "p",
		Status:     "success",
		LatencyMS:  3,
		Timestamp:  model.Now(),
	})

	out := buf.String()
	if !strings.Contains(out, "instance_id=inst-1") {
		t.Fatalf("tool_call 日志应含 instance_id=inst-1，得到：\n%s", out)
	}
}

// captureStore 记录最后写入的采样，供断言入参保留语义。
type captureStore struct {
	got *model.TrafficSample
}

func (s *captureStore) AppendTraffic(_ context.Context, sample model.TrafficSample) error {
	copied := sample
	s.got = &copied
	return nil
}

// TestRecorderPreservesProvidedArgs 验证 Recorder 不自行裁剪入参：捕获与否由网关
// 数据面（runtimeRecordArgs）决定，Record 原样落库网关已捕获的入参。
func TestRecorderPreservesProvidedArgs(t *testing.T) {
	on := &captureStore{}
	base := model.TrafficSample{
		RequestID: "req-1", ServerID: "srv-1", Tool: "demo",
		Status: "success", LatencyMS: 3, Timestamp: model.Now(),
		RequestArgs: map[string]any{"q": "hello"},
	}
	NewRecorder(on, 1.0).Record(context.Background(), base)
	if on.got == nil || len(on.got.RequestArgs) == 0 {
		t.Fatal("Recorder 应原样保留网关已捕获的入参")
	}
	if on.got.RequestArgs["q"] != "hello" {
		t.Fatalf("入参内容不符：%v", on.got.RequestArgs)
	}
}
