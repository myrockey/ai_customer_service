package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"customer_service/internal/middleware"
	"customer_service/internal/model"
	"customer_service/internal/pkg/crypto"
	"customer_service/internal/pkg/permission"
	"customer_service/internal/repository"

	"github.com/gin-gonic/gin"
)

// ============ RBAC：成员管理（租户内子账号） ============

// currentOperator 当前操作人标识（子账号=用户名；app_key 管理员=空，沿用既有审计约定）
func currentOperator(c *gin.Context) string {
	if uid := middleware.UserIDOf(c); uid > 0 {
		if u, err := repository.GetUserByID(uid); err == nil && u != nil {
			return u.Username
		}
	}
	return ""
}

// orDash 空值显示为 "-"
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// canHandleTicket 是否有权处理该工单：app_key 管理员 / 租户管理员（全权限）/ 接管人本人
func canHandleTicket(c *gin.Context, tk *model.Ticket) bool {
	uid := middleware.UserIDOf(c)
	if uid == 0 {
		return true // app_key 管理员
	}
	if repository.IsAdminUser(uid) {
		return true
	}
	return tk.HandlerUID == uid
}

// currentHandlerName 当前接管客服显示名（子账号=昵称优先，无则用户名；app_key 管理员=租户ID）
func currentHandlerName(c *gin.Context) string {
	if uid := middleware.UserIDOf(c); uid > 0 {
		if u, err := repository.GetUserByID(uid); err == nil && u != nil {
			if u.Nickname != "" {
				return u.Nickname
			}
			return u.Username
		}
	}
	return middleware.TenantOf(c)
}

// AdminMemberList 成员列表：GET /api/admin/members
// 筛选参数：q（用户名模糊）、role_id、status
func AdminMemberList(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	if err := repository.EnsureTenantRoles(tid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "角色初始化失败"})
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	roleID, _ := strconv.ParseUint(c.Query("role_id"), 10, 64)
	status := -1
	if s := strings.TrimSpace(c.Query("status")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && (v == 0 || v == 1) {
			status = v
		}
	}
	users, err := repository.ListUsers(tid, q, roleID, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询成员失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": users})
}

// AdminTicketTransferTargets 工单转交目标列表：GET /api/admin/tickets/transfer-targets
// 供客服/管理员转交工单时选择对象（只需 TicketHandle 权限，无需成员管理权限）；
// 返回同租户启用中的客服账号 id/昵称/用户名，不暴露 phone/email 等成员管理字段
func AdminTicketTransferTargets(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	users, err := repository.ListUsers(tid, "", 0, 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询客服失败"})
		return
	}
	list := make([]gin.H, 0, len(users))
	for _, u := range users {
		name := u.Nickname
		if name == "" {
			name = u.Username
		}
		list = append(list, gin.H{"id": u.ID, "nickname": name, "username": u.Username})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// AdminMemberCreate 添加成员：POST /api/admin/members
// body: {username, password, role_ids[]}（多角色）
func AdminMemberCreate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	var b struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		RoleIDs  []uint64 `json:"role_ids"`
		Nickname string   `json:"nickname"`
		Phone    string   `json:"phone"`
		Email    string   `json:"email"`
		Remark   string   `json:"remark"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || b.Username == "" || b.Password == "" || len(b.RoleIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误（至少分配一个角色）"})
		return
	}
	if utf8.RuneCountInString(b.Username) < 3 || utf8.RuneCountInString(b.Username) > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "用户名长度需 3-32 个字符"})
		return
	}
	if b.Phone != "" && !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(b.Phone) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "手机号格式不正确"})
		return
	}
	if b.Email != "" && !strings.Contains(b.Email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "邮箱格式不正确"})
		return
	}
	if len(b.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "密码长度至少 8 位"})
		return
	}
	if err := repository.EnsureTenantRoles(tid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "角色初始化失败"})
		return
	}
	if _, err := repository.GetUserByUsername(tid, b.Username); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "用户名已存在"})
		return
	}
	u := &model.User{
		TenantID:     tid,
		Username:     b.Username,
		PasswordHash: crypto.HashUserPassword(tid, b.Username, b.Password),
		Nickname:     strings.TrimSpace(b.Nickname),
		Phone:        strings.TrimSpace(b.Phone),
		Email:        strings.TrimSpace(b.Email),
		Remark:       strings.TrimSpace(b.Remark),
		Status:       1,
	}
	if err := repository.CreateUser(u, b.RoleIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建成员失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "create", "member", b.Username, "添加成员", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "成员已创建", "data": gin.H{"id": u.ID}})
}

// AdminMemberRoles 修改成员角色（覆盖式）：PUT /api/admin/members/:id/roles
// body: {role_ids: []}
func AdminMemberRoles(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if id == middleware.UserIDOf(c) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不能修改自己的角色"})
		return
	}
	var b struct {
		RoleIDs []uint64 `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || len(b.RoleIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "至少分配一个角色"})
		return
	}
	u, err := repository.GetUserByID(id)
	if err != nil || u.TenantID != tid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "成员不存在"})
		return
	}
	if err := repository.SetUserRoles(tid, id, b.RoleIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新角色失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "update", "member", strconv.FormatUint(id, 10), "变更成员角色", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "角色已更新"})
}

// AdminMemberUpdate 编辑成员（用户名 + 角色 + 资料，一次提交）：PUT /api/admin/members/:id
// body: {username, nickname, phone, email, remark, role_ids[]}；字段都可选，只传一个则只改对应项
func AdminMemberUpdate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if id == middleware.UserIDOf(c) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不能修改自己的账号信息"})
		return
	}
	var b struct {
		Username string   `json:"username"`
		Nickname string   `json:"nickname"`
		Phone    string   `json:"phone"`
		Email    string   `json:"email"`
		Remark   string   `json:"remark"`
		RoleIDs  []uint64 `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || (strings.TrimSpace(b.Username) == "" && b.Nickname == "" && b.Phone == "" && b.Email == "" && b.Remark == "" && len(b.RoleIDs) == 0) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	u, err := repository.GetUserByID(id)
	if err != nil || u.TenantID != tid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "成员不存在"})
		return
	}
	updates := map[string]interface{}{}
	if strings.TrimSpace(b.Username) != "" && b.Username != u.Username {
		name := strings.TrimSpace(b.Username)
		if utf8.RuneCountInString(name) < 3 || utf8.RuneCountInString(name) > 32 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "用户名长度需 3-32 个字符"})
			return
		}
		if exist, err := repository.GetUserByUsername(tid, name); err == nil && exist.ID != id {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "用户名已存在"})
			return
		}
		updates["username"] = name
	}
	if b.Nickname != "" && b.Nickname != u.Nickname {
		updates["nickname"] = strings.TrimSpace(b.Nickname)
	}
	if b.Phone != "" && b.Phone != u.Phone {
		if !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(b.Phone) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "手机号格式不正确"})
			return
		}
		updates["phone"] = strings.TrimSpace(b.Phone)
	}
	if b.Email != "" && b.Email != u.Email {
		if !strings.Contains(b.Email, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "邮箱格式不正确"})
			return
		}
		updates["email"] = strings.TrimSpace(b.Email)
	}
	if b.Remark != "" && b.Remark != u.Remark {
		updates["remark"] = strings.TrimSpace(b.Remark)
	}
	if len(updates) > 0 {
		if err := repository.UpdateUserFields(tid, id, updates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新成员信息失败"})
			return
		}
	}
	if len(b.RoleIDs) > 0 {
		if err := repository.SetUserRoles(tid, id, b.RoleIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新角色失败"})
			return
		}
	}
	repository.WriteAuditLog(tid, currentOperator(c), "update", "member", strconv.FormatUint(id, 10), "编辑成员信息", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "成员已更新"})
}

// AdminMemberStatus 启用/禁用成员：PUT /api/admin/members/:id/status
func AdminMemberStatus(c *gin.Context) {
	tid := middleware.TenantOf(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if id == middleware.UserIDOf(c) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不能操作自己的账号"})
		return
	}
	var b struct {
		Status int `json:"status"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || (b.Status != 0 && b.Status != 1) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if err := repository.UpdateUserStatus(tid, id, b.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败"})
		return
	}
	action := "disable"
	if b.Status == 1 {
		action = "enable"
	}
	repository.WriteAuditLog(tid, currentOperator(c), action, "member", strconv.FormatUint(id, 10), "变更成员状态", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已更新"})
}

// AdminMemberPassword 重置成员密码：PUT /api/admin/members/:id/password
func AdminMemberPassword(c *gin.Context) {
	tid := middleware.TenantOf(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	var b struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || len(b.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "密码长度至少 8 位"})
		return
	}
	u, err := repository.GetUserByID(id)
	if err != nil || u.TenantID != tid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "成员不存在"})
		return
	}
	hash := crypto.HashUserPassword(tid, u.Username, b.Password)
	if err := repository.UpdateUserPassword(tid, id, hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "重置失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "reset_password", "member", u.Username, "重置成员密码", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "密码已重置"})
}

// AdminMemberDelete 删除成员：DELETE /api/admin/members/:id
func AdminMemberDelete(c *gin.Context) {
	tid := middleware.TenantOf(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if id == middleware.UserIDOf(c) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不能删除自己的账号"})
		return
	}
	u, err := repository.GetUserByID(id)
	if err != nil || u.TenantID != tid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "成员不存在"})
		return
	}
	if err := repository.DeleteUser(tid, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "delete", "member", u.Username, "删除成员", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "成员已删除"})
}

// ============ RBAC：角色管理 ============

// RoleItem 角色列表条目（含菜单授权回显）
type RoleItem struct {
	ID          uint64   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MenuIDs     []uint64 `json:"menu_ids"`
	IsAdmin     bool     `json:"is_admin"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// AdminRoleList 角色列表：GET /api/admin/roles
func AdminRoleList(c *gin.Context) {
	tid := middleware.TenantOf(c)
	if tid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未获取到租户信息"})
		return
	}
	if err := repository.EnsureTenantRoles(tid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "角色初始化失败: " + err.Error()})
		return
	}
	roles, err := repository.ListRoles(tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询角色失败"})
		return
	}
	items := make([]RoleItem, 0, len(roles))
	for _, r := range roles {
		items = append(items, RoleItem{
			ID: r.ID, TenantID: r.TenantID, Code: r.Code, Name: r.Name,
			Description: r.Description, MenuIDs: r.MenuIDs, IsAdmin: r.IsAdmin,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: r.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// AdminPermissionCatalog 权限目录：GET /api/admin/permissions
// 从 menus 表推导（菜单=权限载体），保证与菜单管理页一致
func AdminPermissionCatalog(c *gin.Context) {
	menus, err := repository.ListMenus()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询权限目录失败"})
		return
	}
	items := make([]permission.Item, 0, len(menus))
	for _, m := range menus {
		if m.Perm == nil || *m.Perm == "" {
			continue
		}
		items = append(items, permission.Item{Code: *m.Perm, Name: m.Name, Group: m.GroupName})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// AdminRoleCreate 新建角色：POST /api/admin/roles
func AdminRoleCreate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	var b struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		MenuIDs     []uint64 `json:"menu_ids"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || strings.TrimSpace(b.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	if err := repository.EnsureTenantRoles(tid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "角色初始化失败"})
		return
	}
	role := &model.Role{
		TenantID: tid, Code: genRoleCode(), Name: strings.TrimSpace(b.Name),
		Description: b.Description, MenuIDs: b.MenuIDs,
	}
	if err := repository.CreateRole(role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建角色失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "create", "role", role.Name, "新建角色", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "角色已创建", "data": gin.H{"id": role.ID}})
}

// AdminRoleUpdate 更新角色（内置角色权限允许修改）：PUT /api/admin/roles/:id
func AdminRoleUpdate(c *gin.Context) {
	tid := middleware.TenantOf(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var b struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		MenuIDs     []uint64 `json:"menu_ids"`
	}
	if err := c.ShouldBindJSON(&b); err != nil || id == 0 || strings.TrimSpace(b.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	role, err := repository.GetRole(id)
	if err != nil || role.TenantID != tid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "角色不存在"})
		return
	}
	if err := repository.UpdateRole(id, strings.TrimSpace(b.Name), b.Description, b.MenuIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新角色失败"})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "update", "role", role.Name, "更新角色权限", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "角色已更新"})
}

// AdminRoleDelete 删除角色（预置角色同样可删）：DELETE /api/admin/roles/:id
// 成员引用自动解绑（role_id 置 0），删除后 app_key 管理员不受影响
func AdminRoleDelete(c *gin.Context) {
	tid := middleware.TenantOf(c)
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	role, err := repository.GetRole(id)
	if err != nil || role.TenantID != tid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "角色不存在"})
		return
	}
	if err := repository.DeleteRole(tid, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	repository.WriteAuditLog(tid, currentOperator(c), "delete", "role", role.Name, "删除角色", c.ClientIP(), middleware.UserIDOf(c))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "角色已删除"})
}

// genRoleCode 生成租户内唯一角色编码：custom_<12位hex>（碰撞概率可忽略，创建后不可改）
func genRoleCode() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("custom_%d", time.Now().UnixNano()%1000000)
	}
	return "custom_" + hex.EncodeToString(b)
}
