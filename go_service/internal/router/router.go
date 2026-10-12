package router

import (
	"net/http"
	"os"
	"strings"
	"time"

	"customer_service/internal/handler"
	"customer_service/internal/middleware"
	"customer_service/internal/pkg/permission"

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
		// 会话摘要：agent 后台任务拉候选会话 / 回写摘要
		agentInternal.GET("/session-summaries/pending", handler.AgentSessionSummaryPending)
		agentInternal.POST("/session-summaries", handler.AgentSessionSummarySave)
	}

	// ============ WebSocket：用户聊天长连接 ============
	r.GET("/ws/chat", middleware.UserAuth(), handler.HandleUserWS())

	// ============ WebSocket：管理端实时推送 ============
	r.GET("/ws/admin", middleware.AdminAuth(), handler.HandleAdminWS())

	// ============ 鉴权：宿主/管理端换票 ============
	r.GET("/api/auth/public-key", handler.PublicKeyHandler)
	r.POST("/api/auth/token", handler.IssueTokenHandler)
	r.POST("/api/auth/user-token", handler.UserTokenHandler)
	r.POST("/api/auth/refresh", handler.RefreshTokenHandler)
	r.POST("/api/auth/logout", handler.LogoutHandler)
	// 修改自己密码（平台管理员/租户管理员，需登录态）
	r.PUT("/api/auth/password", middleware.AdminAuth(), handler.ChangePasswordHandler)

	// ============ 公开租户列表 ============
	r.GET("/api/auth/tenants", handler.PublicTenants)

	// ============ 用户聊天对外接口 ============
	r.POST("/api/user/chat", middleware.UserAuth(), handler.UserChat)
	// P1：用户端上传图片/视频（发送 msg_type=image/video 消息前先上传拿 URL）
	r.POST("/api/user/upload", middleware.UserAuth(), handler.UserSessionMediaUpload)

	// ============ 用户历史消息 ============
	r.GET("/api/user/messages", middleware.UserAuth(), handler.UserMessages)

	// ============ 用户满意度评价 ============
	r.POST("/api/user/rating", middleware.UserAuth(), handler.UserSubmitRating)

	// ============ 人工客服完整后台 ============
	admin := r.Group("/api/admin", middleware.AdminAuth(), middleware.AuditMiddleware())
	{
		// 菜单树（登录用户渲染侧边栏/动态路由；uid=0 全量，子账号按权限过滤）
		admin.GET("/menus/tree", handler.AdminMenuTree)
		// 公共配置接口：平台管理员和租户都可访问
		// 模型提供商列表是公共配置数据，不属于租户业务数据
		admin.GET("/available-model-providers", handler.ListModelProvidersForTenant)

		// P1-1：Token 用量报表（平台管理员全览 / 租户仅自身，handler 内按角色处理）
		admin.GET("/token-usage", middleware.RequirePerm(permission.UsageView), handler.TokenUsageReport)

		// P1-3：审计日志（平台管理员全览 / 租户仅自身）
		admin.GET("/audit-logs", middleware.RequirePerm(permission.AuditView), handler.AuditLogs)

		// 平台设置（平台管理员动态配置，如知识库上传大小限制 kb_max_file_mb）
		admin.GET("/platform-settings", func(c *gin.Context) {
			handler.ProxyToAgent(c, http.MethodGet, "/agent/platform-settings")
		})
		admin.POST("/model-config/test", handler.AdminModelTest)

		// 业务接口：普通租户客服可用
		biz := admin.Group("", middleware.BusinessOnly())
		{
			// 运营看板（报表聚合）
			biz.GET("/ops/dashboard", middleware.RequirePerm(permission.DashboardView), handler.AdminOpsDashboard)
			biz.GET("/tickets", middleware.RequirePerm(permission.TicketView), handler.AdminTicketList)
			biz.GET("/tickets/:id", middleware.RequirePerm(permission.TicketView), handler.AdminTicketDetail)
			biz.GET("/tickets/:id/messages", middleware.RequirePerm(permission.TicketView), handler.AdminTicketMessages)
			biz.POST("/tickets/:id/takeover", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketTakeover)
			biz.POST("/tickets/:id/reply", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketReply)
			biz.POST("/tickets/:id/close", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketClose)
			biz.POST("/tickets/:id/priority", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketSetPriority)
			biz.POST("/tickets/:id/transfer", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketTransfer)
			biz.GET("/tickets/transfer-targets", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketTransferTargets)
			biz.GET("/tickets/:id/logs", middleware.RequirePerm(permission.TicketView), handler.AdminTicketLogs)
			biz.POST("/tickets/:id/read", middleware.RequirePerm(permission.TicketView), handler.AdminTicketMarkRead)
			biz.POST("/ticket/approve", middleware.RequirePerm(permission.TicketHandle), handler.AdminTicketApprove)

			// 客服在线状态
			biz.GET("/agent-status", middleware.RequirePerm(permission.TicketView), handler.AdminAgentStatusList)
			biz.PUT("/agent-status", middleware.RequirePerm(permission.TicketHandle), handler.AdminAgentStatusUpdate)

			// 快捷回复话术库
			biz.GET("/quick-replies", middleware.RequirePerm(permission.QuickReplyView), handler.AdminQuickReplyList)
			biz.POST("/quick-replies", middleware.RequirePerm(permission.QuickReplyAdd), handler.AdminQuickReplyCreate)
			biz.PUT("/quick-replies/:id", middleware.RequirePerm(permission.QuickReplyEdit), handler.AdminQuickReplyUpdate)
			biz.DELETE("/quick-replies/:id", middleware.RequirePerm(permission.QuickReplyRemove), handler.AdminQuickReplyDelete)

			// 满意度评价
			biz.GET("/ratings", middleware.RequirePerm(permission.RatingView), handler.AdminRatingList)
			biz.GET("/ratings/stats", middleware.RequirePerm(permission.RatingView), handler.AdminRatingStats)

			// Prompt 管理（scope=platform 时仅平台管理员操作平台级全局 Prompt）
			biz.GET("/prompts", middleware.RequirePerm(permission.PromptView), func(c *gin.Context) { handler.ProxyPrompt(c, http.MethodGet, "/agent/prompts") })
			biz.GET("/prompts/active", middleware.RequirePerm(permission.PromptView), func(c *gin.Context) { handler.ProxyPrompt(c, http.MethodGet, "/agent/prompts/active") })
			biz.POST("/prompts", middleware.RequirePerm(permission.PromptAdd), func(c *gin.Context) { handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts") })
			biz.POST("/prompts/:key/activate", middleware.RequirePerm(permission.PromptEdit), func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts/"+c.Param("key")+"/activate")
			})
			biz.DELETE("/prompts/:key", middleware.RequirePerm(permission.PromptRemove), func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodDelete, "/agent/prompts/"+c.Param("key"))
			})
			biz.POST("/prompts/:key/versions", middleware.RequirePerm(permission.PromptEdit), func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts/"+c.Param("key")+"/versions")
			})
			biz.GET("/prompts/:key/versions", middleware.RequirePerm(permission.PromptView), func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodGet, "/agent/prompts/"+c.Param("key")+"/versions")
			})
			biz.POST("/prompts/:key/versions/:vid/rollback", middleware.RequirePerm(permission.PromptEdit), func(c *gin.Context) {
				handler.ProxyPrompt(c, http.MethodPost, "/agent/prompts/"+c.Param("key")+"/versions/"+c.Param("vid")+"/rollback")
			})

			// 知识库管理
			biz.POST("/kb/upload", middleware.RequirePerm(permission.KnowledgeAdd), handler.ProxyKbUpload)
			biz.GET("/kb/documents", middleware.RequirePerm(permission.KnowledgeView), func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodGet, "/agent/kb/documents") })
			biz.DELETE("/kb/documents/:doc_id", middleware.RequirePerm(permission.KnowledgeRemove), func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodDelete, "/agent/kb/documents/"+c.Param("doc_id"))
			})
			biz.POST("/kb/documents/:doc_id/category", middleware.RequirePerm(permission.KnowledgeEdit), func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/documents/"+c.Param("doc_id")+"/category")
			})
			biz.POST("/kb/documents/:doc_id/reindex", middleware.RequirePerm(permission.KnowledgeEdit), func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/documents/"+c.Param("doc_id")+"/reindex")
			})
			// 手动触发索引入队（空 body = 全部待索引/失败文档；doc_ids 数组 = 指定文档）
			biz.POST("/kb/index", middleware.RequirePerm(permission.KnowledgeEdit), func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/index")
			})
			biz.GET("/kb/categories", middleware.RequirePerm(permission.KnowledgeView), func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodGet, "/agent/kb/categories")
			})
			biz.POST("/kb/batch", middleware.RequirePerm(permission.KnowledgeEdit), func(c *gin.Context) {
				handler.ProxyToAgent(c, http.MethodPost, "/agent/kb/batch")
			})

			// 会话清理与统计（清理/归档为运维操作，仅 session:cleanup 权限角色）
			biz.POST("/session/cleanup", middleware.RequirePerm(permission.SessionCleanup), func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodPost, "/agent/session/cleanup") })
			biz.GET("/session/stats", middleware.RequirePerm(permission.SessionView), func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodGet, "/agent/session/stats") })
			biz.GET("/session/cleanup-count", middleware.RequirePerm(permission.SessionCleanup), func(c *gin.Context) { handler.ProxyToAgent(c, http.MethodGet, "/agent/session/cleanup-count") })

			// 会话记录与客户信息（会话列表/聊天记录/人工填写客户档案/重新提取）
			biz.GET("/sessions", middleware.RequirePerm(permission.SessionView), handler.AdminSessionList)
			biz.GET("/sessions/:thread_id/messages", middleware.RequirePerm(permission.SessionView), handler.AdminSessionMessages)
			biz.PUT("/sessions/:thread_id/profile", middleware.RequirePerm(permission.SessionProfileEdit), handler.AdminCustomerProfileUpdate)
			biz.POST("/sessions/:thread_id/reextract", middleware.RequirePerm(permission.SessionProfileEdit), handler.AdminCustomerReextract)
			biz.GET("/sessions/:thread_id/tags", middleware.RequirePerm(permission.SessionView), handler.AdminSessionTagsGet)
			biz.PUT("/sessions/:thread_id/tags", middleware.RequirePerm(permission.SessionProfileEdit), handler.AdminSessionTagsUpdate)
			// 会话图片上传（人工客服回复图片消息，图片 URL 以 msg_type=image 发送）
			biz.POST("/sessions/upload", middleware.RequirePerm(permission.TicketHandle), handler.AdminSessionMediaUpload)
			// 客户列表（按人聚合，支持搜索）
			biz.GET("/customers", middleware.RequirePerm(permission.CustomerView), handler.AdminCustomerList)

			// P1-4：消息归档（租户仅本租户，平台管理员全量；运维操作仅 session:cleanup 权限角色）
			biz.POST("/messages/archive", middleware.RequirePerm(permission.SessionCleanup), handler.ArchiveMessages)
			biz.GET("/messages/archive-count", middleware.RequirePerm(permission.SessionCleanup), handler.ArchiveMessagesCount)

			// 模型配置
			biz.GET("/model-config", middleware.RequirePerm(permission.ModelView), handler.BusinessModelConfig)
			biz.PUT("/model-config", middleware.RequirePerm(permission.ModelEdit), handler.BusinessModelConfigUpdate)
			// RBAC：成员管理
			biz.GET("/members", middleware.RequirePerm(permission.MemberView), handler.AdminMemberList)
			biz.POST("/members", middleware.RequirePerm(permission.MemberAdd), handler.AdminMemberCreate)
			biz.PUT("/members/:id/status", middleware.RequirePerm(permission.MemberEdit), handler.AdminMemberStatus)
			biz.PUT("/members/:id/password", middleware.RequirePerm(permission.MemberEdit), handler.AdminMemberPassword)
			biz.PUT("/members/:id/roles", middleware.RequirePerm(permission.MemberEdit), handler.AdminMemberRoles)
			biz.PUT("/members/:id", middleware.RequirePerm(permission.MemberEdit), handler.AdminMemberUpdate)
			biz.DELETE("/members/:id", middleware.RequirePerm(permission.MemberRemove), handler.AdminMemberDelete)

			// RBAC：角色管理
			biz.GET("/roles", middleware.RequirePerm(permission.RoleView), handler.AdminRoleList)
			biz.GET("/permissions", middleware.RequirePerm(permission.RoleView), handler.AdminPermissionCatalog)
			biz.POST("/roles", middleware.RequirePerm(permission.RoleAdd), handler.AdminRoleCreate)
			biz.PUT("/roles/:id", middleware.RequirePerm(permission.RoleEdit), handler.AdminRoleUpdate)
			biz.DELETE("/roles/:id", middleware.RequirePerm(permission.RoleRemove), handler.AdminRoleDelete)
		}

		// ============ 租户管理（仅平台管理员） ============
		plat := admin.Group("", middleware.PlatformAdminOnly())
		{
			// 菜单权限管理（平台级：菜单=权限载体，全局一份）
			plat.GET("/menus", handler.AdminMenuList)
			plat.POST("/menus", handler.AdminMenuCreate)
			plat.PUT("/menus/:id", handler.AdminMenuUpdate)
			plat.DELETE("/menus/:id", handler.AdminMenuDelete)
			plat.POST("/menus/reset-builtin", handler.AdminMenuResetBuiltin)
	plat.POST("/menus/batch-update", handler.AdminMenuBatchUpdate)
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

			// 客户信息提取规则（平台管理员全局配置，租户共用）
			plat.GET("/extract-rules", handler.AdminExtractRulesGet)
			plat.PUT("/extract-rules", handler.AdminExtractRulesUpdate)
		}
	}
}
