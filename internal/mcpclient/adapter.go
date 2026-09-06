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
	// 同一逻辑 Server 的所有实例共享这套上游凭证。nil 表示不注入。
	headerFor func(ctx context.Context, server model.Server) map[string]string
}

// New 创建适配器（无凭据注入）。
func New() *Adapter {
	return &Adapter{}
}

// WithHeaderFor 配置按逻辑 Server 组装额外 header 的回调（由应用层注入凭据）。
func (a *Adapter) WithHeaderFor(fn func(ctx context.Context, server model.Server) map[string]string) *Adapter {
	a.headerFor = fn
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
	if _, err := c.Initialize(ctx); err != nil {
		return nil, errs.Wrap(codeForError(err), err, "与 Server %q 实例 %q 握手失败", server.Name, instance.Endpoint)
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
	init, err := c.Initialize(ctx)
	if err != nil {
		return nil, errs.Wrap(codeForError(err), err, "与 Server %q 实例 %q 握手失败", server.Name, instance.Endpoint)
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
	result, err := c.CallTool(ctx, tool, arguments)
	if err != nil {
		return nil, errs.Wrap(codeForError(err), err, "调用工具 %q 失败", tool)
	}
	if result.IsError {
		// 上游把工具执行失败以 isError 结果返回：把回显正文放到底层 Err
		// （客户端 err.Error() 仍可见、便于诊断），对外 Message 保持固定
		// 短语，避免完整正文写入调用日志错误列（隐私/体积）。
		text := firstText(result.Content)
		return nil, errs.Wrap(errs.CodeUpstream, errors.New(text), "上游工具执行失败")
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
	if _, err := c.Initialize(ctx); err != nil {
		return model.ServerStatusUnhealthy, err
	}
	return model.ServerStatusHealthy, nil
}

// dial 选择传输方式并为给定实例构建客户端；header 合并顺序为 Server 级凭据
// 优先、extraHeaders（工具级）后置从而覆盖同名头。
func (a *Adapter) dial(ctx context.Context, server model.Server, instance model.Instance, extraHeaders map[string]string) (*mcp.HTTPClient, error) {
	switch instance.Transport {
	case "", model.TransportStreamableHTTP, model.TransportSSE:
		// 支持的 HTTP 传输族：纯 streamable HTTP + SSE 片段兼容解析。
	case model.TransportStdio:
		return nil, errs.New(errs.CodeInternal, "Transport %q 暂未接入（stdlib 子进程由后续版本提供）", instance.Transport)
	default:
		return nil, errs.New(errs.CodeInternal, "未知传输方式 %q", instance.Transport)
	}
	var opts []mcp.Option
	if a.headerFor != nil {
		for k, v := range a.headerFor(ctx, server) {
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
