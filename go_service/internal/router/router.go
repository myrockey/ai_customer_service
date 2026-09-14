package router

import (
	"net/http"
	"os"
	"strings"
	"time"

	"customer_service/internal/handler"
	"customer_service/internal/middleware"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// corsAllowedOrigins 从环境变量 CORS_ALLOW_ORIGINS（逗号分隔）读取跨域白名单；
// 未配置时默认仅放行本地开发地址（生产环境必须显式配置，同源访问不受 CORS 限制）
func corsAllowedOrigins() []string {
	raw := os.Getenv("CORS_ALLOW_ORIGINS")
	if raw == "" {
		return []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	}
	var out []string
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	if len(out) == 0 {
		return []string{"http://localhost:8080", "http://127.0.0.1:8080"}
	}
	return out
}

// SetupRoutes 注册所有路由
func SetupRoutes(r *gin.Engine) {
	// ============ 全局中间件 ============
	// P0-3：CORS 收紧为白名单（同源 iframe 嵌入不受影响；第三方 XHR 直连需配置 CORS_ALLOW_ORIGINS）
	allowOrigins := corsAllowedOrigins()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"X-Trace-Id"},
		MaxAge:           12 * time.Hour,
		AllowCredentials: false,
	}))
	r.Use(middleware.Tracing())
	r.Use(middleware.Trace())
	r.Use(middleware.PrometheusMetrics())
	r.Use(middleware.RequestLogger())
	// P2-1：响应状态码归一化（body.code>=400 改写真实 HTTP 状态码；WS/metrics 跳过）
	r.Use(middleware.StatusCode())

	// ============ Prometheus metrics 端点 ============
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// ============ 业务内部接口：给Python Agent调用查询订单 ============
	r.GET("/api/business/order", handler.BusinessOrder)

	// ============ 内部接口：给 Python Agent 调用查询租户模型配置 ============
	agentInternal := r.Group("/api/agent", middleware.InternalAuth())
	{
		agentInternal.GET("/model-config", handler.AgentModelConfig)
		// P1-2：Agent 配额校验用（返回租户配额数值）
		agentInternal.GET("/tenant-quota", handler.AgentTenantQuota)
		agentInternal.GET("/tenant-concurrency", handler.AgentTenantConcurrency)
	}

	// ============ WebSocket：用户聊天长连接 ============
	r.GET("/ws/chat", middleware.UserAuth(), handler.HandleUserWS())

	// ============ WebSocket：管理端实时推送 ============
	r.GET("/ws/admin", middleware.AdminAuth(), handler.HandleAdminWS())

	// ============ 鉴权：宿主/管理端换票 ============
	r.GET("/api/auth/public-key", handler.PublicKeyHandler)
	r.POST("/api/auth/token", handler.IssueTokenHandler)
	r.POST("/api/auth/refresh", handler.RefreshTokenHandler)
	r.POST("/api/auth/logout", handler.LogoutHandler)
	// 修改自己密码（平台管理员/租户管理员，需登录态）
	r.PUT("/api/auth/password", middleware.AdminAuth(), handler.ChangePasswordHandler)

	// ============ 公开租户列表 ============
	r.GET("/api/auth/tenants", handler.PublicTenants)

	// ============ 用户聊天对外接口 ============
	r.POST("/api/user/chat", middleware.UserAuth(), handler.UserChat)

	// ============ 用户历史消息 ============
	r.GET("/api/user/messages", middleware.UserAuth(), handler.UserMessages)

	// ============ 人工客服完整后台 ============
	admin := r.Group("/api/admin", middleware.AdminAuth(), middleware.AuditMiddleware())
	{
		// 公共配置接口：平台管理员和租户都可访问
		// 模型提供商列表是公共配置数据，不属于租户业务数据
		admin.GET("/available-model-providers", handler.ListModelProvidersForTenant)

		// P1-1：Token 用量报表（平台管理员全览 / 租户仅自身，handler 内按角色处理）
		admin.GET("/token-usage", handler.TokenUsageReport)

		// P1-3：审计日志（平台管理员全览 / 租户仅自身）
		admin.GET("/audit-logs", handler.AuditLogs)

		// 平台设置（平台管理员动态配置，如知识库上传大小限制 kb_max_file_mb）
		admin.GET("/platform-settings", func(c *gin.Context) {
			handler.ProxyToAgent(c, http.MethodGet, "/agent/platform-settings")
		})
		admin.POST("/model-config/test", handler.AdminModelTest)

		// 业务接口：普通租户客服可用
		biz := admin.Group("", middleware.BusinessOnly())
		{
			biz.GET("/tickets", handler.AdminTicketList)
			biz.GET("/tickets/:id", handler.AdminTicketDetail)
			biz.GET("/tickets/:id/messages", handler.AdminTicketMessages)
			biz.POST("/tickets/:id/takeover", handler.AdminTicketTakeover)
			biz.POST("/tickets/:id/reply", handler.AdminTicketReply)
			biz.POST("/tickets/:id/close", handler.AdminTicketClose)
			biz.POST("/tickets/:id/priority", handler.AdminTicketSetPriority)
			biz.GET("/tickets/:id/logs", handler.AdminTicketLogs)
			biz.POST("/tickets/:id/read", handler.AdminTicketMarkRead)
			biz.POST("/ticket/approve", handler.AdminTicketApprove)

			// Prompt 管理（scope=platform 时仅平台管理员操作平台级全局 Prompt）
			biz.GET("/prompts", func(c *gin.Context) { handler.ProxyPrompt(c, http.MethodGet, "/agent/prompts") })
			biz.GET("/prompts/active", func(c *gin.Context) { handler.ProxyPrompt(c, http.MethodGet, "/agent/prompts/active") })
			biz.POST("/prompts", func(c *gin.Context) { handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts") })
			biz.POST("/prompts/:key/activate", func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts/"+c.Param("key")+"/activate")
			})
			biz.DELETE("/prompts/:key", func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodDelete, "/agent/prompts/"+c.Param("key"))
			})
			biz.POST("/prompts/:key/versions", func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts/"+c.Param("key")+"/versions")
			})
			biz.GET("/prompts/:key/versions", func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodGet, "/agent/prompts/"+c.Param("key")+"/versions")
			})
			biz.POST("/prompts/:key/versions/:vid/rollback", func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts/"+c.Param("key")+"/versions/"+c.Param("vid")+"/rollback")
			})

			// 知识库管理
			biz.POST("/kb/upload", handler.ProxyKbUpload)
			biz.GET("/kb/documents", func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodGet, "/agent/kb/documents") })
			biz.DELETE("/kb/documents/:doc_id", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodDelete, "/agent/kb/documents/"+c.Param("doc_id"))
			})
			biz.POST("/kb/documents/:doc_id/category", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/documents/"+c.Param("doc_id")+"/category")
			})
			biz.POST("/kb/documents/:doc_id/reindex", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/documents/"+c.Param("doc_id")+"/reindex")
			})
			// 手动触发索引入队（空 body = 全部待索引/失败文档；doc_ids 数组 = 指定文档）
			biz.POST("/kb/index", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/index")
			})
			biz.GET("/kb/categories", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodGet, "/agent/kb/categories")
			})
			biz.POST("/kb/batch", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/batch")
			})

			// 会话清理与统计
			biz.POST("/session/cleanup", func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodPost, "/agent/session/cleanup") })
			biz.GET("/session/stats", func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodGet, "/agent/session/stats") })
			biz.GET("/session/cleanup-count", func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodGet, "/agent/session/cleanup-count") })

			// P1-4：消息归档（租户仅本租户，平台管理员全量）
			biz.POST("/messages/archive", handler.ArchiveMessages)
			biz.GET("/messages/archive-count", handler.ArchiveMessagesCount)

			// 模型配置
			biz.GET("/model-config", handler.BusinessModelConfig)
			biz.PUT("/model-config", handler.BusinessModelConfigUpdate)
		}

		// ============ 租户管理（仅平台管理员） ============
		plat := admin.Group("", middleware.PlatformAdminOnly())
		{
			plat.GET("/tenants", handler.TenantList)
			plat.GET("/tenants/:tid", handler.TenantDetail)
			plat.POST("/tenants", handler.TenantCreate)
			plat.PUT("/tenants/:tid", handler.TenantUpdate)
			plat.PUT("/tenants/:tid/quota", handler.TenantUpdateQuota)
			plat.GET("/tenants/:tid/model-config", handler.TenantModelConfig)
			plat.PUT("/tenants/:tid/model-config", handler.TenantModelConfigUpdate)
			plat.DELETE("/tenants/:tid", handler.TenantDelete)
			plat.POST("/tenants/:tid/reset-secret", handler.TenantResetSecret)
			plat.POST("/tenants/:tid/toggle", handler.TenantToggle)

			plat.GET("/model-providers", handler.ListModelProviders)
			plat.POST("/model-providers", handler.CreateModelProvider)
			plat.PUT("/model-providers/:id", handler.UpdateModelProvider)
			plat.DELETE("/model-providers/:id", handler.DeleteModelProvider)

			plat.PUT("/platform-settings", func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPut, "/agent/platform-settings")
			})
		}
	}
}
