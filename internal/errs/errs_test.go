package errs

import (
	"context"
	"errors"
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
