package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"customer_service/internal/config"
	"customer_service/internal/model"

	"github.com/gin-gonic/gin"
)

// ============ 模型提供商管理（平台管理员 CRUD） ============
// 模型提供商仅维护元信息（名称/默认模型/请求地址），API Key 不存此表，
// 租户 Key 存 tenants.model_api_key / tenants.embedding_api_key（AES 密文）

// listModelProviders 获取模型提供商列表：GET /api/admin/model-providers
func ListModelProviders(c *gin.Context) {
	var providers []model.ModelProvider
	if err := config.DB.Order("sort_order ASC, id ASC").Find(&providers).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": providers})
}

// createModelProvider 新增模型提供商：POST /api/admin/model-providers
func CreateModelProvider(c *gin.Context) {
	var b struct {
		ProviderKey    string `json:"provider_key" binding:"required"`
		ProviderName   string `json:"provider_name" binding:"required"`
		DefaultModel   string `json:"default_model"`
		DefaultAPIBase string `json:"default_api_base"`
		ProviderType   string `json:"provider_type"`
		SortOrder      int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	if b.ProviderType == "" {
		b.ProviderType = "both"
	}
	provider := model.ModelProvider{
		ProviderKey:    b.ProviderKey,
		ProviderName:   b.ProviderName,
		DefaultModel:   b.DefaultModel,
		DefaultAPIBase: b.DefaultAPIBase,
		ProviderType:   b.ProviderType,
		SortOrder:      b.SortOrder,
		Status:         1,
	}
	if err := config.DB.Create(&provider).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "模型提供商已创建", "data": provider})
}

// updateModelProvider 更新模型提供商：PUT /api/admin/model-providers/:id
func UpdateModelProvider(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "无效的 ID"})
		return
	}
	var provider model.ModelProvider
	if err := config.DB.First(&provider, id).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "模型提供商不存在"})
		return
	}
	var b struct {
		ProviderName   *string `json:"provider_name"`
		DefaultModel   *string `json:"default_model"`
		DefaultAPIBase *string `json:"default_api_base"`
		ProviderType   *string `json:"provider_type"`
		SortOrder      *int    `json:"sort_order"`
		Status         *int    `json:"status"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	updates := map[string]interface{}{}
	if b.ProviderName != nil {
		updates["provider_name"] = *b.ProviderName
	}
	if b.DefaultModel != nil {
		updates["default_model"] = *b.DefaultModel
	}
	if b.DefaultAPIBase != nil {
		updates["default_api_base"] = *b.DefaultAPIBase
	}
	if b.ProviderType != nil {
		updates["provider_type"] = *b.ProviderType
	}
	if b.SortOrder != nil {
		updates["sort_order"] = *b.SortOrder
	}
	if b.Status != nil {
		updates["status"] = *b.Status
	}
	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "没有需要更新的字段"})
		return
	}
	if err := config.DB.Model(&provider).Updates(updates).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "模型提供商已更新", "data": updates})
}

// deleteModelProvider 删除模型提供商：DELETE /api/admin/model-providers/:id
// 任何提供商（含旧版 custom 数据）均可删除；被租户引用时提示先调整租户配置
func DeleteModelProvider(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "无效的 ID"})
		return
	}
	var provider model.ModelProvider
	if err := config.DB.First(&provider, id).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "模型提供商不存在"})
		return
	}
	// 被租户引用时提示（不硬删，避免租户配置悬空）
	var refCnt int64
	if err := config.DB.Model(&model.TenantRow{}).
		Where("llm_provider_id = ? OR embedding_provider_id = ?", id, id).Count(&refCnt).Error; err == nil && refCnt > 0 {
		c.JSON(http.StatusOK, gin.H{"code": 403, "msg": fmt.Sprintf("有 %d 个租户正在使用该提供商，请先调整租户配置", refCnt)})
		return
	}
	if err := config.DB.Delete(&provider).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "模型提供商已删除"})
}

// ============ 普通租户获取可用模型提供商列表 ============

// listModelProvidersForTenant 普通租户获取启用的模型提供商列表：GET /api/business/model-providers
// type 参数：chat=对话模型, embedding=向量模型, 不传=两者都支持的
func ListModelProvidersForTenant(c *gin.Context) {
	providerType := c.Query("type")
	var providers []model.ModelProvider
	query := config.DB.Where("status = 1")
	if providerType != "" {
		// 支持指定类型或 both
		query = query.Where("provider_type = ? OR provider_type = 'both'", providerType)
	}
	if err := query.Order("sort_order ASC, id ASC").Find(&providers).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": providers})
}
