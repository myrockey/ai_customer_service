package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/pkg/auth"
	"customer_service/internal/repository"
)

// ====================== 菜单权限（RuoYi 模式：菜单=权限载体） ======================

// AdminMenuTree 当前用户可访问菜单树：GET /api/admin/menus/tree
// 按登录身份隔离可见菜单（平台/租户各自只看自己的菜单）：
//   - 平台管理员（app_key 且租户 is_platform_admin）→ 仅系统管理组（平台级菜单）
//   - 租户管理员（app_key 普通租户）→ 业务中心组全部节点（隐式全权）
//   - 租户子账号（uid>0）→ 业务中心组按权限过滤保留父链
func AdminMenuTree(c *gin.Context) {
	uid := middleware.UserIDOf(c)
	tid := middleware.TenantOf(c)
	info, ok := auth.TenantInfo(tid)
	scope := repository.MenuScopeAll // 演示模式（未开启鉴权，无租户上下文）→ 全量菜单
	if ok {
		if info.IsPlatformAdmin {
			scope = repository.MenuScopePlatform
		} else if uid == 0 {
			scope = repository.MenuScopeTenantAll
		} else {
			scope = repository.MenuScopeTenantUser
		}
	}
	var perms []string
	if uid > 0 {
		perms = repository.GetUserPerms(uid)
	}
	tree := repository.MenuTreeForUser(perms, scope)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": tree})
}

// AdminMenuList 全量菜单（管理页）：GET /api/admin/menus
func AdminMenuList(c *gin.Context) {
	menus, err := repository.ListMenus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询菜单失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": menus})
}

// AdminMenuCreate 新增菜单节点：POST /api/admin/menus
func AdminMenuCreate(c *gin.Context) {
	var b struct {
		ParentID  uint64  `json:"parent_id"`
		GroupName string  `json:"group_name"`
		Name      string  `json:"name"`
		Type      int     `json:"type"`
		Path      string  `json:"path"`
		Component string  `json:"component"`
		Perm      *string `json:"perm"`
		Icon      string  `json:"icon"`
		Sort      int     `json:"sort"`
		Visible   int     `json:"visible"`
		Status    int     `json:"status"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || strings.TrimSpace(b.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	m := &model.Menu{
		ParentID: b.ParentID, GroupName: b.GroupName, Name: strings.TrimSpace(b.Name),
		Type: b.Type, Path: b.Path, Component: b.Component, Perm: normalizePerm(b.Perm),
		Icon: b.Icon, Sort: b.Sort, Visible: b.Visible, Status: b.Status,
	}
	if m.Type == 0 {
		m.Type = 2
	}
	// visible/status 前端始终显式传值（1 或 0），此处不再强制改写，否则「隐藏/停用」会被静默改回「显示/启用」
	if m.ParentID > 0 {
		if _, err := repository.GetMenu(m.ParentID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "父菜单不存在"})
			return
		}
	}
	if err := repository.CreateMenu(m); err != nil {
		if isDuplicateErr(err) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "权限码已存在，请更换"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建菜单失败"})
		return
	}
	repository.WriteAuditLog(middleware.TenantOf(c), currentOperator(c), "create", "menu", m.Name, "新增菜单", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "菜单已创建", "data": gin.H{"id": m.ID}})
}

// AdminMenuUpdate 更新菜单节点：PUT /api/admin/menus/:id
func AdminMenuUpdate(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var b struct {
		ParentID  *uint64 `json:"parent_id"`
		GroupName *string `json:"group_name"`
		Name      *string `json:"name"`
		Type      *int    `json:"type"`
		Path      *string `json:"path"`
		Component *string `json:"component"`
		Perm      *string `json:"perm"`
		Icon      *string `json:"icon"`
		Sort      *int    `json:"sort"`
		Visible   *int    `json:"visible"`
		Status    *int    `json:"status"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	old, err := repository.GetMenu(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "菜单不存在"})
		return
	}
	fields := map[string]interface{}{}
	if b.ParentID != nil {
		if *b.ParentID > 0 && *b.ParentID != id {
			if _, err := repository.GetMenu(*b.ParentID); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "父菜单不存在"})
				return
			}
		}
		fields["parent_id"] = *b.ParentID
	}
	if b.GroupName != nil {
		fields["group_name"] = *b.GroupName
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
		fields["name"] = strings.TrimSpace(*b.Name)
	}
	if b.Type != nil {
		fields["type"] = *b.Type
	}
	if b.Path != nil {
		fields["path"] = *b.Path
	}
	if b.Component != nil {
		fields["component"] = *b.Component
	}
	if b.Perm != nil {
		fields["perm"] = normalizePerm(b.Perm)
	}
	if b.Icon != nil {
		fields["icon"] = *b.Icon
	}
	if b.Sort != nil {
		fields["sort"] = *b.Sort
	}
	if b.Visible != nil {
		fields["visible"] = *b.Visible
	}
	if b.Status != nil {
		fields["status"] = *b.Status
	}
	if err := repository.UpdateMenu(id, fields); err != nil {
		if isDuplicateErr(err) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "权限码已存在，请更换"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新菜单失败"})
		return
	}
	repository.WriteAuditLog(middleware.TenantOf(c), currentOperator(c), "update", "menu", old.Name, "更新菜单", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "菜单已更新"})
}

// AdminMenuDelete 删除菜单节点：DELETE /api/admin/menus/:id
// 级联删除子节点与角色关联；返回被删节点 perm（前端提示后端接口引用）
func AdminMenuDelete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	old, err := repository.GetMenu(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "菜单不存在"})
		return
	}
	perms, err := repository.DeleteMenu(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除菜单失败"})
		return
	}
	repository.WriteAuditLog(middleware.TenantOf(c), currentOperator(c), "delete", "menu", old.Name, "删除菜单", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "菜单已删除", "data": gin.H{"removed_perms": perms}})
}

// AdminMenuBatchUpdate 批量更新菜单（状态/可见性/排序）：POST /api/admin/menus/batch-update
// body: { "ids":[1,2,3], "status":1, "visible":1, "sort":1 }（字段可选，传了才更新）
func AdminMenuBatchUpdate(c *gin.Context) {
	var b struct {
		IDs     []uint64 `json:"ids"`
		Status  *int     `json:"status"`
		Visible *int     `json:"visible"`
		Sort    *int     `json:"sort"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || len(b.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	fields := map[string]interface{}{}
	if b.Status != nil {
		if *b.Status != 0 && *b.Status != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "状态取值非法"})
			return
		}
		fields["status"] = *b.Status
	}
	if b.Visible != nil {
		if *b.Visible != 0 && *b.Visible != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "可见性取值非法"})
			return
		}
		fields["visible"] = *b.Visible
	}
	if b.Sort != nil {
		fields["sort"] = *b.Sort
	}
	if len(fields) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "缺少更新字段"})
		return
	}
	if err := repository.BatchUpdateMenus(b.IDs, fields); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "批量更新菜单失败"})
		return
	}
	repository.WriteAuditLog(middleware.TenantOf(c), currentOperator(c), "update", "menu", "批量菜单", "批量更新菜单", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已批量更新 " + strconv.Itoa(len(b.IDs)) + " 条菜单"})
}

// AdminMenuResetBuiltin 重置内置菜单：POST /api/admin/menus/reset-builtin
func AdminMenuResetBuiltin(c *gin.Context) {
	if err := repository.ResetBuiltinMenus(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "重置内置菜单失败"})
		return
	}
	repository.WriteAuditLog(middleware.TenantOf(c), currentOperator(c), "update", "menu", "内置菜单", "重置内置菜单", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "内置菜单已重置为初始状态"})
}

// normalizePerm 空串统一为 nil（唯一索引允许多个 NULL）
func normalizePerm(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	if t == "" {
		return nil
	}
	return &t
}

// isDuplicateErr 是否唯一键冲突
func isDuplicateErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}
