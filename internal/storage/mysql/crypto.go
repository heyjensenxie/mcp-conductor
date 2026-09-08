package mysql

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// credentialCipher 用 AES-256-GCM 对凭证值加解密。
// 密文以 hex 文本保存于 encrypted_value 列，便于 MySQL TEXT 列存储；
// 每次加密使用随机 nonce，输出为 nonce||ciphertext 的 hex 编码。
type credentialCipher struct {
	gcm cipher.AEAD
}

// newCredentialCipher 由 32 字节密钥（64 位 hex）构造 AES-256-GCM。
func newCredentialCipher(keyHex string) (*credentialCipher, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("解析凭证加密密钥失败（应为 64 位 hex）: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("凭证加密密钥必须为 32 字节（AES-256）")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &credentialCipher{gcm: gcm}, nil
}

// encryptHex 把明文加密为 hex 文本；空明文返回空串。
func (c *credentialCipher) encryptHex(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return hex.EncodeToString(sealed), nil
}

// decryptHex 解密 hex 密文；空密文返回空串。
func (c *credentialCipher) decryptHex(sealedHex string) (string, error) {
	if sealedHex == "" {
		return "", nil
	}
	sealed, err := hex.DecodeString(sealedHex)
	if err != nil {
		return "", fmt.Errorf("解析凭据密文失败: %w", err)
	}
	ns := c.gcm.NonceSize()
	if len(sealed) < ns {
		return "", errors.New("凭据密文长度非法")
	}
	plain, err := c.gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("解密凭据失败（密钥可能已更换）: %w", err)
	}
	return string(plain), nil
}

// encryptValue 编码待落库的凭证值：未配置密钥时只允许空值，拒绝明文落库。
func encryptValue(c *credentialCipher, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if c == nil {
		return "", errors.New("未配置 credentials.encryption_key，MySQL 拒绝保存明文凭证")
	}
	return c.encryptHex(value)
}

// decryptValue 解码从库中读出的凭证值。
func decryptValue(c *credentialCipher, sealedHex string) (string, error) {
	if c == nil {
		// 未配置 encryption_key 时理论上不应存在非空密文；若确实存在说明密钥
		// 缺失/被移除，明确报错（fail-closed），避免把"读不出"静默成"没配置"
		// 而匿名请求上游。
		if sealedHex != "" {
			return "", errors.New("未配置 credentials.encryption_key，无法解密已存在的凭证密文")
		}
		return "", nil
	}
	return c.decryptHex(sealedHex)
}
