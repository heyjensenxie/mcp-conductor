package observability

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/xmj128/mcp-conductor/internal/model"
)

// TrafficWriter 是调用日志必需的最小存储能力。
type TrafficWriter interface {
	AppendTraffic(ctx context.Context, sample model.TrafficSample) error
}

// Recorder 负责把工具调用写入 Request Log 存储并输出结构化日志。
//
// 采样率与表体记录由配置控制；采样即"丢弃"，防止调用洪峰淹没存储。
type Recorder struct {
	store      TrafficWriter
	recordBody bool
	sampleRate float64
}

// NewRecorder 创建调用记录器。
func NewRecorder(store TrafficWriter, recordBody bool, sampleRate float64) *Recorder {
	return &Recorder{store: store, recordBody: recordBody, sampleRate: sampleRate}
}

// Record 记录一条调用采样。recordBody 预留：当前 struct 含 Error 字段，
// 完整参数/返回值按 PRD 默认不落库，避免记录隐私数据。
func (r *Recorder) Record(ctx context.Context, sample model.TrafficSample) {
	if r.sampleRate < 1.0 && rand.Float64() > r.sampleRate {
		return
	}
	if err := r.store.AppendTraffic(ctx, sample); err != nil {
		slog.Warn("写入调用日志失败", "request_id", sample.RequestID, "error", err)
	}
	slog.Info("tool_call",
		"request_id", sample.RequestID,
		"trace_id", sample.TraceID,
		"server_id", sample.ServerID,
		"tool", sample.Tool,
		"client", sample.Client,
		"status", sample.Status,
		"latency_ms", sample.LatencyMS,
		"ts", sample.Timestamp.Format(time.RFC3339Nano),
	)
}
