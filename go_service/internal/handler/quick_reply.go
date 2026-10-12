package handler

import (
	"net/http"
	"strconv"
	"strings"

	"customer_service/internal/config"
	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// AdminQuickReplyList 快捷回复列表：GET /api/admin/quick-replies
// 参数：category（分类筛选）、q（标题/内容关键词）、active（1启用 0停用，空=全部）
func AdminQuickReplyList(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	category := strings.TrimSpace(c.Query("category"))
	active := -1
	if s := strings.TrimSpace(c.Query("active")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && (v == 0 || v == 1) {
			active = v
		}
	}
	where := "tenant_id = ?"
	args := []interface{}{tid}
	if q != "" {
		where += " AND (title LIKE ? OR content LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	if category != "" {
		where += " AND category = ?"
		args = append(args, category)
	}
	if active >= 0 {
		where += " AND is_active = ?"
		args = append(args, active)
	}
	var list []model.QuickReply
	if err := config.DB.Where(where, args...).Order("sort ASC, id DESC").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询快捷回复失败"})
		return
	}
	if list == nil {
		list = []model.QuickReply{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// AdminQuickReplyCreate 新增话术：POST /api/admin/quick-replies
// body: {category, title, content, sort?, is_active?}
func AdminQuickReplyCreate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	var b struct {
		Category string `json:"category"`
		Title    string `json:"title"`
		Content  string `json:"content"`
		Sort     int    `json:"sort"`
		IsActive *int   `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	b.Title = strings.TrimSpace(b.Title)
	b.Content = strings.TrimSpace(b.Content)
	if b.Title == "" || b.Content == "" {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "标题和内容不能为空"})
		return
	}
	qr := model.QuickReply{
		TenantID: tid,
		Category: b.Category,
		Title:    b.Title,
		Content:  b.Content,
		Sort:     b.Sort,
		IsActive: 1,
	}
	if b.IsActive != nil && (*b.IsActive == 0 || *b.IsActive == 1) {
		qr.IsActive = *b.IsActive
	}
	if qr.Category == "" {
		qr.Category = "通用"
	}
	if err := config.DB.Create(&qr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "新增快捷回复失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "create", "quick_reply", strconv.FormatUint(qr.ID, 10), "新增快捷回复："+qr.Title, c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": qr})
}

// AdminQuickReplyUpdate 编辑/启停：PUT /api/admin/quick-replies/:id
func AdminQuickReplyUpdate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var qr model.QuickReply
	if err := config.DB.Where("tenant_id = ? AND id = ?", tid, id).First(&qr).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "快捷回复不存在"})
		return
	}
	var b struct {
		Category *string `json:"category"`
		Title    *string `json:"title"`
		Content  *string `json:"content"`
		Sort     *int    `json:"sort"`
		IsActive *int    `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	fields := map[string]interface{}{}
	if b.Category != nil {
		v := strings.TrimSpace(*b.Category)
		if v == "" {
			v = "通用"
		}
		fields["category"] = v
	}
	if b.Title != nil {
		v := strings.TrimSpace(*b.Title)
		if v == "" {
			c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "标题不能为空"})
			return
		}
		fields["title"] = v
	}
	if b.Content != nil {
		v := strings.TrimSpace(*b.Content)
		if v == "" {
			c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "内容不能为空"})
			return
		}
		fields["content"] = v
	}
	if b.Sort != nil {
		fields["sort"] = *b.Sort
	}
	if b.IsActive != nil && (*b.IsActive == 0 || *b.IsActive == 1) {
		fields["is_active"] = *b.IsActive
	}
	if len(fields) > 0 {
		if err := config.DB.Model(&qr).Updates(fields).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新快捷回复失败"})
			return
		}
	}
	repository.WriteAuditLog(tid, currentOperator(c), "update", "quick_reply", strconv.FormatUint(qr.ID, 10), "编辑快捷回复："+qr.Title, c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已更新"})
}

// AdminQuickReplyDelete 删除话术（软删）：DELETE /api/admin/quick-replies/:id
func AdminQuickReplyDelete(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var qr model.QuickReply
	if err := config.DB.Where("tenant_id = ? AND id = ?", tid, id).First(&qr).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 404, "msg": "快捷回复不存在"})
		return
	}
	if err := config.DB.Delete(&qr).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除快捷回复失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "delete", "quick_reply", strconv.FormatUint(qr.ID, 10), "删除快捷回复："+qr.Title, c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已删除"})
}
