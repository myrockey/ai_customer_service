package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// getEncryptKey 从环境变量获取加密密钥，不足32字节则用0填充，超过则截断
// P0-4：生产模式（PROD_MODE=true）必须显式设置 MODEL_KEY_ENCRYPT_KEY，禁止使用硬编码默认密钥
func getEncryptKey() []byte {
	key := os.Getenv("MODEL_KEY_ENCRYPT_KEY")
	if key == "" {
		if os.Getenv("PROD_MODE") == "true" {
			panic("MODEL_KEY_ENCRYPT_KEY 未设置：生产模式禁止使用默认加密密钥，请通过环境变量/密钥服务注入")
		}
		// 开发环境默认密钥（仅限本地开发，生产必须覆盖）
		key = "cs_prod_default_encrypt_key_2026!"
	}
	b := []byte(key)
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded, b)
		return padded
	}
	return b[:32]
}

// EncryptAPIKey AES-GCM 加密 API Key，返回 base64 编码的密文
func EncryptAPIKey(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key := getEncryptKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptAPIKey AES-GCM 解密 API Key，输入 base64 编码的密文
func DecryptAPIKey(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	key := getEncryptKey()
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("密文太短")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// MaskAPIKey 脱敏 API Key，只显示后4位，如 sk-...abcd
func MaskAPIKey(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	if len(plaintext) <= 4 {
		return "****"
	}
	return "..." + plaintext[len(plaintext)-4:]
}

// HashSecret app_secret 哈希（sha256 hex）
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
