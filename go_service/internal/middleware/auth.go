package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ============ 通用中间件 ============

// Trace 链路追踪中间件：自动生成/透传 X-Trace-Id
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceId := c.GetHeader(config.TraceIDKey)
		if traceId == "" {
			traceId = uuid.NewString()
		}
		c.Set(config.TraceIDKey, traceId)
		c.Header(config.TraceIDKey, traceId)
		c.Next()
	}
}

// ============ 鉴权中间件 ============

// resolveToken 从 Authorization: Bearer 头或 query token= 中取令牌
func resolveToken(c *gin.Context) string {
	if v := c.GetHeader("Authorization"); strings.HasPrefix(v, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
	}
	return c.Query("token")
}

// UserAuth 用户侧鉴权：校验 token 有效性 + user_id 归属当前租户
func UserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.AuthEnabled() { // 演示模式放行
			c.Next()
			return
		}
		tok := resolveToken(c)
		if tok == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "缺少访问令牌 token"})
			c.Abort()
			return
		}
		tenantID, err := auth.VerifyToken(tok)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "令牌无效或已过期"})
			c.Abort()
			return
		}
		// 取 user_id：GET/WS 从 query（user_id 或 thread_id）；POST 从 body（读后重置，供 handler 继续解析）
		userID := c.Query("user_id")
		if userID == "" {
			userID = c.Query("thread_id")
		}
		if userID == "" && c.Request.Method == http.MethodPost {
			body, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			var b struct {
				UserID string `json:"user_id"`
			}
			if json.Unmarshal(body, &b) == nil {
				userID = b.UserID
			}
		}
		if userID == "" || !auth.UserIDBelongsToTenant(tenantID, userID) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "user_id 不属于当前租户"})
			c.Abort()
			return
		}
		c.Set("tenant_id", tenantID)
		c.Next()
	}
}

// AdminAuth 管理侧鉴权：校验 token 并注入租户
func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.AuthEnabled() { // 演示模式放行
			c.Next()
			return
		}
		tok := resolveToken(c)
		if tok == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "缺少访问令牌 token"})
			c.Abort()
			return
		}
		tenantID, err := auth.VerifyToken(tok)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "令牌无效或已过期"})
			c.Abort()
			return
		}
		c.Set("tenant_id", tenantID)
		c.Next()
	}
}

// TenantOf 从 context 取当前租户 ID
func TenantOf(c *gin.Context) string {
	v, _ := c.Get("tenant_id")
	s, _ := v.(string)
	return s
}

// TicketOwnedByTenant 工单是否属于当前租户（严格按 tenant_id 匹配；演示模式放行）
func TicketOwnedByTenant(c *gin.Context, tk *model.Ticket) bool {
	if !auth.AuthEnabled() {
		return true
	}
	tenantID := TenantOf(c)
	if tenantID == "" {
		return true
	}
	return tk.TenantID == tenantID
}

// PlatformAdminOnly 平台管理员中间件（租户管理类接口专用）
func PlatformAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.AuthEnabled() {
			c.Next()
			return
		}
		info, ok := auth.TenantInfo(TenantOf(c))
		if !ok || !info.IsPlatformAdmin {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅平台管理员可操作"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// BusinessOnly 业务接口中间件：禁止平台管理员访问租户业务数据
// 仅放行平台级 Prompt 管理（/api/admin/prompts 系列，平台管理员维护全局 Prompt），
// 其余租户业务接口（工单/知识库/会话/模型配置/用量/审计）平台管理员一律不可访问
func BusinessOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.AuthEnabled() {
			c.Next()
			return
		}
		info, ok := auth.TenantInfo(TenantOf(c))
		if ok && info.IsPlatformAdmin {
			// 平台管理员仅可访问平台级 Prompt 管理接口
			if strings.HasPrefix(c.Request.URL.Path, "/api/admin/prompts") {
				c.Next()
				return
			}
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "平台管理员不可访问租户业务数据，请使用对应租户账号"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// InternalAuth 内部接口鉴权中间件：校验 X-Internal-Token 请求头
// 仅用于 Go 与 Agent 之间的内部通信，防止外部直接访问返回明文 API Key 的接口
func InternalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		expectedToken := config.GlobalConfig.InternalAPIToken
		if expectedToken == "" {
			// 未配置内部 token 时，允许访问（兼容旧部署），但记录警告
			logger.Warn("内部接口无鉴权保护", logger.Fields{"path": c.Request.URL.Path})
			c.Next()
			return
		}
		actualToken := c.GetHeader("X-Internal-Token")
		if actualToken != expectedToken {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "内部接口鉴权失败"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ExtractToken 从请求头提取 token
func ExtractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	// 支持 "Bearer <token>" 和直接 token 两种格式
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return authHeader
}
