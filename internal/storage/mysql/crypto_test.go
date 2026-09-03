package mysql

import (
	"strings"
	"testing"
)

// testCredentialKeyHex 是 32 字节 AES-256 密钥的 64 位 hex（测试固定值）。
const testCredentialKeyHex = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCredentialCipherRoundTrip(t *testing.T) {
	cipher, err := newCredentialCipher(testCredentialKeyHex)
	if err != nil {
		t.Fatalf("构造 cipher 失败: %v", err)
	}

	sealed, err := cipher.encryptHex("super-secret-token")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if sealed == "" || strings.Contains(sealed, "super-secret") {
		t.Fatalf("密文不应包含明文: %q", sealed)
	}

	plain, err := cipher.decryptHex(sealed)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if plain != "super-secret-token" {
		t.Fatalf("回读与原文不一致: %q", plain)
	}
}

func TestCredentialCipherWrongKeyFails(t *testing.T) {
	encryptor, _ := newCredentialCipher(testCredentialKeyHex)
	sealed, err := encryptor.encryptHex("secret")
	if err != nil {
		t.Fatal(err)
	}
	decryptor, _ := newCredentialCipher(strings.Repeat("b", 64))
	if _, err := decryptor.decryptHex(sealed); err == nil {
		t.Fatal("错误密钥解密应失败")
	}
}

func TestCredentialCipherEmpty(t *testing.T) {
	cipher, _ := newCredentialCipher(testCredentialKeyHex)
	if sealed, _ := cipher.encryptHex(""); sealed != "" {
		t.Fatalf("空明文应加密为空串: %q", sealed)
	}
	if plain, _ := cipher.decryptHex(""); plain != "" {
		t.Fatalf("空密文应解密为空串: %q", plain)
	}
}

func TestCredentialCipherBadKey(t *testing.T) {
	if _, err := newCredentialCipher("short"); err == nil {
		t.Fatal("非法密钥应报错")
	}
}

func TestEncryptValueRequiresKeyForNonEmpty(t *testing.T) {
	if _, err := encryptValue(nil, ""); err != nil {
		t.Fatalf("空值应放行: %v", err)
	}
	if _, err := encryptValue(nil, "secret"); err == nil {
		t.Fatal("无密钥时非空值应被拒绝")
	}
}
