package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// ServeStdio 以 MCP stdio 传输方式运行一个最小 MCP Server：从 stdin 依次读取
// 换行分隔的 JSON-RPC 请求并把响应写回 stdout（通知无 id，不会得到响应）。
// 与 HTTP Handler 共用同一套方法分发（initialize/tools/list/tools/call），
// stderr 由调用方自行指定或忽略。用于演示上游与测试（供客户端进程调用），
// 非本网关对外能力。
func ServeStdio(ctx context.Context, service ToolService, stdin io.Reader, stdout io.Writer) error {
	handler := NewHandler(service)
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64<<10), stdioMaxMessage)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			// 单条消息解析失败：回一个解析错误并继续（不中断进程）。
			if wErr := writeStdioMessage(stdout, nil, nil, &RPCError{Code: codeParseError, Message: "无效的 JSON-RPC 请求"}); wErr != nil {
				return wErr
			}
			continue
		}
		if req.JSONRPC != "2.0" {
			if wErr := writeStdioMessage(stdout, &req.ID, nil, &RPCError{Code: codeInvalidRequest, Message: "jsonrpc 必须为 2.0"}); wErr != nil {
				return wErr
			}
			continue
		}
		if req.ID == nil {
			// 通知（含 notifications/initialized）不回响应。
			continue
		}
		result, rpcErr := handler.dispatch(ctx, req.Method, req.Params)
		if err := writeStdioMessage(stdout, &req.ID, result, rpcErr); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// writeStdioMessage 写一条 JSON-RPC 响应（含换行）；result 与 rpcErr 二选一。
func writeStdioMessage(w io.Writer, id *json.RawMessage, result any, rpcErr *RPCError) error {
	var idVal any
	if id != nil {
		_ = json.Unmarshal(*id, &idVal)
	}
	return json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      idVal,
		Result:  result,
		Error:   rpcErr,
	})
}
