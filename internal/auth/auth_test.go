package auth

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xmj128/mcp-conductor/internal/config"
	"github.com/xmj128/mcp-conductor/internal/errs"
	"github.com/xmj128/mcp-conductor/internal/model"
)

// testSecretHex 是 16 字节 HMAC 密钥（32 位 hex）。
const testSecretHex = "00112233445566778899aabbccddeeff"

// testOperatorToken 是测试用控制面管理令牌。
const testOperatorToken = "op-secret-123456"

// memoryKeyStore 是测试用最小 KeyStore（按哈希索引）。
type memoryKeyStore struct {
	byHash map[string]*model.AccessKey
}

func (m *memoryKeyStore) GetAccessKeyByKeyHash(_ context.Context, keyHash string) (*model.AccessKey, error) {
	k, ok := m.byHash[keyHash]
	if !ok {
		return nil, fmt.Errorf("access key %q 不存在", keyHash)
	}
	return k, nil
}

// seedKeyStore 以 subject:key 格式预置数据面 API Key 到存储（含哈希）。
func seedKeyStore(subjects ...string) *memoryKeyStore {
	st := &memoryKeyStore{byHash: map[string]*model.AccessKey{}}
	for _, entry := range subjects {
		name := entry
		key := entry
		if i := strings.IndexByte(entry, ':'); i > 0 {
			name, key = entry[:i], entry[i+1:]
		}
		k := &model.AccessKey{
			ID:      "key-test-" + name,
			Name:    name,
			Subject: name,
			Enabled: true,
			KeyHash: hashAccessKey(secretBytes(), key),
		}
		st.byHash[k.KeyHash] = k
	}
	return st
}

// secretBytes 返回 testSecretHex 解码后的 HMAC 密钥字节。
func secretBytes() []byte {
	b, err := hex.DecodeString(testSecretHex)
	if err != nil {
		panic(err)
	}
	return b
}

func testService(t *testing.T, enabled bool) *Service {
	t.Helper()
	cfg := config.AuthConfig{
		Enabled:     enabled,
		TokenSecret: testSecretHex,
		SessionTTL:  24 * time.Hour,
	}
	if enabled {
		cfg.OperatorToken = testOperatorToken
	}
	svc, err := NewService(cfg, seedKeyStore("client-a:client-key-secret"))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestOperatorLoginAndSession(t *testing.T) {
	svc := testService(t, true)
	session, err := svc.Login(context.Background(), "", testOperatorToken)
	if err != nil {
		t.Fatalf("operator Login 失败: %v", err)
	}
	if session.Token == "" || session.Subject != "operator" {
		t.Fatalf("会话不正确: %+v", session)
	}

	identity, err := svc.Authenticate(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Authenticate 会话令牌失败: %v", err)
	}
	if identity.Subject != "operator" || !identity.Operator || identity.Key != nil {
		t.Fatalf("会话令牌应视为 Operator 身份: %+v", identity)
	}
}

func TestOperatorTokenAuthenticateDirect(t *testing.T) {
	svc := testService(t, true)
	identity, err := svc.Authenticate(context.Background(), testOperatorToken)
	if err != nil {
		t.Fatalf("管理令牌应直接放行: %v", err)
	}
	if identity.Subject != "operator" || !identity.Operator || identity.Key != nil {
		t.Fatalf("管理令牌应得到 Operator 身份（无 Key）: %+v", identity)
	}
}

func TestLoginWrongOperatorToken(t *testing.T) {
	svc := testService(t, true)
	if _, err := svc.Login(context.Background(), "admin", "wrong"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("错误管理令牌应拒绝，得到 %v", err)
	}
}

func TestAccessKeyNotUsableForLogin(t *testing.T) {
	svc := testService(t, true)
	// 数据面 API Key 明文不能换取控制面会话（防提权）。
	if _, err := svc.Login(context.Background(), "", "client-key-secret"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("AccessKey 明文用于登录应被拒绝，得到 %v", err)
	}
	if _, err := svc.Login(context.Background(), "client-a", "client-key-secret"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("带用户名提交 AccessKey 明文登录也应被拒绝，得到 %v", err)
	}
}

func TestAuthenticateAPIKeyIsDataPlane(t *testing.T) {
	svc := testService(t, true)
	identity, err := svc.Authenticate(context.Background(), "client-key-secret")
	if err != nil {
		t.Fatalf("数据面 API Key 应放行: %v", err)
	}
	if identity.Subject != "client-a" {
		t.Fatalf("API Key 主体应为 client-a，得到 %q", identity.Subject)
	}
	if identity.Key == nil {
		t.Fatal("API Key 认证应附带完整的 AccessKey 配置")
	}
	if identity.Operator {
		t.Fatal("数据面 API Key 不应被视为 Operator 身份")
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	svc := testService(t, true)
	if _, err := svc.Authenticate(context.Background(), "unknown-key"); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("未知 key 应拒绝，得到 %v", err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	svc := testService(t, true)
	session, _ := svc.Login(context.Background(), "", testOperatorToken)
	tampered := session.Token[:len(session.Token)-4] + "beef"
	if _, err := svc.Authenticate(context.Background(), tampered); err == nil {
		t.Fatal("篡改令牌应被拒绝")
	}
}

func TestWrongSecretRejected(t *testing.T) {
	svc := testService(t, true)
	session, _ := svc.Login(context.Background(), "", testOperatorToken)
	other, _ := NewService(config.AuthConfig{
		Enabled:       true,
		OperatorToken: testOperatorToken,
		TokenSecret:   "ffeeddccbbaa99887766554433221100",
		SessionTTL:    24 * time.Hour,
	}, seedKeyStore("client-a:client-key-secret"))
	if _, err := other.Authenticate(context.Background(), session.Token); err == nil {
		t.Fatal("错误密钥应拒绝令牌")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	svc := testService(t, true)
	// 用内部 sign 构造已过期令牌。
	token, err := svc.sign("operator", time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), token); !errs.Is(err, errs.CodeAuthentication) {
		t.Fatalf("过期令牌应拒绝，得到 %v", err)
	}
}

func TestDisabledAuthAnonymousIsOperator(t *testing.T) {
	svc := testService(t, false)
	identity, err := svc.Authenticate(context.Background(), "")
	if err != nil || identity.Subject != "anonymous" || !identity.Operator {
		t.Fatalf("认证关闭应匿名放行且视为 Operator: %v / %+v", err, identity)
	}
	session, err := svc.Login(context.Background(), "admin", "anything")
	if err != nil || session.Token != "" {
		t.Fatalf("认证关闭时登录返回空令牌: %v / %+v", err, session)
	}
}
