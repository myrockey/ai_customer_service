package handler

import (
	"net/http"
	"strconv"

	"customer_service/internal/middleware"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// AuditLogs P1-3：审计日志列表
// 平台管理员：全览所有租户；租户管理端：仅本租户
// 支持筛选：time_from/time_to、action、object_type、operator、ip
func AuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	tenantID := ""
	if info, ok := auth.TenantInfo(middleware.TenantOf(c)); !ok || !info.IsPlatformAdmin {
		tenantID = middleware.TenantOf(c)
	}

	filter := repository.AuditLogFilter{
		Action:     c.Query("action"),
		ObjectType: c.Query("object_type"),
		Operator:   c.Query("operator"),
		IP:         c.Query("ip"),
		TimeFrom:   c.Query("time_from"),
		TimeTo:     c.Query("time_to"),
	}

	logs, total, err := repository.ListAuditLogs(tenantID, filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询审计日志失败:" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}})
}

// ArchiveMessages P1-4：手动触发消息归档（超过保留期迁移到 messages_archive）
// 租户管理端仅归档本租户消息；平台管理员可全量归档。
func ArchiveMessages(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "90"))
	if days <= 0 {
		days = 90
	}
	if days > 365 {
		days = 365
	}
	tenantID := ""
	if info, ok := auth.TenantInfo(middleware.TenantOf(c)); !ok || !info.IsPlatformAdmin {
		tenantID = middleware.TenantOf(c)
	}
	cnt := repository.ArchiveOldMessages(days, tenantID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "归档完成", "data": gin.H{"archived": cnt}})
}

// ArchiveMessagesCount P1-4：可归档消息统计（租户仅自身，平台管理员全览）
// 返回超过保留期且无活跃工单归属的可归档条数，供管理端"可归档 N 条"展示
func ArchiveMessagesCount(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "90"))
	if days <= 0 {
		days = 90
	}
	if days > 365 {
		days = 365
	}
	tenantID := ""
	if info, ok := auth.TenantInfo(middleware.TenantOf(c)); !ok || !info.IsPlatformAdmin {
		tenantID = middleware.TenantOf(c)
	}
	cnt, err := repository.CountArchivableMessages(days, tenantID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "查询失败:" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"count": cnt}})
}
