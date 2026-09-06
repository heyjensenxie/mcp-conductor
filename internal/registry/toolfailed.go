// Package 级错误标记：区分「上游正常响应但工具执行失败」与「实例/传输故障」。
//
// MCP 的 tools/call 有两种失败形态：
//  1. isError=true 的 JSON-RPC 结果——上游连通、正常响应，只是工具执行本身失败
//     （业务性失败，如参数被上游拒绝）。这不代表实例不可用。
//  2. 传输层失败（连接失败 / 超时 / 请求中途断开）——实例可能已故障。
//
// 前者不应触发实例级负载均衡冷却（避免把一次业务失败误判成实例宕机而摘除）。
// Adapter 在识别到 isError 结果时返回 ToolFailedError，Gateway 据此区分两种失败。
package registry

// ToolFailedError 表示一次上游连通但工具执行失败的调用（MCP isError 结果）。
// 实现 Unwrap 透传底层 errs.Error，使 CodeOf / SafeMessage / errs.Is 等既有
// 错误语义（CodeUpstream、脱敏摘要）不受影响；仅用于上层区分是否实例故障。
type ToolFailedError struct{ err error }

// NewToolFailedError 包装一次上游工具执行失败（业务失败，非实例故障）。
func NewToolFailedError(err error) error {
	return &ToolFailedError{err: err}
}

// Error 透传底层错误文本，便于日志/响应展示。
func (e *ToolFailedError) Error() string { return e.err.Error() }

// Unwrap 暴露底层统一错误，保证既有错误分类（errs.*）仍可穿透。
func (e *ToolFailedError) Unwrap() error { return e.err }
