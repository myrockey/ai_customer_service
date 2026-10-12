package handler

import (
	"net/http"

	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// AdminAgentStatusList 在线客服视图：GET /api/admin/agent-status
// 返回同租户所有接入状态的客服（含昵称/状态/处理量/上限），前端状态条与分配下拉用
func AdminAgentStatusList(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	rows, err := repository.ListAgentStatus(tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询客服状态失败"})
		return
	}
	if rows == nil {
		rows = []model.AgentStatusRow{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rows})
}

// AdminAgentStatusUpdate 客服切换自己的状态：PUT /api/admin/agent-status
// body: {status: 0离线|1在线|2忙碌}；仅修改本人，不动处理量
func AdminAgentStatusUpdate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	uid := middleware.UserIDOf(c)
	if tid == "" || uid == 0 {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "app_key 管理员无需设置状态"})
		return
	}
	var b struct {
		Status int `json:"status"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || (b.Status != 0 && b.Status != 1 && b.Status != 2) {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if err := repository.SetAgentStatus(tid, uid, b.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已更新"})
}
