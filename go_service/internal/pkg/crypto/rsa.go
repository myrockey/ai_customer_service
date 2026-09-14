package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
)

// ============ RSA 非对称加密（登录 app_secret 传输加密） ============
// 前端用公钥 RSA-OAEP(SHA-256) 加密 app_secret → 后端私钥解密后校验。
// 密钥对懒加载：首次启动自动生成并持久化到 RSAKeyPath，生产建议预置并备份私钥。

var (
	rsaOnce sync.Once
	rsaKey  *rsa.PrivateKey
	rsaErr  error
)

// InitRSA 初始化 RSA 密钥对（可重复调用，幂等）
func InitRSA(path string) {
	rsaOnce.Do(func() {
		rsaKey, rsaErr = loadOrCreateRSAKey(path)
	})
}

// GetRSAKey 返回已初始化的 RSA 私钥
func GetRSAKey() (*rsa.PrivateKey, error) {
	return rsaKey, rsaErr
}

// loadOrCreateRSAKey 加载私钥文件；不存在则生成 2048 位密钥并保存
func loadOrCreateRSAKey(path string) (*rsa.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
				return key, nil
			}
		}
	}
	// 生成新密钥
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(path, pemData, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// PublicKeyPEM 导出 PKIX/SPKI 格式公钥 PEM（前端 Web Crypto / JSEncrypt 通用）
func PublicKeyPEM(key *rsa.PrivateKey) (string, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})), nil
}

// DecryptRSAOAEP 解密前端 RSA-OAEP(SHA-256) 加密的 base64 密文，返回明文
func DecryptRSAOAEP(key *rsa.PrivateKey, cipherB64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", err
	}
	plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, data, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
