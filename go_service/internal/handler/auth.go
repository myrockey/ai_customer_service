package handler

import (
	"net/http"
	"time"

	"customer_service/internal/middleware"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/crypto"
	"customer_service/internal/repository"
	"customer_service/internal/service"

	"github.com/gin-gonic/gin"
)

// ============ 登录公钥下发：GET /api/auth/public-key ============
// 管理端登录时用该公钥 RSA-OAEP(SHA-256) 加密 app_secret，防明文传输/抓包
func PublicKeyHandler(c *gin.Context) {
	key, err := crypto.GetRSAKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "RSA 密钥未就绪"})
		return
	}
	pub, err := crypto.PublicKeyPEM(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "公钥导出失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"public_key": pub}})
}

// ============ 换票接口：POST /api/auth/token ============
// 宿主系统用 app_key + app_secret 换取访问令牌；管理端登录亦复用此接口。
// 安全：app_secret 支持 RSA 加密传输（encrypted=true 时用私钥解密后再校验），
// 兼容第三方明文接入（encrypted 缺省为 false）。
func IssueTokenHandler(c *gin.Context) {
	var b struct {
		AppKey    string `json:"app_key"`
		AppSecret string `json:"app_secret"`
		Encrypted bool   `json:"encrypted"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || b.AppKey == "" || b.AppSecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}

	// P0-2：登录防爆破（同一 IP+app_key 连续失败 5 次锁定 10 分钟）
	ip := c.ClientIP()
	limiterKey := ip + ":" + b.AppKey
	if ok, wait := auth.CheckLoginAllowed(limiterKey); !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "尝试过于频繁，请 " + wait.Round(time.Minute).String() + " 后再试"})
		return
	}

	secret := b.AppSecret
	if b.Encrypted {
		key, err := crypto.GetRSAKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "RSA 密钥未就绪"})
			return
		}
		plain, err := crypto.DecryptRSAOAEP(key, b.AppSecret)
		if err != nil {
			auth.RecordLoginFail(limiterKey)
			repository.WriteAuditLog("", b.AppKey, "login_fail", "auth", "app_secret", "解密失败", ip)
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "app_secret 解密失败"})
			return
		}
		secret = plain
	}

	tok, tid, exp, err := auth.IssueToken(b.AppKey, secret)
	if err != nil {
		auth.RecordLoginFail(limiterKey)
		repository.WriteAuditLog("", b.AppKey, "login_fail", "auth", "app_key", err.Error(), ip)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": err.Error()})
		return
	}
	auth.ClearLoginFails(limiterKey)
	isPlat := false
	if info, ok := auth.TenantInfo(tid); ok {
		isPlat = info.IsPlatformAdmin
	}
	repository.WriteAuditLog(tid, b.AppKey, "login_ok", "auth", "app_key", "登录成功", ip)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"token": tok, "tenant_id": tid, "expires_in": exp, "is_platform_admin": isPlat,
	}})
}

// RefreshTokenHandler 刷新 Token：POST /api/auth/refresh
// 请求头 Authorization: Bearer <旧 token>，返回新 token（旧 token 自动失效）
func RefreshTokenHandler(c *gin.Context) {
	oldToken := middleware.ExtractToken(c)
	if oldToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未提供 token"})
		return
	}
	newToken, tid, exp, err := auth.RefreshToken(oldToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "token 刷新失败: " + err.Error()})
		return
	}
	isPlat := false
	if info, ok := auth.TenantInfo(tid); ok {
		isPlat = info.IsPlatformAdmin
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"token": newToken, "tenant_id": tid, "expires_in": exp, "is_platform_admin": isPlat,
	}})
}

// LogoutHandler 主动登出：POST /api/auth/logout
// 将当前 token 加入黑名单，使其立即失效
func LogoutHandler(c *gin.Context) {
	token := middleware.ExtractToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未提供 token"})
		return
	}
	auth.BlacklistToken(token)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已登出，token 已失效"})
}

// ============ 修改自己密码：PUT /api/auth/password ============
// 平台管理员 / 租户管理员修改自己的登录密钥（app_secret）。
// 安全：old/new 均支持 RSA 加密传输（encrypted=true，与登录一致）；
// 校验旧密钥防止越权；更新后旧 token 因签名密钥变化自动失效，需重新登录。
func ChangePasswordHandler(c *gin.Context) {
	var b struct {
		OldSecret string `json:"old_app_secret"`
		NewSecret string `json:"new_app_secret"`
		Encrypted bool   `json:"encrypted"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || b.OldSecret == "" || b.NewSecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}

	oldS, newS := b.OldSecret, b.NewSecret
	if b.Encrypted {
		key, err := crypto.GetRSAKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "RSA 密钥未就绪"})
			return
		}
		plainOld, err := crypto.DecryptRSAOAEP(key, b.OldSecret)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "旧密钥解密失败"})
			return
		}
		plainNew, err := crypto.DecryptRSAOAEP(key, b.NewSecret)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "新密钥解密失败"})
			return
		}
		oldS, newS = plainOld, plainNew
	}
	if len(newS) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "新密钥长度至少 8 位"})
		return
	}

	if err := service.ChangeTenantSecret(tid, oldS, newS); err != nil {
		repository.WriteAuditLog(tid, "", "change_password_fail", "auth", "app_secret", err.Error(), c.ClientIP())
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": err.Error()})
		return
	}
	repository.WriteAuditLog(tid, "", "change_password", "auth", "app_secret", "修改登录密钥成功", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "密码修改成功，请重新登录"})
}
