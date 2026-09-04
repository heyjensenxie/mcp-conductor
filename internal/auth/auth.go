// Package auth 负责入口认证与登录会话。
//
// 凭据维度：
//   * 静态 API Key（subject:key），用于 Machine-to-Machine / 服务调用；
//   * 登录会话令牌（HMAC 签名，含过期时间），供 Console 用户登录后携带。
// 会话令牌无状态（服务端不存会话），重启/过期即失效；登出由客户端丢弃令牌完成。
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xmj128/mcp-conductor/internal/config"
	"github.com/xmj128/mcp-conductor/internal/errs"
)

// Identity 是认证通过后的调用主体。
type Identity struct {
	Subject string // 如 api-key 名称 / 登录用户，或 "anonymous"
}

// Authenticator 校验凭据并返回调用主体。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Identity, error)
}

// Session 是登录成功后下发的会话。
type Session struct {
	Token     string `json:"token"`
	Subject   string `json:"subject"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ---- 静态 API Key ----

// StaticKeys 是基于静态 API Key 列表的凭据库。
// 未启用 auth 时，任意（或空）凭据都通过，主体为 anonymous。
type StaticKeys struct {
	enabled         bool
	keys            map[string]string // key -> subject
	keyBySubject    map[string]string // subject -> key
}

// NewStaticKeys 解析 subject:key 列表构建凭据库。
func NewStaticKeys(enabled bool, keys []string) *StaticKeys {
	keyIndex := make(map[string]string, len(keys))
	keyBySubject := make(map[string]string, len(keys))
	for _, entry := range keys {
		subject := entry
		key := entry
		if i := strings.Index(entry, ":"); i > 0 {
			subject = entry[:i]
			key = entry[i+1:]
		}
		keyIndex[key] = subject
		keyBySubject[subject] = key
	}
	return &StaticKeys{enabled: enabled, keys: keyIndex, keyBySubject: keyBySubject}
}

// Authenticate 校验 key；禁用认证时始终通过。
func (s *StaticKeys) Authenticate(_ context.Context, key string) (*Identity, error) {
	if !s.enabled {
		return &Identity{Subject: "anonymous"}, nil
	}
	subject, ok := s.keys[key]
	if !ok {
		return nil, errs.New(errs.CodeAuthentication, "无效的凭据")
	}
	return &Identity{Subject: subject}, nil
}

// login 校验用户名/密码：username 留空时 password 视为 API Key；
// 否则按 subject → key 校验。
func (s *StaticKeys) login(username, password string) (*Identity, error) {
	if username == "" {
		return s.Authenticate(context.Background(), password)
	}
	key, ok := s.keyBySubject[username]
	if !ok || key == "" || password != key {
		return nil, errs.New(errs.CodeAuthentication, "用户名或密码错误")
	}
	return &Identity{Subject: username}, nil
}

// ---- 登录会话令牌 ----

// tokenPrefix 会话令牌前缀，便于与静态 Key 区分。
const tokenPrefix = "mc1."

// tokenTTLMargin 校验时的过期容差，避免临界时刻误判。
const tokenTTLMargin = 30 * time.Second

// Service 是认证门面：静态 Key 校验 + 登录会话签发/校验。
type Service struct {
	static *StaticKeys
	secret []byte
	ttl    time.Duration
}

// NewService 由配置构建认证服务；auth 未启用时仅承载匿名放行路径。
func NewService(cfg config.AuthConfig) (*Service, error) {
	var secret []byte
	if cfg.Enabled {
		if strings.TrimSpace(cfg.TokenSecret) == "" {
			return nil, errors.New("auth.enabled 时须配置 auth.token_secret")
		}
		s, err := hex.DecodeString(cfg.TokenSecret)
		if err != nil || len(s) < 16 {
			return nil, errors.New("auth.token_secret 须为 32 位 hex（至少 16 字节密钥）")
		}
		secret = s
	}
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &Service{static: NewStaticKeys(cfg.Enabled, cfg.APIKeys), secret: secret, ttl: ttl}, nil
}

// Enabled 报告认证是否开启。
func (s *Service) Enabled() bool { return s.static.enabled }

// Login 校验用户名密码并签发会话令牌。
func (s *Service) Login(username, password string) (*Session, error) {
	ident, err := s.static.login(username, password)
	if err != nil {
		return nil, err
	}
	if !s.Enabled() {
		// 认证关闭时也允许登录（匿名主体），便于开发环境体验完整流程。
		return &Session{Token: "", Subject: ident.Subject, ExpiresAt: time.Time{}}, nil
	}
	exp := time.Now().UTC().Add(s.ttl)
	token, err := s.sign(ident.Subject, exp)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, err, "签发会话令牌失败")
	}
	return &Session{Token: token, Subject: ident.Subject, ExpiresAt: exp}, nil
}

// Authenticate 依次接受：会话令牌 → 静态 API Key；认证关闭时匿名放行。
func (s *Service) Authenticate(_ context.Context, token string) (*Identity, error) {
	if !s.Enabled() {
		return &Identity{Subject: "anonymous"}, nil
	}
	if strings.HasPrefix(token, tokenPrefix) {
		subject, exp, err := s.verify(token)
		if err != nil {
			return nil, errs.New(errs.CodeAuthentication, "会话令牌无效")
		}
		if exp.Before(time.Now().UTC().Add(-tokenTTLMargin)) {
			return nil, errs.New(errs.CodeAuthentication, "会话已过期，请重新登录")
		}
		return &Identity{Subject: subject}, nil
	}
	return s.static.Authenticate(context.Background(), token)
}

// sign 生成 mc1.<payload>.<sig> 形式的会话令牌。
// payload = base64url(subject) + "." + expUnixMilli。
func (s *Service) sign(subject string, exp time.Time) (string, error) {
	raw := fmt.Sprintf("%s.%d", base64.RawURLEncoding.EncodeToString([]byte(subject)), exp.UnixMilli())
	mac := hmac.New(sha256.New, s.secret)
	if _, err := mac.Write([]byte(raw)); err != nil {
		return "", err
	}
	return tokenPrefix + raw + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// verify 校验签名并解析 subject 与过期时间。
func (s *Service) verify(token string) (string, time.Time, error) {
	body := strings.TrimPrefix(token, tokenPrefix)
	i := strings.LastIndex(body, ".")
	if i <= 0 {
		return "", time.Time{}, errors.New("令牌格式非法")
	}
	raw, sigHex := body[:i], body[i+1:]
	expected := signRaw(s.secret, raw)
	if !hmac.Equal([]byte(expected), []byte(sigHex)) {
		return "", time.Time{}, errors.New("令牌签名不匹配")
	}
	j := strings.IndexByte(raw, '.')
	if j <= 0 {
		return "", time.Time{}, errors.New("令牌载荷非法")
	}
	subB64, expStr := raw[:j], raw[j+1:]
	sub, err := base64.RawURLEncoding.DecodeString(subB64)
	if err != nil {
		return "", time.Time{}, err
	}
	var exp int64
	if _, err := fmt.Sscanf(expStr, "%d", &exp); err != nil {
		return "", time.Time{}, err
	}
	return string(sub), time.UnixMilli(exp), nil
}

// signRaw 计算 payload 的 HMAC-SHA256 hex。
func signRaw(secret []byte, raw string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}