package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
)

// trafficDetail 是单条调用日志的详情（仅此处显式带出入参；列表保持轻量）。
type trafficDetail struct {
	model.TrafficSample
	RequestArgs map[string]any `json:"request_args,omitempty"`
}

// handleGetLogDetail 返回单条调用日志详情（含已捕获入参，供回放弹窗预填/查看）。
func (c *Control) handleGetLogDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTrafficID(w, r)
	if !ok {
		return
	}
	sample, err := c.store.GetTraffic(r.Context(), id)
	if err != nil {
		writeGatewayError(w, r, http.StatusNotFound,
			errs.Wrap(errs.CodeNotFound, err, "调用日志不存在或不可读"))
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), trafficDetail{TrafficSample: *sample, RequestArgs: sample.RequestArgs})
}

// handleReplayLog 把捕获的调用回放到其上游实例（诊断，不写 metrics/调用日志）。
// Operator 触发（/api 已要求 Operator 身份）。is_error 也以 200 返回，镜像
// key-invoke 语义；仅结构性失败（未装配/行不存在/无入参）才走 4xx/5xx。
func (c *Control) handleReplayLog(w http.ResponseWriter, r *http.Request) {
	if c.replay == nil {
		writeGatewayError(w, r, http.StatusInternalServerError,
			errs.New(errs.CodeInternal, "调用回放能力未装配"))
		return
	}
	id, ok := parseTrafficID(w, r)
	if !ok {
		return
	}
	timeout := 10 * time.Second
	var body struct {
		TimeoutMS *int `json:"timeout_ms"`
	}
	if err := decodeBody(r, &body); err != nil && !errors.Is(err, io.EOF) {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if body.TimeoutMS != nil {
		if *body.TimeoutMS <= 0 {
			writeGatewayError(w, r, http.StatusBadRequest,
				errs.New(errs.CodeInvalidArgument, "timeout_ms 须为正整数"))
			return
		}
		if *body.TimeoutMS > 60_000 {
			*body.TimeoutMS = 60_000
		}
		timeout = time.Duration(*body.TimeoutMS) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	res, err := c.replay.Replay(ctx, id, timeout)
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), res)
}

// purgeArgsView 是清除入参的结果：purged 为被清除入参的调用日志行数。
type purgeArgsView struct {
	Purged int64 `json:"purged"`
}

// handlePurgeTrafficArgs 清空全部已捕获入参（隐私收尾：关闭入参捕获后清历史）。
// 幂等可重复：无入参的行本来为 NULL，重复清除返回 0。仅置 request_args 为空，
// 行本身与元数据保留（指标/趋势不受影响）。
func (c *Control) handlePurgeTrafficArgs(w http.ResponseWriter, r *http.Request) {
	n, err := c.store.PurgeTrafficArgs(r.Context())
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), purgeArgsView{Purged: n})
}

// parseTrafficID 解析路径中的数字流水主键，失败时写 400 并返回 false。
func parseTrafficID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.New(errs.CodeInvalidArgument, "调用日志 id 须为正整数"))
		return 0, false
	}
	return id, true
}
