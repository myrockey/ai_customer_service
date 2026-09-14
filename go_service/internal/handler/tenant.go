package handler

import (
	"fmt"
	"log"
	"net/http"

	"customer_service/internal/pkg/cache"
	"customer_service/internal/service"

	"github.com/gin-gonic/gin"
)

// ============ 租户管理（仅平台管理员） ============
// P4-2：引入 service 层，handler 只负责参数解析和响应返回

// tenantList 租户列表：GET /api/admin/tenants
func TenantList(c *gin.Context) {
	input := service.TenantListInput{
		Name:   c.Query("name"),
		AppKey: c.Query("app_key"),
		Status: c.Query("status"),
		Sort:   c.Query("sort"),
	}
	list, err := service.ListTenants(input)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// tenantCreate 创建租户：POST /api/admin/tenants {name, app_key?}
// 自动生成 tenant_id / app_secret；app_secret 仅此一次返回明文
func TenantCreate(c *gin.Context) {
	var b struct {
		Name   string `json:"name"`
		AppKey string `json:"app_key"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || b.Name == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "name 不能为空"})
		return
	}
	result, err := service.CreateTenant(b.Name, b.AppKey)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"tenant_id":  result.TenantID,
		"name":       result.Name,
		"app_key":    result.AppKey,
		"app_secret": result.AppSecret,
		"tip":        "请立即保存 app_secret，仅此一次显示",
	}})
}

// tenantResetSecret 重置密钥：POST /api/admin/tenants/:tid/reset-secret
func TenantResetSecret(c *gin.Context) {
	tid := c.Param("tid")
	appSecret, err := service.ResetTenantSecret(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"tenant_id":  tid,
		"app_secret": appSecret,
		"tip":        "请立即保存新的 app_secret，仅此一次显示",
	}})
}

// tenantToggle 启用/禁用租户：POST /api/admin/tenants/:tid/toggle
func TenantToggle(c *gin.Context) {
	tid := c.Param("tid")
	newStatus, err := service.ToggleTenantStatus(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	statusText := "已禁用"
	if newStatus == 1 {
		statusText = "已启用"
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "租户" + statusText, "data": gin.H{
		"tenant_id": tid, "status": newStatus,
	}})
}

// tenantDetail 租户详情：GET /api/admin/tenants/:tid
func TenantDetail(c *gin.Context) {
	tid := c.Param("tid")
	row, err := service.GetTenantDetail(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": row})
}

// tenantModelConfig 平台管理员查看指定租户模型配置：GET /api/admin/tenants/:tid/model-config
func TenantModelConfig(c *gin.Context) {
	tid := c.Param("tid")
	result, err := service.GetBusinessModelConfig(tid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// tenantModelConfigUpdate 平台管理员代配租户模型：PUT /api/admin/tenants/:tid/model-config
func TenantModelConfigUpdate(c *gin.Context) {
	tid := c.Param("tid")
	var input service.UpdateModelConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	updates, err := service.UpdateModelConfig(tid, &input)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	if updates == nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "没有需要更新的字段"})
		return
	}
	// 通知 Agent 清除该租户模型配置缓存（按变更范围精确清除：chat/embedding/all），使新配置立即生效（失败不影响主流程）
	// 强制 Redis 广播：单实例自订阅自消费，多实例全量消费；发布失败仅记日志
	// 平台管理员配置（t_admin = 平台默认）影响所有「跟随平台」的租户，
	// 必须广播 tenant_id="" 清全部租户的检索器/Agent 缓存，否则跟随租户仍用旧 embedding/模型实例。
	go func() {
		scope := service.ModelUpdateScope(updates)
		target := tid
		if tid == "t_admin" {
			target = ""
		}
		if err := cache.Publish("cs:model:cache:invalidate", fmt.Sprintf(`{"tenant_id":%q,"scope":%q}`, target, scope)); err != nil {
			log.Printf("⚠️ Redis 发布缓存失效广播失败: %v", err)
		}
	}()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "模型配置已保存", "data": gin.H{
		"tenant_id": tid, "updated": updates,
	}})
}

// tenantUpdate 更新租户：PUT /api/admin/tenants/:tid {name}
func TenantUpdate(c *gin.Context) {
	tid := c.Param("tid")
	var b struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || b.Name == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "name 不能为空"})
		return
	}
	if err := service.UpdateTenant(tid, b.Name); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "租户已更新", "data": gin.H{
		"tenant_id": tid, "name": b.Name,
	}})
}

// tenantUpdateQuota 更新租户配额：PUT /api/admin/tenants/:tid/quota（P1-2）
func TenantUpdateQuota(c *gin.Context) {
	tid := c.Param("tid")
	var b struct {
		MaxConcurrentSessions int `json:"max_concurrent_sessions"`
		DailyMessageLimit     int `json:"daily_message_limit"`
		MaxKbDocs             int `json:"max_kb_docs"`
		MaxKbSizeMB           int `json:"max_kb_size_mb"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	// 负数视为不限制
	if b.MaxConcurrentSessions < 0 {
		b.MaxConcurrentSessions = 0
	}
	if b.DailyMessageLimit < 0 {
		b.DailyMessageLimit = 0
	}
	if b.MaxKbDocs < 0 {
		b.MaxKbDocs = 0
	}
	if b.MaxKbSizeMB < 0 {
		b.MaxKbSizeMB = 0
	}
	if err := service.UpdateTenantQuota(tid, b.MaxConcurrentSessions, b.DailyMessageLimit, b.MaxKbDocs, b.MaxKbSizeMB); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "租户配额已更新"})
}

// tenantDelete 删除租户：DELETE /api/admin/tenants/:tid
// 级联删除：工单、消息、Prompt、知识库数据
func TenantDelete(c *gin.Context) {
	tid := c.Param("tid")
	// 禁止删除平台管理员租户
	if tid == "t_admin" {
		c.JSON(http.StatusOK, gin.H{"code": 403, "msg": "不能删除平台管理员租户"})
		return
	}
	if err := service.DeleteTenant(tid); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "租户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已删除租户 " + tid + "（含工单/消息/Prompt/知识库数据）"})
}
