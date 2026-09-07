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

// rejectStore 统计拒绝审计落库次数并保留最后一条采样。
type rejectStore struct {
	count int
	last  model.TrafficSample
}

func (s *rejectStore) AppendTraffic(_ context.Context, sample model.TrafficSample) error {
	s.count++
	s.last = sample
	return nil
}

// TestRecordRejection 验证拒绝审计落库（tool 恒空、status/error/IP/主体写入）与
// 按 (status, client_ip) 冷却去重：窗口内同键去重、不同 IP/不同 status 保留、
// 冷却过期后同键可再次落库。
func TestRecordRejection(t *testing.T) {
	ctx := context.Background()
	base := Rejection{
		RequestID: "req-1", TraceID: "t-1",
		Status: "rate_limit_error", Error: "服务繁忙，请稍后再试",
		Client: "key-a", ClientIP: "198.51.100.9", Timestamp: model.Now(),
	}

	s := &rejectStore{}
	r := NewRecorder(s, 1.0)
	r.RecordRejection(ctx, base)
	r.RecordRejection(ctx, base) // 同键窗口内去重
	if s.count != 1 {
		t.Fatalf("窗口内同 (status,ip) 只应落 1 行，得到 %d", s.count)
	}
	if s.last.Tool != "" || s.last.Status != "rate_limit_error" || s.last.Client != "key-a" ||
		s.last.ClientIP != "198.51.100.9" || s.last.Error != "服务繁忙，请稍后再试" {
		t.Fatalf("拒绝行字段不符：%+v", s.last)
	}

	// 不同 IP、同 status → 保留各自首条。
	r.RecordRejection(ctx, Rejection{Status: "rate_limit_error", ClientIP: "198.51.100.10"})
	if s.count != 2 {
		t.Fatalf("不同 IP 应落库，得到 %d", s.count)
	}
	// 同 IP、不同 status → 保留。
	r.RecordRejection(ctx, Rejection{Status: "authentication_error", ClientIP: "198.51.100.9"})
	if s.count != 3 {
		t.Fatalf("不同 status 应落库，得到 %d", s.count)
	}

	// 冷却窗口按注入时刻确定性断言：窗口内去重、窗口过期后放行。
	t0 := time.Unix(1_700_000_000, 0).UTC()
	r3 := NewRecorder(&rejectStore{}, 1.0)
	if !r3.rejectionAllowedAt(t0, base) {
		t.Fatal("首个事件应放行")
	}
	if r3.rejectionAllowedAt(t0.Add(time.Second), base) {
		t.Fatal("窗口内同键应去重")
	}
	if !r3.rejectionAllowedAt(t0.Add(6*time.Second), base) {
		t.Fatal("窗口过期后同键应再次放行")
	}
}
