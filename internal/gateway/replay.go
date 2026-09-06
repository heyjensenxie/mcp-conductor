package gateway

import (
	"context"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/storage"
)

// TrafficReplayResult 是「回放捕获调用」的诊断结果：把记录行当时的 tools/call
// 入参重发到其命中的上游实例。刻意与数据面解耦：直连实例（不走 route/均衡/
// 鉴权/限流），且**不写 metrics/调用日志**（Operator 触发的诊断，避免污染遥测）。
type TrafficReplayResult struct {
	ServerID   string `json:"server_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"` // 实际命中的实例（原实例不可调则回退）
	LatencyMS  int64  `json:"latency_ms"`
	IsError    bool   `json:"is_error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Content    string `json:"content,omitempty"`
}

// ReplayService 是把某条调用日志回放到上游的能力（由 app 注入 Control；
// nil 表示未装配，handleReplayLog 返回 500）。
type ReplayService interface {
	Replay(ctx context.Context, id int64, timeout time.Duration) (*TrafficReplayResult, error)
}

// replayService 是 ReplayService 的默认实现：以存储中的入参 + 记录行归属的
// Server/实例直连上游复现。仅携带 Server 级凭据（app 已装配 headerFor）；
// grant 级请求头/固定参数不落库、不重放（见 README 注明）。
type replayService struct {
	store          storage.Store
	caller         ToolCaller
	defaultTimeout time.Duration
}

// NewReplayService 创建调用回放服务。
func NewReplayService(store storage.Store, caller ToolCaller, defaultTimeout time.Duration) ReplayService {
	return &replayService{store: store, caller: caller, defaultTimeout: defaultTimeout}
}

// Replay 回放一条捕获调用。结构性失败（行不存在/无入参/Server 不存在）返回 error
// 供 HTTP 层映射；目标不可达与上游调用失败则返回 is_error 结果（200 + 脱敏码）。
func (s *replayService) Replay(ctx context.Context, id int64, timeout time.Duration) (*TrafficReplayResult, error) {
	start := time.Now()

	row, err := s.store.GetTraffic(ctx, id)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "调用日志不存在或不可读")
	}
	if len(row.RequestArgs) == 0 {
		return nil, errs.New(errs.CodeInvalidArgument, "该调用未捕获入参（record_args 未开启或该行被采样丢弃），无法回放")
	}

	server, err := s.store.GetServer(ctx, row.ServerID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeNotFound, err, "读取 Server 失败")
	}
	if !server.Enabled {
		return replayFail(start, server.ID, "", errs.CodeRoute, "Server %q 已禁用，无法回放", server.ID), nil
	}

	// 实例：优先记录行当时命中的实例（仍存在且可调）；否则回退该 Server 首个可调实例。
	inst, err := s.pickCallableInstance(ctx, server, row.InstanceID)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "读取实例失败")
	}
	if inst == nil {
		return replayFail(start, server.ID, "", errs.CodeRoute, "Server %q 无可用实例，无法回放", server.ID), nil
	}

	// 工具：按 gateway_name 还原当前 original_name（改名后仍用最新源名直连）。
	original := row.Tool
	if tools, err := s.store.ListToolsByServer(ctx, server.ID); err == nil {
		for _, t := range tools {
			if t.GatewayName == row.Tool {
				original = t.OriginalName
				break
			}
		}
	}

	if timeout <= 0 {
		timeout = s.defaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	contents, callErr := s.caller.Call(callCtx, *server, *inst, original, row.RequestArgs, nil)

	latency := time.Since(start).Milliseconds()
	if callErr != nil {
		return &TrafficReplayResult{
			ServerID: server.ID, InstanceID: inst.ID, LatencyMS: latency,
			IsError: true, ErrorCode: string(errs.CodeOf(callErr)), Message: errs.SafeMessage(callErr),
		}, nil
	}
	return &TrafficReplayResult{
		ServerID: server.ID, InstanceID: inst.ID, LatencyMS: latency, Content: joinContent(contents),
	}, nil
}

// pickCallableInstance 优先取记录行当时命中的实例；它不存在/被禁用/不健康时回退
// 到该 Server 首个可调实例。无可调实例返回 (nil, nil)。
func (s *replayService) pickCallableInstance(ctx context.Context, server *model.Server, preferID string) (*model.Instance, error) {
	if preferID != "" {
		if inst, err := s.store.GetInstance(ctx, preferID); err == nil && inst.ServerID == server.ID && inst.IsCallable() {
			return inst, nil
		}
	}
	list, err := s.store.ListInstancesByServer(ctx, server.ID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].IsCallable() {
			return &list[i], nil
		}
	}
	return nil, nil
}

// replayFail 组装结构性失败为 is_error 结果（HTTP 仍 200，镜像 key-invoke 语义）。
func replayFail(start time.Time, serverID, instanceID string, code errs.Code, format string, args ...any) *TrafficReplayResult {
	return &TrafficReplayResult{
		ServerID: serverID, InstanceID: instanceID,
		LatencyMS: time.Since(start).Milliseconds(),
		IsError:   true, ErrorCode: string(code),
		Message: errs.New(code, format, args...).Error(),
	}
}

// 保证 *replayService 满足 ReplayService，装配期即发现实现漂移。
var _ ReplayService = (*replayService)(nil)
