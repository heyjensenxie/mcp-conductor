package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

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

	r := NewRecorder(recorderStore{}, false, 1.0)
	r.Record(context.Background(), model.TrafficSample{
		RequestID:  "req-1",
		ServerID:   "srv-1",
		InstanceID: "inst-1",
		Tool:       "mock.search",
		Client:     "p",
		Status:     "success",
		LatencyMS:  3,
		Timestamp:  time.Now().UTC(),
	})

	out := buf.String()
	if !strings.Contains(out, "instance_id=inst-1") {
		t.Fatalf("tool_call 日志应含 instance_id=inst-1，得到：\n%s", out)
	}
}

// captureStore 记录最后写入的采样，供断言入参捕获语义。
type captureStore struct {
	got *model.TrafficSample
}

func (s *captureStore) AppendTraffic(_ context.Context, sample model.TrafficSample) error {
	copied := sample
	s.got = &copied
	return nil
}

func TestRecorderCaptureArgsGate(t *testing.T) {
	ctx := context.Background()
	args := map[string]any{"q": "hello"}
	base := model.TrafficSample{
		RequestID: "req-1", ServerID: "srv-1", Tool: "demo",
		Status: "success", LatencyMS: 3, Timestamp: time.Now().UTC(), RequestArgs: args,
	}

	// 开启捕获 → 入参随行保留。
	on := &captureStore{}
	NewRecorder(on, true, 1.0).Record(ctx, base)
	if on.got == nil || len(on.got.RequestArgs) == 0 {
		t.Fatal("captureArgs=true 时应保留入参")
	}

	// 关闭捕获（默认）→ 入参被丢弃（响应本就永不记录）。
	off := &captureStore{}
	NewRecorder(off, false, 1.0).Record(ctx, base)
	if off.got == nil {
		t.Fatal("未采样时应写入采样")
	}
	if off.got.RequestArgs != nil {
		t.Fatal("captureArgs=false 时不得携带入参")
	}
}
