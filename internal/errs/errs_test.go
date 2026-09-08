package errs

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// fakeTimeoutErr 实现 net.Error 且 Timeout() 为 true，模拟网络层超时。
type fakeTimeoutErr struct{}

func (fakeTimeoutErr) Error() string   { return "i/o timeout" }
func (fakeTimeoutErr) Timeout() bool   { return true }
func (fakeTimeoutErr) Temporary() bool { return true }

func TestIsTimeout(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"deadline wrapped", Wrap(CodeUpstream, context.DeadlineExceeded, "调用超时"), true},
		{"net.Error timeout", Wrap(CodeUpstream, fakeTimeoutErr{}, "连接超时"), true},
		{"client cancel 不算超时", context.Canceled, false},
		{"普通错误", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsTimeout(c.err); got != c.want {
				t.Fatalf("IsTimeout() = %v, want %v", got, c.want)
			}
		})
	}
}

// causeTestErr 同时实现 CauseSummary，模拟 mcp 层“只给摘要、不给响应体”的错误。
type causeTestErr struct{ summary string }

func (e causeTestErr) Error() string        { return e.summary }
func (e causeTestErr) CauseSummary() string { return e.summary }

// fakeNetErr 实现 net.Error 且非超时，模拟普通网络层失败。
type fakeNetErr struct{ msg string }

func (e fakeNetErr) Error() string   { return e.msg }
func (e fakeNetErr) Timeout() bool   { return false }
func (e fakeNetErr) Temporary() bool { return false }

func TestRedactEndpoint(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"去除 userinfo 与 query", "https://user:pass@host:8443/path?token=secret&x=1#frag", "https://host:8443/path"},
		{"无凭据保持原样", "http://h:9000/mcp", "http://h:9000/mcp"},
		{"stdio 命令不过滤", `/usr/local/bin/srv --token abc`, `/usr/local/bin/srv --token abc`},
		{"非法 URL 原样返回", "://bad", "://bad"},
		{"空串原样返回", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactEndpoint(c.in); got != c.want {
				t.Fatalf("RedactEndpoint(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSafeCause(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"超时归因", Wrap(CodeUpstream, fakeTimeoutErr{}, "调用超时"), "连接上游超时"},
		{"CauseSummary 优先于通用网络分类", Wrap(CodeUpstream, causeTestErr{"上游返回 HTTP 状态 401"}, "握手失败"), "上游返回 HTTP 状态 401"},
		{"DNS 解析失败", Wrap(CodeUpstream, &net.DNSError{Err: "no such host", Name: "x"}, "握手失败"), "域名解析失败"},
		{"其余网络错误", Wrap(CodeUpstream, fakeNetErr{msg: "dial tcp refused"}, "握手失败"), "网络错误（无法连接上游）"},
		{"回退统一错误 Message", Wrap(CodeInternal, errors.New("存储读失败"), "读取 Server 凭据失败"), "读取 Server 凭据失败"},
		{"无已识别根因返回空", errors.New("boom"), ""},
		{"nil 返回空", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SafeCause(c.err); got != c.want {
				t.Fatalf("SafeCause() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSafeMessage(t *testing.T) {
	t.Run("取统一错误 Message 不含根因", func(t *testing.T) {
		err := Wrap(CodeUpstream, errors.New("root-cause: 明细"), "调用工具 %q 失败", "search")
		if got := SafeMessage(err); got != `调用工具 "search" 失败` {
			t.Fatalf("SafeMessage() = %q", got)
		}
	})
	t.Run("非统一错误返回通用文案", func(t *testing.T) {
		if got := SafeMessage(errors.New("boom")); got != "调用失败" {
			t.Fatalf("SafeMessage() = %q, want 调用失败", got)
		}
	})
	t.Run("折叠空白并截断", func(t *testing.T) {
		long := strings.Repeat("a", 300)
		err := New(CodeInternal, "%s  \n  %s", long, long)
		got := SafeMessage(err)
		if len([]rune(got)) > maxSafeMessageLen+1 { // +1 省略号
			t.Fatalf("SafeMessage 长度 %d 超过上限", len([]rune(got)))
		}
		if strings.Contains(got, "\n") || strings.Contains(got, "  ") {
			t.Fatalf("SafeMessage 未折叠空白: %q", got)
		}
		if !strings.HasSuffix(got, "…") {
			t.Fatalf("超长应截断并追加省略号: %q", got)
		}
	})
}
