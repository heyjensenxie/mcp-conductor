package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/heyjensenxie/mcp-conductor/internal/auth"
	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
)

// KeyCallResult 是「按某 API Key 身份试调用」的诊断结果。与真实数据面 tools/call
// 走同一授权→路由→均衡→上游链路，但**不写 metrics/调用日志**（Operator 触发的
// 诊断，避免污染真实遥测），也不经 per-key 限流（HTTP 层身份是 Operator）。
type KeyCallResult struct {
	// Allowed 表示该 Key 是否被授权调用该工具。false 时 ErrorCode/Message 为
	// 授权拒绝原因（HTTP 层映射 403），其余字段为空。
	Allowed bool `json:"allowed"`
	// IsError 表示授权通过但本次调用失败（route/upstream/timeout 等）。
	IsError    bool   `json:"is_error,omitempty"`
	ServerID   string `json:"server_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"` // 实际被均衡选中的实例
	LatencyMS  int64  `json:"latency_ms"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Content    string `json:"content,omitempty"`
}

// KeyCallService 是以某 AccessKey 身份执行端到端试调用的能力（由 *MCPGateway
// 满足，Control 通过依赖注入持有；nil 表示未装配）。
type KeyCallService interface {
	CallAsKey(ctx context.Context, key *model.AccessKey, name string, arguments map[string]any) *KeyCallResult
}

// CallAsKey 以指定 AccessKey（无需明文密钥，后台直接构造其身份）执行一次完整
// 数据面调用：授权（key×tool 白名单）→ grant 参数/请求头合并 → 路由 → 负载
// 均衡 → 上游调用。用于 Operator 在 Console 验证某 Key 的细粒度授权与端到端
// 可用性。
//
// 刻意与 CallTool 的观测路径解耦（镜像其数据面管线但**不调用 recorder/metrics**，
// 避免把诊断流量计成真实调用）；关键步骤与 CallTool 保持同步，改动 CallTool 时
// 需同步此处。per-key 限流属 HTTP 中间件（数据面），Operator 触发不走该路径。
func (g *MCPGateway) CallAsKey(ctx context.Context, key *model.AccessKey, name string, arguments map[string]any) *KeyCallResult {
	start := time.Now()
	callFail := func(code errs.Code, err error) *KeyCallResult {
		return &KeyCallResult{
			Allowed:   true,
			IsError:   true,
			ErrorCode: string(code),
			Message:   errs.SafeMessage(err),
			LatencyMS: time.Since(start).Milliseconds(),
		}
	}

	// 1. 授权（与 CallTool 一致：Key 走白名单）。未授权 = 未放行（HTTP 403）。
	identity := &auth.Identity{Subject: key.Subject, Key: key}
	if err := g.authorizer.Authorize(ctx, identity, name); err != nil {
		return &KeyCallResult{
			ErrorCode: string(errs.CodeOf(err)),
			Message:   errs.SafeMessage(err),
			LatencyMS: time.Since(start).Milliseconds(),
		}
	}

	// 2. 路由解析。
	resolved, err := g.resolver.Resolve(ctx, name)
	if err != nil {
		return callFail(errs.CodeOf(err), errs.Wrap(errs.CodeOf(err), err, "路由解析失败"))
	}

	// 3. 负载均衡（与 CallTool 相同：真实选择健康实例）。
	target, err := g.balancer.Pick(ctx, resolved.Targets)
	if err != nil {
		return callFail(errs.CodeRoute, errs.Wrap(errs.CodeRoute, err, "无可用上游实例"))
	}
	dialInstance := model.Instance{
		ID:        target.ID,
		ServerID:  target.ServerID,
		Endpoint:  target.Endpoint,
		Transport: target.Transport,
		Args:      target.Args,
	}

	// 4. 并发上限（与 CallTool 一致，诊断也受网关整体并发约束）。
	if g.sem != nil {
		select {
		case g.sem <- struct{}{}:
			defer func() { <-g.sem }()
		default:
			return callFail(errs.CodeRateLimit, errs.New(errs.CodeRateLimit, "网关并发已满，请稍后重试"))
		}
	}

	// 5. 调用配置：合并该工具 grant 的固定参数与该工具附加请求头（鉴权）。
	targetArgs := arguments
	var extraHeaders map[string]string
	if grant := key.GrantFor(name); grant != nil {
		targetArgs = model.MergeArguments(grant.DefaultArgs, arguments)
		extraHeaders = grant.Headers
	}

	// 6. 上游调用（带超时）。
	callCtx, cancel := context.WithTimeout(ctx, g.upstreamTimeout)
	defer cancel()
	contents, callErr := g.caller.Call(callCtx, resolved.Server, dialInstance, resolved.Tool.OriginalName, targetArgs, extraHeaders)

	latency := time.Since(start).Milliseconds()
	if callErr != nil {
		return &KeyCallResult{
			Allowed:    true,
			IsError:    true,
			ServerID:   resolved.Server.ID,
			InstanceID: target.ID,
			LatencyMS:  latency,
			ErrorCode:  string(errs.CodeOf(callErr)),
			Message:    errs.SafeMessage(callErr),
		}
	}
	return &KeyCallResult{
		Allowed:    true,
		ServerID:   resolved.Server.ID,
		InstanceID: target.ID,
		LatencyMS:  latency,
		Content:    joinContent(contents),
	}
}

// joinContent 拼接上游文本内容（诊断场景只展示文本，不落库）。
func joinContent(blocks []registry.CallContent) string {
	if len(blocks) == 0 {
		return ""
	}
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(block.Text)
	}
	return b.String()
}

// handleKeyInvoke 控制面「以某 API Key 身份试调用」：Operator 触发（/api 控制面
// 已要求 Operator 身份），后台用已存 Key 构造身份跑完整数据面链路，验证白名单
// 授权与端到端可用性。禁用 key / 未授权工具 → 403；其余失败 → 200 + is_error。
func (c *Control) handleKeyInvoke(w http.ResponseWriter, r *http.Request) {
	if c.keyCall == nil {
		writeGatewayError(w, r, http.StatusInternalServerError,
			errs.New(errs.CodeInternal, "按 Key 试调用能力未装配"))
		return
	}
	key, err := c.store.GetAccessKey(r.Context(), r.PathValue("id"))
	if err != nil {
		writeGatewayError(w, r, statusForError(err), err)
		return
	}
	if !key.Enabled {
		writeGatewayError(w, r, http.StatusForbidden,
			errs.New(errs.CodeAuthorization, "API Key %q 已禁用，无法试调用", key.Subject))
		return
	}
	var body struct {
		GatewayTool string         `json:"gateway_tool"`
		Arguments   map[string]any `json:"arguments"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.Wrap(errs.CodeInvalidArgument, err, "请求体无效"))
		return
	}
	if strings.TrimSpace(body.GatewayTool) == "" {
		writeGatewayError(w, r, http.StatusBadRequest,
			errs.New(errs.CodeInvalidArgument, "gateway_tool 不能为空"))
		return
	}

	res := c.keyCall.CallAsKey(r.Context(), key, body.GatewayTool, body.Arguments)
	if !res.Allowed {
		// 授权失败（key 无该工具权限等）：明确 403，不上抛裸上游/内部文本。
		writeGatewayError(w, r, http.StatusForbidden,
			errs.New(errs.CodeAuthorization, "%s", res.Message))
		return
	}
	writeOK(w, RequestIDFrom(r.Context()), res)
}

// 保证 *MCPGateway 满足 KeyCallService，装配期即发现实现漂移。
var _ KeyCallService = (*MCPGateway)(nil)
