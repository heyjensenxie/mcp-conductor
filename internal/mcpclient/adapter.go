// Package mcpclient 是外部 MCP Server 的接入适配层。
//
// 把 internal/mcp 的最小协议客户端实现为 Registry 所需的工具发现/调用能力，
// 同时兼作健康检查器（基于 initialize 握手）。上游凭据注入通过 HeaderFor
// 回调由应用层组装，本包不接触明文 Credential。
package mcpclient

import (
	"context"
	"fmt"

	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/mcp"
	"github.com/xmj128/mcp-conductor/internal/model"
	"github.com/xmj128/mcp-conductor/internal/registry"
)

// Adapter 适配上游 MCP Server。
type Adapter struct {
	// headerFor 按 Server 返回调用上游时附加的 header（如注入 API Key / Token）。
	// nil 表示不注入。
	headerFor func(ctx context.Context, server model.Server) map[string]string
}

// New 创建适配器（无凭据注入）。
func New() *Adapter {
	return &Adapter{}
}

// WithHeaderFor 配置按 Server 组装额外 header 的回调（由应用层注入凭据）。
func (a *Adapter) WithHeaderFor(fn func(ctx context.Context, server model.Server) map[string]string) *Adapter {
	a.headerFor = fn
	return a
}

// Discover 连接上游、握手并发现工具。
func (a *Adapter) Discover(ctx context.Context, server model.Server) ([]registry.DiscoveredTool, error) {
	c, err := a.clientFor(ctx, server, nil)
	if err != nil {
		return nil, err
	}
	if _, err := c.Initialize(ctx); err != nil {
		return nil, errs.Wrap(errs.CodeUpstream, err, "与 Server %q 握手失败", server.Name)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeUpstream, err, "发现 Server %q 工具失败", server.Name)
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

// Call 调用上游工具并转换为 Gateway 统一结果。
// extraHeaders 是按工具附加的请求头（如 per-tool 鉴权），与 Server 级
// 凭据（headerFor）合并；同名 header 以工具级为准。
func (a *Adapter) Call(ctx context.Context, server model.Server, tool string, arguments map[string]any, extraHeaders map[string]string) ([]registry.CallContent, error) {
	c, err := a.clientFor(ctx, server, extraHeaders)
	if err != nil {
		return nil, err
	}
	result, err := c.CallTool(ctx, tool, arguments)
	if err != nil {
		return nil, errs.Wrap(errs.CodeUpstream, err, "调用工具 %q 失败", tool)
	}
	if result.IsError {
		text := firstText(result.Content)
		return nil, errs.New(errs.CodeUpstream, "%s", text)
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

// Check 以 initialize 握手结果作为健康结论。
func (a *Adapter) Check(ctx context.Context, server model.Server) (model.ServerStatus, error) {
	c, err := a.clientFor(ctx, server, nil)
	if err != nil {
		return model.ServerStatusUnhealthy, err
	}
	if _, err := c.Initialize(ctx); err != nil {
		return model.ServerStatusUnhealthy, err
	}
	return model.ServerStatusHealthy, nil
}

// clientFor 选择传输方式并构建客户端；header 合并顺序为 Server 级凭据
// 优先、extraHeaders（工具级）后置从而覆盖同名头。
func (a *Adapter) clientFor(ctx context.Context, server model.Server, extraHeaders map[string]string) (*mcp.HTTPClient, error) {
	switch server.Transport {
	case "", model.TransportStreamableHTTP, model.TransportSSE:
		opts := []mcp.Option{}
		if a.headerFor != nil {
			for k, v := range a.headerFor(ctx, server) {
				opts = append(opts, mcp.WithHeader(k, v))
			}
		}
		for k, v := range extraHeaders {
			opts = append(opts, mcp.WithHeader(k, v))
		}
		return mcp.NewHTTPClient(server.Endpoint, opts...), nil
	case model.TransportStdio:
		return nil, errs.New(errs.CodeInternal, "Transport %q 暂未接入（stdlib 子进程由后续版本提供）", server.Transport)
	default:
		return nil, errs.New(errs.CodeInternal, "未知传输方式 %q", server.Transport)
	}
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
