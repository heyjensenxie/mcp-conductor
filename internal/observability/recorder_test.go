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
