package observability

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// TrafficWriter 是调用日志必需的最小存储能力。
type TrafficWriter interface {
	AppendTraffic(ctx context.Context, sample model.TrafficSample) error
}

// Recorder 负责把工具调用写入 Request Log 存储并输出结构化日志。
//
// 采样率与入参捕获由配置控制；采样即"丢弃"，防止调用洪峰淹没存储。
type Recorder struct {
	store       TrafficWriter
	captureArgs bool
	sampleRate  float64
}

// NewRecorder 创建调用记录器。
// captureArgs 为 true 时保留入参（record_args 开启，供 Traffic Replay）；
// 否则丢弃，避免把可能含隐私的请求体落库。
func NewRecorder(store TrafficWriter, captureArgs bool, sampleRate float64) *Recorder {
	return &Recorder{store: store, captureArgs: captureArgs, sampleRate: sampleRate}
}

// Record 记录一条调用采样。入参仅当开启捕获时才随行落库；响应**永不**记录
// （工具均为查询语义，回放只复用入参）。采样即丢弃整行（连同入参）。
func (r *Recorder) Record(ctx context.Context, sample model.TrafficSample) {
	if r.sampleRate < 1.0 && rand.Float64() > r.sampleRate {
		return
	}
	if !r.captureArgs {
		sample.RequestArgs = nil
	}
	if err := r.store.AppendTraffic(ctx, sample); err != nil {
		slog.Warn("写入调用日志失败", "request_id", sample.RequestID, "error", err)
	}
	slog.Info("tool_call",
		"request_id", sample.RequestID,
		"trace_id", sample.TraceID,
		"server_id", sample.ServerID,
		"instance_id", sample.InstanceID,
		"tool", sample.Tool,
		"client", sample.Client,
		"status", sample.Status,
		"latency_ms", sample.LatencyMS,
		"ts", sample.Timestamp.Format(time.RFC3339Nano),
	)
}
