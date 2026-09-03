// Package errs 定义统一错误模型。
//
// 基本原则：不把 panic、SQL error、stack trace 或内部函数名直接返回给前端。
// API 层向外返回 code + message + request_id；服务端日志记录完整上下文。
package errs

import (
	"errors"
	"fmt"
)

// Code 是稳定、可枚举的错误码，对外契约的一部分。
type Code string

const (
	// CodeProtocol 表示 MCP 协议校验失败。
	CodeProtocol Code = "protocol_error"
	// CodeAuthentication 表示认证失败（凭据缺失/无效）。
	CodeAuthentication Code = "authentication_error"
	// CodeAuthorization 表示授权失败（策略拒绝）。
	CodeAuthorization Code = "authorization_error"
	// CodeRateLimit 表示触发限流/并发上限。
	CodeRateLimit Code = "rate_limit_error"
	// CodeRoute 表示无法解析目标 Server（路由失败）。
	CodeRoute Code = "route_error"
	// CodeUpstream 表示上游 MCP Server 返回错误。
	CodeUpstream Code = "upstream_error"
	// CodeTimeout 表示上游调用超时。
	CodeTimeout Code = "timeout_error"
	// CodeInvalidArgument 表示请求参数非法。
	CodeInvalidArgument Code = "invalid_argument"
	// CodeNotFound 表示资源不存在。
	CodeNotFound Code = "not_found"
	// CodeInternal 表示内部未知错误，对外不暴露细节。
	CodeInternal Code = "internal_error"
)

// Error 是统一错误类型，携带对外 code、面向运维的 message 与可选的底层包装。
type Error struct {
	Code    Code
	Message string
	Err     error // 内部原因，仅用于日志，不暴露给前端
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 暴露底层错误，便于 errors.Is/As 链路判断。
func (e *Error) Unwrap() error { return e.Err }

// New 构造一个无底层包装的统一错误。
func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap 构造一个携带底层原因的统一错误。
func Wrap(code Code, err error, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Err:     err,
	}
}

// Is 报告 err 是否为统一错误且 code 匹配；便于调用方按类别分流处理。
func Is(err error, code Code) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// CodeOf 提取 err 对应的错误码；非统一错误一律归为 Internal。
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}
