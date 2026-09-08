package errs

import (
	"errors"
	"net"
	"net/url"
)

// RedactEndpoint 移除 endpoint 中可能携带凭据的部分（URL userinfo、query 串、
// fragment），用于把实例端点安全地写进对外可见的错误文案；stdio 命令等非 URL
// 值原样返回。脱敏只作用于展示，实际连接仍使用完整 Endpoint。
func RedactEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return endpoint
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// causeSummarizer 由可安全展示给用户的错误类型实现（如 mcp.UpstreamHTTPError）。
// 只返回状态/错误码等诊断摘要，绝不包含请求 URL、上游响应体或凭据。
type causeSummarizer interface{ CauseSummary() string }

// SafeCause 提取“可诊断但不泄露”的根因短语，供拼进测试/控制台错误文案：
//   - 超时 → “连接上游超时”；
//   - 实现 CauseSummary 的错误（上游 HTTP 状态 / JSON-RPC 错误码）→ 其摘要；
//   - DNS 解析失败、其余网络错误 → 分类短语；
//   - 回退：链上存在统一错误时取其 Message（已是脱敏文案，如“读取 Server 凭据
//     失败”）；
//   - 无已识别根因返回空串，避免把本地业务错误误标成上游/网络错误。
//
// 顺序上有讲究：先判超时，再判 CauseSummary，随后才是 DNS（DNS 也实现
// net.Error），最后才落到通用网络分类，避免把前几类误判成普通网络错误。
func SafeCause(err error) string {
	if IsTimeout(err) {
		return "连接上游超时"
	}
	if cs, ok := causeSummaryIn(err); ok {
		return cs.CauseSummary()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "域名解析失败"
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return "网络错误（无法连接上游）"
	}
	var e *Error
	if errors.As(err, &e) && e.Message != "" {
		return e.Message
	}
	return ""
}

// causeSummaryIn 沿错误链查找可安全摘要的错误类型；命中且摘要非空才返回。
func causeSummaryIn(err error) (causeSummarizer, bool) {
	var cs causeSummarizer
	if errors.As(err, &cs) && cs.CauseSummary() != "" {
		return cs, true
	}
	return nil, false
}
