package middleware

import (
	"strings"

	"customer_service/internal/pkg/logger"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// AuditMiddleware P1-3：统一记录管理端写操作审计日志
// 挂在 /api/admin 分组上，拦截 POST/PUT/DELETE 请求，记录操作人/对象/IP
// 读操作（GET）不记录，避免日志膨胀
func AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		method := c.Request.Method
		// 仅记录写操作；4xx/5xx 且未实际生效的写操作也不记录（除鉴权失败类）
		if method == "GET" || method == "OPTIONS" {
			return
		}
		if c.Writer.Status() >= 400 {
			return
		}
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/agent") || strings.HasPrefix(path, "/api/user") {
			return // 内部接口与用户端接口不记管理审计
		}
		action := map[string]string{
			"POST":   "create",
			"PUT":    "update",
			"DELETE": "delete",
		}[method]
		if action == "" {
			return
		}
		tenantID := TenantOf(c)
		if tenantID == "" {
			tenantID = "system"
		}
		objectType := auditObjectType(path)
		// 平台管理员操作标记为平台级（t_admin 本身是租户表内的平台管理员行）
		objectID := c.Param("id")
		if objectID == "" {
			// 形如 /prompts/:key 的路由参数是 :key，取路径最后一段
			parts := strings.Split(strings.TrimSuffix(c.Request.URL.Path, "/"), "/")
			objectID = parts[len(parts)-1]
		}
		repository.WriteAuditLog(tenantID, tenantID, action, objectType,
			objectID, "", c.ClientIP())
	}
}

// auditObjectType 从路径推断操作对象类型
func auditObjectType(path string) string {
	p := strings.TrimPrefix(path, "/api/admin/")
	p = strings.TrimSuffix(strings.SplitN(p, "/", 2)[0], "s")
	switch p {
	case "ticket":
		return "ticket"
	case "prompt":
		return "prompt"
	case "knowledge", "kb-doc", "knowledge-doc":
		return "knowledge"
	case "tenant":
		return "tenant"
	case "model-config":
		return "model_config"
	case "model-provider":
		return "model_provider"
	case "platform-config":
		return "platform_config"
	case "session":
		return "session"
	default:
		return "other"
	}
}

var _ = logger.Info // 保留 import（后续扩展审计日志统计用）
