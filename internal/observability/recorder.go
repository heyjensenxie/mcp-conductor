package observability

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
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

	// rejectionWindow 是拒绝审计按 (status, client_ip) 的冷却窗口；0 时用
	// rejectionDedupWindow 默认。字段化便于测试注入短窗口，生产由构造函数设置。
	rejectionWindow time.Duration

	// dedupeMu 保护拒绝审计冷却表（seen），见 RecordRejection。
	dedupeMu sync.Mutex
	seen     map[rejectionKey]rejectionSeen
}

// NewRecorder 创建调用记录器。
// sampleRate 为 0-1 采样率：低于 1.0 时按概率丢弃整行（连同入参，若网关已捕获）。
func NewRecorder(store TrafficWriter, sampleRate float64) *Recorder {
	return &Recorder{
		store:           store,
		sampleRate:      sampleRate,
		rejectionWindow: rejectionDedupWindow,
		seen:            make(map[rejectionKey]rejectionSeen),
	}
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

// Rejection 是一次 /mcp 请求在中间件层被拒绝的审计事件（认证失败 / 限流 /
// 封禁 / key 停用）。Tool 恒为空串：拒绝时尚未进入工具维度，靠 status + 来源 IP
// 区分；成功进入 tools/call 的失败由 Record/auditFailure 记录，不走本类型。
type Rejection struct {
	// RequestID / TraceID 透传网关请求/追踪标识，便于与访问日志关联。
	RequestID string
	TraceID   string
	// Status 取统一错误码字符串（authentication_error / rate_limit_error /
	// authorization_error），供日志筛选与 IP 统计。
	Status string
	// Error 是脱敏后的拒绝原因摘要（errs.SafeMessage 口径）。
	Error string
	// Client 是具备归属身份时的 key subject；匿名 / operator / 控制面调用不填。
	Client   string
	ClientIP string
	// Timestamp 记录拒绝发生的时刻（UTC）。
	Timestamp model.Time
}

// rejectionDedupWindow 是拒绝审计按 (status, client_ip) 的冷却窗口：同一来源同类
// 拒绝窗口内只落 1 行，遏制攻击流把每次拒绝都写入存储（DDoS 写放大）。
const rejectionDedupWindow = 5 * time.Second

// rejectionDedupMax 是冷却表条目上限；超限做一轮过期清理，防随机 IP 撑爆内存。
const rejectionDedupMax = 4096

// rejectionKey 标识"某来源 IP 的某类拒绝"。
type rejectionKey struct {
	status string
	ip     string
}

// rejectionSeen 记录同键最近一次写入发生时刻（供冷却判定）。
type rejectionSeen struct {
	last time.Time
}

// RecordRejection 记录一条中间件拒绝审计（写入调用日志，tool 为空串）。
// 按 (status, client_ip) 冷却节流：窗口内重复拒绝丢弃；首个事件与窗口后的复现
// 均保留，运维仍能看到来源持续在打。采样率（sampleRate）不作用于拒绝事件，
// 避免把安全相关记录无谓丢弃（冷却已承担限流职责）。
func (r *Recorder) RecordRejection(ctx context.Context, rej Rejection) {
	if !r.rejectionAllowed(rej) {
		return
	}
	if err := r.store.AppendTraffic(ctx, model.TrafficSample{
		RequestID: rej.RequestID,
		TraceID:   rej.TraceID,
		Tool:      "",
		Client:    rej.Client,
		ClientIP:  rej.ClientIP,
		Status:    rej.Status,
		Error:     rej.Error,
		Timestamp: rej.Timestamp,
	}); err != nil {
		slog.Warn("写入拒绝审计失败", "request_id", rej.RequestID, "error", err)
	}
}

// rejectionAllowed 判定该拒绝事件是否应落库（冷却去重）。无来源 IP（测试构造 /
// 异常）恒放行，避免冷却判定空转。
// rejectionAllowed 判定该拒绝事件是否应落库（冷却去重，按当前时间）。
func (r *Recorder) rejectionAllowed(rej Rejection) bool {
	return r.rejectionAllowedAt(time.Now(), rej)
}

// rejectionAllowedAt 判定给定时刻该拒绝事件是否应落库；at 参数由调用方注入，
// 便于单元测试对冷却窗口做确定性断言（窗口内 false、过期 true）。无来源 IP
// （测试构造 / 异常）恒放行，避免冷却判定空转。
func (r *Recorder) rejectionAllowedAt(at time.Time, rej Rejection) bool {
	window := r.rejectionWindow
	if window <= 0 {
		window = rejectionDedupWindow
	}
	if rej.ClientIP == "" {
		return true
	}
	key := rejectionKey{status: rej.Status, ip: rej.ClientIP}
	r.dedupeMu.Lock()
	defer r.dedupeMu.Unlock()
	if last, ok := r.seen[key]; ok && at.Sub(last.last) < window {
		return false
	}
	r.seen[key] = rejectionSeen{last: at}
	// 上限清理：先丢过期键；仍超限则整体重建（大量随机 IP 场景比无界增长更可接受）。
	if len(r.seen) > rejectionDedupMax {
		for k, v := range r.seen {
			if at.Sub(v.last) >= window {
				delete(r.seen, k)
			}
		}
		if len(r.seen) > rejectionDedupMax {
			r.seen = make(map[rejectionKey]rejectionSeen)
		}
	}
	return true
}
