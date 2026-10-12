package handler

import (
	"net/http"

	"customer_service/internal/middleware"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// AdminOpsDashboard 运营看板聚合：GET /api/admin/ops/dashboard
// 返回会话/消息/工单/满意度总览、近7天趋势、工单状态分布、客服处理量排行
func AdminOpsDashboard(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	data, err := repository.BuildDashboard(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "统计失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}
