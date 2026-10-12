package handler

import (
	"customer_service/internal/config"
	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/repository"
	"net/http"
	"strconv"
	"customer_service/internal/service"
	"strings"

	"github.com/gin-gonic/gin"
)

// ============ 工单管理（人工客服后台） ============
// P4-2：引入 service 层，handler 只负责参数解析和响应返回

// adminTicketList 工单列表：GET /api/admin/tickets?status=&page=&size=
func AdminTicketList(c *gin.Context) {
	status := -1
	if s := c.Query("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			status = v
		}
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	// 工单列表严格按当前租户过滤（平台管理员不可访问业务接口，由 businessOnly 中间件拦截）
	tid := middleware.TenantOf(c)
	// scope=mine：只看当前登录客服的工单（含自动分配）
	var handlerUID uint64
	if c.Query("scope") == "mine" {
		handlerUID = middleware.UserIDOf(c)
	}
	priority := -1
	if p := c.Query("priority"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v >= 0 && v <= 3 {
			priority = v
		}
	}
	result, err := service.ListTickets(tid, service.TicketListInput{
		Status:     status,
		Page:       page,
		Size:       size,
		HandlerUID: handlerUID,
		Keyword:    strings.TrimSpace(c.Query("q")),
		Priority:   priority,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// adminTicketDetail 工单详情：GET /api/admin/tickets/:id
func AdminTicketDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": tk})
}

// adminTicketMessages 会话消息查看：GET /api/admin/tickets/:id/messages
func AdminTicketMessages(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	msgs, err := service.GetTicketMessages(tk)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": msgs})
}

// AdminTicketMarkRead 标记工单会话已读：POST /api/admin/tickets/:id/read
// 客服打开/查看工单详情时调用，将本会话所有非用户消息置为已读，未读数归零
func AdminTicketMarkRead(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	if err := repository.MarkMessagesRead(tk.ThreadID); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// adminTicketTakeover 人工接管：POST /api/admin/tickets/:id/takeover
func AdminTicketTakeover(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var body struct {
		WaitTime int `json:"wait_time"`
	}
	_ = c.ShouldBindJSON(&body)

	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	if tk.Status == model.TicketStatusClosed {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "工单已关闭"})
		return
	}
	if tk.Status == model.TicketStatusCanceled {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "工单已取消"})
		return
	}
	// 防重：已接管中，仅接管人本人/管理员可重复接管（幂等），其余拒绝
	if tk.Status == model.TicketStatusHandling && !canHandleTicket(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "该工单由 " + orDash(tk.HandlerName) + " 接管中，无权接管"})
		return
	}
	msg, err := service.TakeoverTicket(tk, body.WaitTime, c.GetString(config.TraceIDKey), middleware.UserIDOf(c), currentHandlerName(c))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msg})
}

// adminTicketReply 人工客服回复：POST /api/admin/tickets/:id/reply
func AdminTicketReply(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Content == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "content 不能为空"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	if tk.Status != model.TicketStatusHandling {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "该工单未处于人工接管中，无法回复"})
		return
	}
	if !canHandleTicket(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "该工单由 " + orDash(tk.HandlerName) + " 接管中，无权回复"})
		return
	}
	if err := service.ReplyTicket(tk, body.Content); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// adminTicketClose 关闭工单：POST /api/admin/tickets/:id/close
// 关闭后该线程恢复智能客服接待
func AdminTicketClose(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	if tk.Status == model.TicketStatusClosed {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "工单已关闭"})
		return
	}
	if tk.Status == model.TicketStatusHandling && !canHandleTicket(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "该工单由 " + orDash(tk.HandlerName) + " 接管中，无权关闭"})
		return
	}
	if err := service.CloseTicket(tk, currentHandlerName(c)); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已关闭"})
}

// adminTicketApprove 兼容旧接口：POST /api/admin/ticket/approve
func AdminTicketApprove(c *gin.Context) {
	var body struct {
		TicketNo  string `json:"ticket_no"`
		Confirmed bool   `json:"confirmed"`
		WaitTime  int    `json:"wait_time"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.TicketNo == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tk, err := service.GetTicketByNo(body.TicketNo)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	msg, err := service.ApproveTicket(tk, service.ApproveTicketInput{
		TicketNo:  body.TicketNo,
		Confirmed: body.Confirmed,
		WaitTime:  body.WaitTime,
		TraceID:   c.GetString(config.TraceIDKey),
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msg})
}

// P1-3：工单状态流转完善
// adminTicketSetPriority 设置工单优先级
func AdminTicketSetPriority(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var body struct {
		Priority int    `json:"priority" binding:"required"`
		Remark   string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误：priority 必填"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	if err := service.SetTicketPriority(tk, body.Priority, currentHandlerName(c), body.Remark); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "priority 取值范围：0-3"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "优先级设置成功", "data": gin.H{"priority": body.Priority}})
}

// adminTicketLogs 工单操作历史
func AdminTicketLogs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	logs, err := service.GetTicketLogs(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": logs})
}


// AdminTicketTransfer 转交工单：POST /api/admin/tickets/:id/transfer
// 仅接管人本人 / 租户管理员 / app_key 管理员可转交；目标客服需同租户
func AdminTicketTransfer(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var body struct {
		ToUserID uint64 `json:"to_user_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ToUserID == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "to_user_id 不能为空"})
		return
	}
	tk, err := service.GetTicketByID(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "工单不存在"})
		return
	}
	if !middleware.TicketOwnedByTenant(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问该租户工单"})
		return
	}
	if tk.Status != model.TicketStatusHandling && tk.Status != model.TicketStatusPending {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "仅待处理或接管中的工单可转交"})
		return
	}
	if !canHandleTicket(c, tk) {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "该工单由 " + orDash(tk.HandlerName) + " 接管中，无权转交"})
		return
	}
	to, err := repository.GetUserByID(body.ToUserID)
	if err != nil || to == nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "目标客服不存在"})
		return
	}
	if to.TenantID != tk.TenantID {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "目标客服不属于当前租户"})
		return
	}
	if to.Status != 1 {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "目标客服已禁用，不能转交"})
		return
	}
	toName := to.Nickname
	if toName == "" {
		toName = to.Username
	}
	operator := currentHandlerName(c)
	if err := service.TransferTicket(tk, to.ID, toName, operator); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	repository.WriteAuditLog(tk.TenantID, operator, "update", "ticket", strconv.FormatUint(tk.ID, 10),
		"转交工单："+tk.TicketNo+" → "+toName, c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已转交"})
}
