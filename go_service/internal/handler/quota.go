package handler

import (
	"net/http"

	"customer_service/internal/pkg/ws"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// AgentTenantQuota P1-2：Agent 端配额校验内部接口（InternalAuth 保护）
// GET /api/agent/tenant-quota?tenant_id=xxx → {code:0, data:{max_concurrent_sessions, daily_message_limit, max_kb_docs, max_kb_size_mb}}
func AgentTenantQuota(c *gin.Context) {
	tid := c.Query("tenant_id")
	if tid == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 tenant_id"})
		return
	}
	row, err := repository.GetTenantQuota(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在:" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"tenant_id":               tid,
		"max_concurrent_sessions": row.MaxConcurrentSessions,
		"daily_message_limit":     row.DailyMessageLimit,
		"max_kb_docs":             row.MaxKbDocs,
		"max_kb_size_mb":          row.MaxKbSizeMB,
	}})
}

// AgentTenantConcurrency P1-2：Agent 端查询租户实时在线连接数（并发会话配额用）
// GET /api/agent/tenant-concurrency?tenant_id=xxx → {code:0, data:{tenant_id, concurrent_sessions}}
func AgentTenantConcurrency(c *gin.Context) {
	tid := c.Query("tenant_id")
	if tid == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "缺少 tenant_id"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"tenant_id":          tid,
		"concurrent_sessions": ws.WSHub.CountTenantConns(tid),
	}})
}
