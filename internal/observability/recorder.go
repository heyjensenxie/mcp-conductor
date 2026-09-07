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
// 采样率由配置控制；采样即"丢弃"，防止调用洪峰淹没存储。入参捕获决策不属于本层：
// tools/call 是否携带入参由网关数据面按运行期有效配置（observability.record_args）
// 决定后再调用 Record，本层不做 privacy 闸门（避免依赖方向倒置/包环）。后续新增
// Recorder 落库点时须在网关观测点收敛，勿绕过捕获闸门。
type Recorder struct {
	store      TrafficWriter
	sampleRate float64
}

// NewRecorder 创建调用记录器。
// sampleRate 为 0-1 采样率：低于 1.0 时按概率丢弃整行（连同入参，若网关已捕获）。
func NewRecorder(store TrafficWriter, sampleRate float64) *Recorder {
	return &Recorder{store: store, sampleRate: sampleRate}
}

// Record 记录一条调用采样。入参是否已捕获由网关决定（记录调用样本构造时）；采样
// 即丢弃整行（连同入参）。响应**永不**记录（工具均为查询语义，回放只复用入参）。
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
		"instance_id", sample.InstanceID,
		"tool", sample.Tool,
		"client", sample.Client,
		"ip", sample.ClientIP,
		"status", sample.Status,
		"latency_ms", sample.LatencyMS,
		"ts", sample.Timestamp.Format(time.RFC3339Nano),
	)
}
