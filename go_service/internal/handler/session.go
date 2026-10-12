package handler

import (
	"net/http"
	"strconv"
	"strings"

	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// ============ 会话记录与客户信息（租户业务接口，BusinessOnly 保护） ============

// threadOwnedByTenant 会话是否属于当前租户（thread_id 前缀 = tenant_id；演示模式放行）
func threadOwnedByTenant(c *gin.Context, threadID string) bool {
	if !auth.AuthEnabled() {
		return true
	}
	tenantID := middleware.TenantOf(c)
	if tenantID == "" {
		return true
	}
	return auth.TenantFromThreadID(threadID) == tenantID
}

// AdminSessionList 会话列表：GET /api/admin/sessions?page=&size=
// 返回：会话聚合（客户消息数/最后活跃/未读数/转人工）+ 客户档案（姓名/电话/地址/邮箱）
func AdminSessionList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	tid := middleware.TenantOf(c)
	handoff := 0
	if h := c.Query("handoff"); h == "1" {
		handoff = 1
	}
	items, total, err := repository.ListSessions(tid, page, size, repository.SessionListOpt{
		Keyword:  strings.TrimSpace(c.Query("q")),
		DateFrom: strings.TrimSpace(c.Query("start")),
		DateTo:   strings.TrimSpace(c.Query("end")),
		Handoff:  handoff,
		Tag:      strings.TrimSpace(c.Query("tag")),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"list":  items,
		"total": total,
		"page":  page,
		"size":  size,
	}})
}

// AdminCustomerList 客户列表：GET /api/admin/customers?page=&size=&q=
// 按人聚合展示提取/人工填写的客户档案，支持姓名/电话/邮箱/地址/备注模糊搜索
func AdminCustomerList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	q := strings.TrimSpace(c.DefaultQuery("q", ""))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	tid := middleware.TenantOf(c)
	items, total, err := repository.ListCustomers(tid, page, size, q)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"list":  items,
		"total": total,
		"page":  page,
		"size":  size,
		"q":     q,
	}})
}

// AdminSessionMessages 会话聊天记录：GET /api/admin/sessions/:thread_id/messages
// messages 与 messages_archive 合并返回，归档消息带 archived 标记
func AdminSessionMessages(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 thread_id"})
		return
	}
	if !threadOwnedByTenant(c, threadID) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户会话"})
		return
	}
	msgs, err := repository.ListMessagesWithArchive(threadID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	profile, _ := repository.GetCustomerProfile(threadID) // 无档案不报错
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"messages": msgs,
		"profile":  profile,
	}})
}

// AdminCustomerProfileUpdate 人工填写/更新客户信息：PUT /api/admin/sessions/:thread_id/profile
// body: {"name":"","phone":"","address":"","email":"","remark":""}
func AdminCustomerProfileUpdate(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 thread_id"})
		return
	}
	if !threadOwnedByTenant(c, threadID) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户会话"})
		return
	}
	var body struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Address string `json:"address"`
		Email   string `json:"email"`
		Remark  string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	tid := middleware.TenantOf(c)
	cp := &model.CustomerProfile{
		ThreadID: threadID,
		TenantID: tid,
		Name:     body.Name,
		Phone:    body.Phone,
		Address:  body.Address,
		Email:    body.Email,
		Remark:   body.Remark,
	}
	if err := repository.SaveCustomerProfile(cp); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "客户信息已保存"})
}

// AdminSessionTagsGet 查询会话标签：GET /api/admin/sessions/:thread_id/tags
func AdminSessionTagsGet(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 thread_id"})
		return
	}
	if !threadOwnedByTenant(c, threadID) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户会话"})
		return
	}
	tid := middleware.TenantOf(c)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"thread_id": threadID, "tags": repository.GetSessionTags(tid, threadID)}})
}

// AdminSessionTagsUpdate 设置会话标签：PUT /api/admin/sessions/:thread_id/tags body:{tags:"意向,投诉"}
func AdminSessionTagsUpdate(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 thread_id"})
		return
	}
	if !threadOwnedByTenant(c, threadID) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户会话"})
		return
	}
	var body struct {
		Tags string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	tid := middleware.TenantOf(c)
	if err := repository.SetSessionTags(tid, threadID, body.Tags); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "标签已保存"})
}

// AdminCustomerReextract 手动重新提取：POST /api/admin/sessions/:thread_id/reextract
// 规则变更后对指定会话重跑正则，补回 phone/email
func AdminCustomerReextract(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 thread_id"})
		return
	}
	if !threadOwnedByTenant(c, threadID) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户会话"})
		return
	}
	if err := repository.ReextractSession(threadID); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "重新提取完成"})
}

// ============ 提取规则配置（仅平台管理员，plat 子组） ============

// AdminExtractRulesGet 获取提取规则：GET /api/admin/extract-rules
func AdminExtractRulesGet(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": repository.GetExtractRules()})
}

// AdminExtractRulesUpdate 更新提取规则：PUT /api/admin/extract-rules
// body: [{"field":"phone","pattern":"..."},{"field":"email","pattern":"..."}]
func AdminExtractRulesUpdate(c *gin.Context) {
	var rules []model.ExtractRule
	if err := c.ShouldBindJSON(&rules); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	if err := repository.SaveExtractRules(rules); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "提取规则已更新"})
}

// ====================== 会话摘要（agent 后台任务内部接口） ======================

// AgentSessionSummaryPending 返回待生成摘要的候选会话：GET /api/agent/session-summaries/pending?limit=20
func AgentSessionSummaryPending(c *gin.Context) {
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	cands, err := repository.PendingSummaryCandidates(limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cands})
}

// AgentSessionSummarySave 保存会话摘要：POST /api/agent/session-summaries
// body: {"thread_id":"...","tenant_id":"...","summary":"..."}
func AgentSessionSummarySave(c *gin.Context) {
	var body struct {
		ThreadID string `json:"thread_id"`
		TenantID string `json:"tenant_id"`
		Summary  string `json:"summary"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	if body.ThreadID == "" || body.TenantID == "" || body.Summary == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "thread_id/tenant_id/summary 不能为空"})
		return
	}
	if err := repository.UpsertSessionSummary(body.TenantID, body.ThreadID, body.Summary); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "会话摘要已保存"})
}
