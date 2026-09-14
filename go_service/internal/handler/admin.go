package handler

import (
	"customer_service/internal/config"
	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/repository"
	"net/http"
	"strconv"
	"customer_service/internal/service"

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
	result, err := service.ListTickets(tid, service.TicketListInput{
		Status: status,
		Page:   page,
		Size:   size,
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
	msg, err := service.TakeoverTicket(tk, body.WaitTime, c.GetString(config.TraceIDKey))
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
	if err := service.CloseTicket(tk); err != nil {
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
	if err := service.SetTicketPriority(tk, body.Priority, "admin", body.Remark); err != nil {
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
