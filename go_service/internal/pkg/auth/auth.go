package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/cache"
	"customer_service/internal/pkg/crypto"
)

var randRead = rand.Read

// RuntimeTenant 运行时租户（来自 config.DB）
type RuntimeTenant struct {
	TenantID        string
	Name            string
	AppKey          string
	AppSecretHash   string
	IsPlatformAdmin bool
	Status          int
}

var (
	authEnabled bool
	authTTL     = 24 * time.Hour
	tenantByKey = map[string]RuntimeTenant{}
	TenantsList = []RuntimeTenant{}

	// Token 黑名单（主动登出）：Redis SETEX（TTL 自动过期，多实例全局一致，强制依赖）
	// Redis 查询失败时按"已拉黑"处理（fail-closed）：登出的 token 绝不允许复活
)

const tokenBlacklistKeyPrefix = "cs:tok:bl:"

// BlacklistToken 将 Token 加入黑名单（主动登出）
func BlacklistToken(token string) {
	ttl := 24 * time.Hour
	// 解析 token 的过期时间，作为黑名单条目的过期时间
	parts := strings.Split(token, ".")
	if len(parts) == 2 {
		if pl, err := base64.RawURLEncoding.DecodeString(parts[0]); err == nil {
			var p tokenPayload
			if json.Unmarshal(pl, &p) == nil {
				ttl = time.Until(time.Unix(p.Exp, 0))
				if ttl <= 0 {
					ttl = time.Minute
				}
			}
		}
	}
	if rdb := cache.Client(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		rdb.Set(ctx, tokenBlacklistKeyPrefix+token, "1", ttl)
	}
}

// IsTokenBlacklisted 检查 Token 是否在黑名单中
// 强制 Redis：查询失败 → 按已拉黑处理（fail-closed，登出的 token 不允许复活）
func IsTokenBlacklisted(token string) bool {
	rdb := cache.Client()
	if rdb == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	n, err := rdb.Exists(ctx, tokenBlacklistKeyPrefix+token).Result()
	if err != nil {
		return true
	}
	return n > 0
}

// CleanupBlacklist 定期清理过期的黑名单条目（Redis TTL 自动清理，保留空实现兼容调用）
func CleanupBlacklist() {}

// RefreshToken 刷新 Token：验证旧 Token 有效后签发新 Token
// 返回 (newToken, tenantID, expiresIn, error)
func RefreshToken(oldToken string) (string, string, int64, error) {
	tenantID, err := VerifyToken(oldToken)
	if err != nil {
		return "", "", 0, err
	}
	// 查找租户信息，签发新 Token
	for _, t := range TenantsList {
		if t.TenantID == tenantID {
			p := tokenPayload{TenantID: t.TenantID, Exp: time.Now().Add(authTTL).Unix(), Nonce: generateNonce()}
			pl, _ := json.Marshal(p)
			newToken := base64.RawURLEncoding.EncodeToString(pl) + "." + signPayload(t.AppSecretHash, pl)
			// 旧 Token 加入黑名单，防止重复使用
			BlacklistToken(oldToken)
			return newToken, t.TenantID, int64(authTTL.Seconds()), nil
		}
	}
	return "", "", 0, errors.New("租户不存在")
}

// InitAuth 初始化鉴权（enabled=false 演示模式全部放行）
func InitAuth(enabled bool, ttl string, _ []model.Tenant) {
	authEnabled = enabled
	if ttl != "" {
		if d, err := time.ParseDuration(ttl); err == nil {
			authTTL = d
		}
	}
	if err := RefreshTenants(); err != nil {
		panic("load tenants from db: " + err.Error())
	}
}

// RefreshTenants 从 config.DB 重新加载全部租户（租户管理接口增删改后调用）
func RefreshTenants() error {
	var rows []model.TenantRow
	if err := config.DB.Model(&model.TenantRow{}).Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	tenantByKey = map[string]RuntimeTenant{}
	TenantsList = []RuntimeTenant{}
	for _, r := range rows {
		t := RuntimeTenant{
			TenantID:        r.TenantID,
			Name:            r.Name,
			AppKey:          r.AppKey,
			AppSecretHash:   r.AppSecretHash,
			IsPlatformAdmin: r.IsPlatformAdmin,
			Status:          r.Status,
		}
		tenantByKey[r.AppKey] = t
		TenantsList = append(TenantsList, t)
	}
	return nil
}

// AuthEnabled 是否启用鉴权
func AuthEnabled() bool { return authEnabled }

// GenSecret 生成随机 app_secret（16 字节 hex）
func GenSecret() string {
	b := make([]byte, 16)
	if _, err := randRead(b); err != nil {
		return time.Now().Format("20060102150405") + "cs"
	}
	return hex.EncodeToString(b)
}

// GenAppKey 生成随机 app_key（tk_ + 8 字节 hex）
func GenAppKey() string {
	b := make([]byte, 8)
	if _, err := randRead(b); err != nil {
		return "tk_" + time.Now().Format("150405")
	}
	return "tk_" + hex.EncodeToString(b)
}

type tokenPayload struct {
	TenantID string `json:"tid"`
	Exp      int64  `json:"exp"`
	Nonce    string `json:"nonce,omitempty"` // 随机数，确保每次签发的 Token 都不同
}

func signPayload(secretHash string, pl []byte) string {
	m := hmac.New(sha256.New, []byte(secretHash))
	m.Write(pl)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// generateNonce 生成随机 Nonce 字符串（16字节 hex）
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := randRead(b); err != nil {
		// 随机数生成失败时用时间戳兜底
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// IssueToken 用 app_key + app_secret 换取访问令牌（secret 哈希比对）
func IssueToken(appKey, appSecret string) (token, tenantID string, expiresIn int64, err error) {
	t, ok := tenantByKey[appKey]
	if !ok {
		return "", "", 0, errors.New("无效的 app_key 或 app_secret")
	}
	if t.Status != 1 || t.AppSecretHash != crypto.HashSecret(appSecret) {
		return "", "", 0, errors.New("无效的 app_key 或 app_secret")
	}
	p := tokenPayload{TenantID: t.TenantID, Exp: time.Now().Add(authTTL).Unix(), Nonce: generateNonce()}
	pl, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(pl) + "." + signPayload(t.AppSecretHash, pl),
		t.TenantID, int64(authTTL.Seconds()), nil
}

// VerifyToken 校验令牌，返回租户 ID
func VerifyToken(token string) (string, error) {
	// 检查 Token 是否在黑名单中（主动登出）
	if IsTokenBlacklisted(token) {
		return "", errors.New("token 已失效（已登出）")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", errors.New("token 格式错误")
	}
	pl, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("token 解码失败")
	}
	var p tokenPayload
	if err := json.Unmarshal(pl, &p); err != nil {
		return "", errors.New("token 载荷无效")
	}
	if p.Exp < time.Now().Unix() {
		return "", errors.New("token 已过期")
	}
	for _, t := range TenantsList {
		if t.TenantID == p.TenantID {
			if !hmac.Equal([]byte(signPayload(t.AppSecretHash, pl)), []byte(parts[1])) {
				return "", errors.New("token 签名无效")
			}
			return p.TenantID, nil
		}
	}
	return "", errors.New("租户不存在")
}

// TenantInfo 当前租户运行时信息（管理端上下文）
func TenantInfo(tenantID string) (RuntimeTenant, bool) {
	for _, t := range TenantsList {
		if t.TenantID == tenantID {
			return t, true
		}
	}
	return RuntimeTenant{}, false
}

// UserIDBelongsToTenant 校验 user_id 是否属于指定租户（未开启鉴权时放行）
func UserIDBelongsToTenant(tenantID, userID string) bool {
	if !authEnabled {
		return true
	}
	return tenantID != "" && strings.HasPrefix(userID, tenantID+":")
}

// TenantFromUserID 从 user_id 提取租户 ID
func TenantFromUserID(userID string) string {
	if i := strings.Index(userID, ":"); i > 0 {
		return userID[:i]
	}
	return ""
}

// TenantFromThreadID 从 thread_id 提取租户 ID（thread_id 格式与 user_id 一致：{tenant_id}:{uid}）
func TenantFromThreadID(threadID string) string {
	return TenantFromUserID(threadID)
}

// AllTenants 返回全部运行时租户（含禁用）
func AllTenants() []RuntimeTenant {
	return TenantsList
}
