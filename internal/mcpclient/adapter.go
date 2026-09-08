// Package mcpclient 是外部 MCP Server 的接入适配层。
//
// 把 internal/mcp 的最小协议客户端实现为 Registry 所需的工具发现/调用能力，
// 同时兼作健康检查器（基于 initialize 握手）。上游凭据注入通过 HeaderFor
// 回调由应用层组装，本包不接触明文 Credential。
//
// 拨测目标模型：逻辑 Server 下可能有多个实例（endpoint+transport），发现/调用/
// 健康检查都作用于某个具体实例；Credentials 仍按逻辑 Server 组装并注入到
// 被选中的实例。
package mcpclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/heyjensenxie/mcp-conductor/internal/errs"
	"github.com/heyjensenxie/mcp-conductor/internal/mcp"
	"github.com/heyjensenxie/mcp-conductor/internal/model"
	"github.com/heyjensenxie/mcp-conductor/internal/registry"
)

// Adapter 适配上游 MCP Server。
type Adapter struct {
	// headerFor 按逻辑 Server 返回调用上游时附加的 header（如注入 API Key / Token）。
	// 同一逻辑 Server 的所有实例共享这套上游凭证。返回错误时中止拨测（凭据
	// 读取/解密失败不再匿名盲发）；nil 表示不注入。
	headerFor func(ctx context.Context, server model.Server) (map[string]string, error)
	// stdioEnv 是启动 stdio 子进程时附加的环境变量（追加在当前进程环境之上）。
	// stdio 上游身份凭据由进程自身环境自持，通常经此注入。
	stdioEnv []string
}

// New 创建适配器（无凭据注入）。
func New() *Adapter {
	return &Adapter{}
}

// WithHeaderFor 配置按逻辑 Server 组装额外 header 的回调（由应用层注入凭据）。
// 回调返回错误（如凭据读取/解密失败）时，拨测以内部错误中止，不匿名发送。
func (a *Adapter) WithHeaderFor(fn func(ctx context.Context, server model.Server) (map[string]string, error)) *Adapter {
	a.headerFor = fn
	return a
}

// WithStdioEnv 配置启动 stdio 子进程时附加的环境变量（kv 形式，如 "TOKEN=x"）。
// 供 stdio 上游的身份/配置注入；仅作用于 stdio 传输。
func (a *Adapter) WithStdioEnv(kv ...string) *Adapter {
	a.stdioEnv = append(a.stdioEnv, kv...)
	return a
}

// codeForError 把底层上游错误归类为统一错误码：超时归 CodeTimeout，
// 其余归 CodeUpstream（配合网关把超时从上游错误中区分出来）。
func codeForError(err error) errs.Code {
	if errs.IsTimeout(err) {
		return errs.CodeTimeout
	}
	return errs.CodeUpstream
}

// Discover 连接上游实例、握手并发现工具。
func (a *Adapter) Discover(ctx context.Context, server model.Server, instance model.Instance) ([]registry.DiscoveredTool, error) {
	c, err := a.dial(ctx, server, instance, nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if _, err := c.Initialize(ctx); err != nil {
		return nil, errs.Wrap(codeForError(err), err, "与 Server %q 实例 %q 握手失败", server.Name, errs.RedactEndpoint(instance.Endpoint))
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, errs.Wrap(codeForError(err), err, "发现 Server %q 工具失败", server.Name)
	}
	out := make([]registry.DiscoveredTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, registry.DiscoveredTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out, nil
}

// ProbeResult 是一次评测探测的完整结果：协议握手回显 + 上游工具定义。
// Tools 为 tools/list 的最新上游定义（未被平台覆盖污染）。
type ProbeResult struct {
	ProtocolVersion string
	ServerInfo      mcp.Implementation
	Capabilities    mcp.Capabilities
	Tools           []mcp.Tool
}

// Probe 对某个实例执行一次评测探测：initialize（保留握手结果）+ tools/list。
// 用于 MCP 评测模块获取真实协议版本/serverInfo/capabilities 与上游工具定义。
func (a *Adapter) Probe(ctx context.Context, server model.Server, instance model.Instance) (*ProbeResult, error) {
	c, err := a.dial(ctx, server, instance, nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	init, err := c.Initialize(ctx)
	if err != nil {
		return nil, errs.Wrap(codeForError(err), err, "与 Server %q 实例 %q 握手失败", server.Name, errs.RedactEndpoint(instance.Endpoint))
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, errs.Wrap(codeForError(err), err, "发现 Server %q 工具失败", server.Name)
	}
	return &ProbeResult{
		ProtocolVersion: init.ProtocolVersion,
		ServerInfo:      init.ServerInfo,
		Capabilities:    init.Capabilities,
		Tools:           tools,
	}, nil
}

// Call 调用上游实例上的工具并转换为 Gateway 统一结果。
// extraHeaders 是按工具附加的请求头（如 per-tool 鉴权），与 Server 级
// 凭据（headerFor）合并；同名 header 以工具级为准。
func (a *Adapter) Call(ctx context.Context, server model.Server, instance model.Instance, tool string, arguments map[string]any, extraHeaders map[string]string) ([]registry.CallContent, error) {
	c, err := a.dial(ctx, server, instance, extraHeaders)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	result, err := c.CallTool(ctx, tool, arguments)
	if err != nil {
		var rpcErr *mcp.RPCErrorResponse
		var httpErr *mcp.UpstreamHTTPError
		// 上游可达但拒绝本次调用：JSON-RPC error 响应，或 HTTP 4xx（参数/客户端被拒）——
		// 均非实例故障，与下方 isError 结果同等归类为 ToolFailedError，避免被负载
		// 均衡的失败冷却误判为实例故障而摘除。
		if errors.As(err, &rpcErr) || (errors.As(err, &httpErr) && httpErr.Status >= 400 && httpErr.Status < 500) {
			return nil, registry.NewToolFailedError(errs.Wrap(errs.CodeUpstream, err, "上游工具执行失败"))
		}
		// 其余（连接失败 / 超时 / HTTP 5xx 等）视为传输/实例级故障。
		return nil, errs.Wrap(codeForError(err), err, "调用工具 %q 失败", tool)
	}
	if result.IsError {
		// 上游把工具执行失败以 isError 结果返回：把回显正文放到底层 Err
		// （客户端 err.Error() 仍可见、便于诊断），对外 Message 保持固定
		// 短语，避免完整正文写入调用日志错误列（隐私/体积）。
		// 用 ToolFailedError 标记“业务失败、实例仍健康”，避免被负载均衡
		// 的失败冷却误判为实例故障而摘除。
		text := firstText(result.Content)
		return nil, registry.NewToolFailedError(errs.Wrap(errs.CodeUpstream, errors.New(text), "上游工具执行失败"))
	}
	contents := make([]registry.CallContent, 0, len(result.Content))
	for _, block := range result.Content {
		typ := block.Type
		if typ == "" {
			typ = "text"
		}
		contents = append(contents, registry.CallContent{Type: typ, Text: block.Text})
	}
	if len(contents) == 0 {
		contents = []registry.CallContent{{Type: "text", Text: ""}}
	}
	return contents, nil
}

// Check 以 initialize 握手结果作为某个实例的健康结论。
func (a *Adapter) Check(ctx context.Context, server model.Server, instance model.Instance) (model.ServerStatus, error) {
	c, err := a.dial(ctx, server, instance, nil)
	if err != nil {
		return model.ServerStatusUnhealthy, err
	}
	defer c.Close()
	if _, err := c.Initialize(ctx); err != nil {
		return model.ServerStatusUnhealthy, err
	}
	return model.ServerStatusHealthy, nil
}

// dial 选择传输方式并为给定实例构建客户端；header 合并顺序为 Server 级凭据
// 优先、extraHeaders（工具级）后置从而覆盖同名头。
//
// stdio 传输没有 HTTP 头部概念：上游进程由其自身环境自持凭据，采 用
// spawn-per-dial（每次操作新建并回收子进程），因此 Header 凭据注入对 stdio
// 不适用，仅在 HTTP 传输族上组装。
func (a *Adapter) dial(ctx context.Context, server model.Server, instance model.Instance, extraHeaders map[string]string) (mcp.Client, error) {
	switch instance.Transport {
	case "", model.TransportStreamableHTTP, model.TransportSSE:
		// 支持的 HTTP 传输族：纯 streamable HTTP + SSE 片段兼容解析。
	case model.TransportStdio:
		client := mcp.NewStdioClient(instance.Endpoint, instance.Args)
		if len(a.stdioEnv) > 0 {
			client.WithStdioEnv(a.stdioEnv...)
		}
		return client, nil
	default:
		return nil, errs.New(errs.CodeInternal, "未知传输方式 %q", instance.Transport)
	}
	var opts []mcp.Option
	if a.headerFor != nil {
		headers, err := a.headerFor(ctx, server)
		if err != nil {
			return nil, errs.Wrap(errs.CodeInternal, err, "读取 Server %q 凭据失败", server.Name)
		}
		for k, v := range headers {
			opts = append(opts, mcp.WithHeader(k, v))
		}
	}
	for k, v := range extraHeaders {
		opts = append(opts, mcp.WithHeader(k, v))
	}
	return mcp.NewHTTPClient(instance.Endpoint, opts...), nil
}

// firstText 取首段文本内容，用于错误摘要。
func firstText(blocks []mcp.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return fmt.Sprintf("上游返回 %d 块结果", len(blocks))
}
