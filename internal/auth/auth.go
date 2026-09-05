// Package auth 负责入口认证与登录会话。
//
// 凭据维度：
//   - 管理令牌（auth.operator_token）：Console/控制面唯一凭据。认证通过后身份
//     Operator=true，可访问 /api 控制面；也可登录换取会话令牌。
//   - API Key（AccessKey 领域实体），密钥以 HMAC-SHA256 哈希落库，明文仅在
//     创建时返回一次；仅用于数据面 /mcp 调用（按 key×工具白名单授权），
//     不能访问 /api 控制面，也不能用来登录。
//   - 登录会话令牌（HMAC 签名，含过期时间），由 operator token 登录签发。
//
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
	"github.com/xmj128/mcp-conductor/internal/model"
)

// Identity 是认证通过后的调用主体。
type Identity struct {
	Subject string           // 如 api-key 名称 / "operator" / 登录用户，或 "anonymous"
	Key     *model.AccessKey // API Key 调用方完整配置（以 API Key 认证时非空）
	// Operator 标记控制面身份（operator token / 会话令牌；认证关闭时匿名放行）。
	// 仅 Operator 身份可访问 /api 控制面；普通 API Key（Key 非空）是数据面身份。
	Operator bool
}

// Authenticator 校验凭据并返回调用主体。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Identity, error)
}

// Session 是登录成功后下发的会话。
type Session struct {
	Token     string    `json:"token"`
	Subject   string    `json:"subject"`
	ExpiresAt time.Time `json:"expires_at"`
}

// KeyStore 是认证依赖的最小存储能力（由 storage.AccessKeyStore 满足）；
// 仅需按密钥哈希反查 AccessKey（登录已收敛为 operator token，不再按 subject 登录）。
type KeyStore interface {
	GetAccessKeyByKeyHash(ctx context.Context, keyHash string) (*model.AccessKey, error)
}

// ---- 登录会话令牌 ----

// tokenPrefix 会话令牌前缀，便于与 API Key 区分。
const tokenPrefix = "mc1."

// tokenTTLMargin 校验时的过期容差，避免临界时刻误判。
const tokenTTLMargin = 30 * time.Second

// Service 是认证门面：管理令牌/API Key 校验 + 登录会话签发/校验。
//
// 认证关闭时仅承载匿名放行路径（匿名亦视为 Operator，保持 /api 开箱即用）。
// API Key 的校验基于 AccessKey 存储（按密钥哈希查询）；keyHash 使用
// auth.token_secret 作为 HMAC 密钥，更换 token_secret 会导致既有 key 哈希失配
// （需重置 key）。
type Service struct {
	enabled       bool
	keys          KeyStore
	secret        []byte // token_secret：签发会话 + 计算 API Key 哈希
	operatorToken []byte // operator_token：控制面管理凭据
	ttl           time.Duration
}

// NewService 由配置与 key 存储构建认证服务。
func NewService(cfg config.AuthConfig, keys KeyStore) (*Service, error) {
	var secret, operatorToken []byte
	if cfg.Enabled {
		if strings.TrimSpace(cfg.TokenSecret) == "" {
			return nil, errors.New("auth.enabled 时须配置 auth.token_secret")
		}
		s, err := hex.DecodeString(cfg.TokenSecret)
		if err != nil || len(s) < 16 {
			return nil, errors.New("auth.token_secret 须为 32 位 hex（至少 16 字节密钥）")
		}
		secret = s
		if strings.TrimSpace(cfg.OperatorToken) == "" {
			return nil, errors.New("auth.enabled 时须配置 auth.operator_token（控制面管理凭据）")
		}
		operatorToken = []byte(cfg.OperatorToken)
	}
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &Service{
		enabled:       cfg.Enabled,
		keys:          keys,
		secret:        secret,
		operatorToken: operatorToken,
		ttl:           ttl,
	}, nil
}

// Enabled 报告认证是否开启。
func (s *Service) Enabled() bool { return s.enabled }

// hashAccessKey 计算 API Key 的 HMAC-SHA256 哈希（hex），用于安全落库与精确定位。
func hashAccessKey(secret []byte, token string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// KeyHash 对外计算某个 API Key 的 HMAC-SHA256 哈希，供创建 AccessKey 落库用。
// tokenSecretHex 需为合法 hex（与 auth.token_secret 一致）。
func KeyHash(tokenSecretHex, token string) (string, error) {
	secret, err := hex.DecodeString(tokenSecretHex)
	if err != nil {
		return "", errs.New(errs.CodeInvalidArgument, "auth.token_secret 不是合法 hex")
	}
	return hashAccessKey(secret, token), nil
}

// operatorSubject 是 operator token 认证后与会话的统一主体标识。
const operatorSubject = "operator"

// Login 校验管理令牌并签发会话令牌。
// password 必须与 auth.operator_token 常量时间相等；API Key 明文不能用于登录
// （避免把数据面凭据升级为控制面/全量工具会话）。
func (s *Service) Login(ctx context.Context, _ string, password string) (*Session, error) {
	ident, err := s.login(ctx, password)
	if err != nil {
		return nil, err
	}
	if !s.enabled {
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

// login 校验登录凭据并返回调用主体；认证关闭时匿名放行（视为 Operator）。
func (s *Service) login(ctx context.Context, password string) (*Identity, error) {
	if !s.enabled {
		return &Identity{Subject: "anonymous", Operator: true}, nil
	}
	if len(s.operatorToken) == 0 || !hmac.Equal([]byte(password), s.operatorToken) {
		return nil, errs.New(errs.CodeAuthentication, "管理令牌错误")
	}
	return &Identity{Subject: operatorSubject, Operator: true}, nil
}

// Authenticate 依次接受：会话令牌 → 管理令牌 → API Key；认证关闭时匿名放行
// （匿名视为 Operator，保持控制面与数据面开箱即用）。
func (s *Service) Authenticate(ctx context.Context, token string) (*Identity, error) {
	if !s.enabled {
		return &Identity{Subject: "anonymous", Operator: true}, nil
	}
	if strings.HasPrefix(token, tokenPrefix) {
		subject, exp, err := s.verify(token)
		if err != nil {
			return nil, errs.New(errs.CodeAuthentication, "会话令牌无效")
		}
		if exp.Before(time.Now().UTC().Add(-tokenTTLMargin)) {
			return nil, errs.New(errs.CodeAuthentication, "会话已过期，请重新登录")
		}
		// 会话令牌只能由 operator token 登录取得，故一律视为控制面身份。
		return &Identity{Subject: subject, Operator: true}, nil
	}
	if token == "" {
		return nil, errs.New(errs.CodeAuthentication, "缺少凭据")
	}
	if len(s.operatorToken) > 0 && hmac.Equal([]byte(token), s.operatorToken) {
		return &Identity{Subject: operatorSubject, Operator: true}, nil
	}
	key, err := s.keys.GetAccessKeyByKeyHash(ctx, hashAccessKey(s.secret, token))
	if err != nil {
		return nil, errs.New(errs.CodeAuthentication, "无效的凭据")
	}
	if !key.Enabled {
		return nil, errs.New(errs.CodeAuthentication, "凭据已禁用")
	}
	return &Identity{Subject: key.Subject, Key: key}, nil
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
