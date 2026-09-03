// Package auth 负责入口认证：区分"客户端→Gateway"的凭据校验。
//
// MVP 使用静态 API Key 集合承载；Identity.Subject 供后续授权（Policy）使用。
// 凭据只用于校验，绝不打印或返回明文。
package auth

import (
	"context"
	"strings"

	"github.com/xmj128/mcp-conductor/internal/errs"
)

// Identity 是认证通过后的调用主体。
type Identity struct {
	Subject string // 如 api-key 的名称或 "anonymous"
}

// Authenticator 校验凭据并返回调用主体。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Identity, error)
}

// StaticKeys 是基于静态 API Key 列表的认证器。
//
// 未启用 auth 时，任意（或空）凭据都通过，主体为 anonymous。
type StaticKeys struct {
	enabled bool
	keys    map[string]string // api-key -> subject
	// tokenExtractor 从请求上下文提取原始凭据，便于测试注入。
}

// NewStaticKeys 创建静态 Key 认证器。
func NewStaticKeys(enabled bool, keys []string) *StaticKeys {
	m := make(map[string]string, len(keys))
	for _, key := range keys {
		subject := key
		if i := strings.Index(key, ":"); i > 0 {
			subject = key[:i]
			key = key[i+1:]
		}
		m[key] = subject
	}
	return &StaticKeys{enabled: enabled, keys: m}
}

// Authenticate 校验 token；禁用认证时始终通过。
func (s *StaticKeys) Authenticate(_ context.Context, token string) (*Identity, error) {
	if !s.enabled {
		return &Identity{Subject: "anonymous"}, nil
	}
	subject, ok := s.keys[token]
	if !ok {
		return nil, errs.New(errs.CodeAuthentication, "无效的 API Key")
	}
	return &Identity{Subject: subject}, nil
}
