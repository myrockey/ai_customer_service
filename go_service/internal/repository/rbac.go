package repository

import (
	"errors"
	"sync/atomic"
	"time"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/permission"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ====================== 内置角色种子（懒初始化，每租户两条） ======================

// EnsureTenantRoles 为租户做一次性的预置角色初始化（admin/staff）。
// 新项目语义（不做存量兼容）：
//   - 从未初始化（rbac_seeded=0）且无角色 → 种入 admin/staff，并写入菜单授权（admin=业务中心全量，staff=客服权限）；
//   - 已初始化 / 已有角色 → 跳过（预置角色可删可改，删除后不自动补种）。
func EnsureTenantRoles(tenantID string) error {
	builtins := []model.Role{
		{TenantID: tenantID, Code: "admin", Name: "管理员", Description: "租户全部权限"},
		{TenantID: tenantID, Code: "staff", Name: "客服", Description: "会话/工单/客户/运营看板/知识库查看"},
	}
	return config.DB.Transaction(func(tx *gorm.DB) error {
		var cnt int64
		if err := tx.Model(&model.Role{}).Where("tenant_id = ?", tenantID).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			return nil // 已有角色（含被删光后重建的自定义角色）：不补种
		}
		var t model.TenantRow
		locked := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ?", tenantID).First(&t).Error == nil
		if locked && t.RbacSeeded {
			return nil // 已初始化且角色被删光：不补种
		}
		for _, r := range builtins {
			now := time.Now()
			r.CreatedAt = now
			r.UpdatedAt = now
			if err := tx.Create(&r).Error; err != nil {
				return err
			}
			// 菜单授权
			if r.Code == "admin" {
				var bizMenus []model.Menu
				if err := tx.Where("group_name = ?", "业务中心").Find(&bizMenus).Error; err != nil {
					return err
				}
				for _, m := range bizMenus {
					if err := tx.Create(&model.RoleMenu{RoleID: r.ID, MenuID: m.ID}).Error; err != nil {
						return err
					}
				}
			} else {
				perms := permission.StaffPerms()
				var menus []model.Menu
				if err := tx.Where("perm IN ? AND status = 1", perms).Find(&menus).Error; err != nil {
					return err
				}
				for _, m := range menus {
					if err := tx.Create(&model.RoleMenu{RoleID: r.ID, MenuID: m.ID}).Error; err != nil {
						return err
					}
				}
			}
		}
		if locked {
			if err := tx.Model(&model.TenantRow{}).Where("tenant_id = ?", tenantID).Update("rbac_seeded", true).Error; err != nil {
				return err
			}
		}
		invalidateRolePermsCache()
		return nil
	})
}

// ====================== 角色 ======================

// 成员权限内存缓存（用户维度）：map[uint64][]string（user_id → 权限码并集）。
// 失效点：成员角色变更（SetUserRoles）、角色权限变更/删除（CreateRole/UpdateRole/DeleteRole）、
// EnsureTenantRoles 初始化与去重后调用 invalidateRolePermsCache。
var rolePermsCache atomic.Value // map[uint64][]string

func loadRolePermsCache() map[uint64][]string {
	if v, ok := rolePermsCache.Load().(map[uint64][]string); ok {
		return v
	}
	return nil
}

func invalidateRolePermsCache() { rolePermsCache.Store(map[uint64][]string{}) }

// GetUserPerms 获取成员全部角色权限并集（带缓存）
// 推导链路：user_roles → sys_role_menu → menus.perm（status=1）
func GetUserPerms(userID uint64) []string {
	if cache := loadRolePermsCache(); cache != nil {
		if p, ok := cache[userID]; ok {
			return p
		}
	}
	set := map[string]bool{}
	var rels []model.UserRole
	if err := config.DB.Where("user_id = ?", userID).Find(&rels).Error; err == nil {
		roleIDs := make([]uint64, 0, len(rels))
		for _, rel := range rels {
			roleIDs = append(roleIDs, rel.RoleID)
		}
		var menuIDs []uint64
		if err := config.DB.Model(&model.RoleMenu{}).Where("role_id IN ?", roleIDs).Distinct("menu_id").Pluck("menu_id", &menuIDs).Error; err == nil {
			if perms, err := PermsOfMenuIDs(menuIDs); err == nil {
				for _, p := range perms {
					set[p] = true
				}
			}
		}
	}
	perms := make([]string, 0, len(set))
	for p := range set {
		perms = append(perms, p)
	}
	cache := loadRolePermsCache()
	if cache == nil {
		cache = map[uint64][]string{}
	}
	cache[userID] = perms
	rolePermsCache.Store(cache)
	return perms
}

// isFullPerms 权限集合是否覆盖全部权限码（管理员判定的唯一实现）
func isFullPerms(perms []string) bool {
	if len(perms) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, p := range perms {
		set[p] = true
	}
	for _, code := range permission.AllCodes() {
		if !set[code] {
			return false
		}
	}
	return true
}

// IsAdminUser 是否租户管理员：权限并集覆盖全部权限码即视为管理员。
// 与角色名/标记解耦（admin 角色可删可改）；权限集合是唯一身份来源，避免双源不一致
func IsAdminUser(userID uint64) bool {
	if userID == 0 {
		return true // app_key 管理员（无 user_id）全权，与 canHandleTicket 的 uid==0 放行一致
	}
	return isFullPerms(GetUserPerms(userID))
}

// IsAdminRole 角色是否管理员角色：关联菜单推导出的权限覆盖全部权限码即视为管理员角色。
// 与角色名解耦（admin 角色可删可改），权限集合是唯一身份来源
func IsAdminRole(r *model.Role) bool {
	perms, err := RoleMenuPerms(r.ID)
	if err != nil {
		return false
	}
	return isFullPerms(perms)
}

// ListRoles 租户角色列表（含菜单授权回显）
func ListRoles(tenantID string) ([]model.Role, error) {
	var roles []model.Role
	err := config.DB.Where("tenant_id = ?", tenantID).Order("id ASC").Find(&roles).Error
	for i := range roles {
		roles[i].MenuIDs, _ = RoleMenuIDs(roles[i].ID)
		roles[i].IsAdmin = IsAdminRole(&roles[i])
	}
	return roles, err
}

// GetRole 按 ID 查角色
func GetRole(id uint64) (*model.Role, error) {
	var r model.Role
	if err := config.DB.First(&r, id).Error; err != nil {
		return nil, err
	}
	r.MenuIDs, _ = RoleMenuIDs(r.ID)
	r.IsAdmin = IsAdminRole(&r)
	return &r, nil
}

// CreateRole 新建自定义角色（menuIDs 为菜单勾选集合）
func CreateRole(r *model.Role) error {
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(r).Error; err != nil {
			return err
		}
		if len(r.MenuIDs) == 0 {
			return nil
		}
		for _, id := range r.MenuIDs {
			if err := tx.Create(&model.RoleMenu{RoleID: r.ID, MenuID: id}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		invalidateRolePermsCache()
	}
	return err
}

// UpdateRole 更新角色名称/描述/菜单授权
func UpdateRole(id uint64, name, desc string, menuIDs []uint64) error {
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Role{}).Where("id = ?", id).
			Updates(map[string]interface{}{"name": name, "description": desc, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		seen := map[uint64]bool{}
		for _, mid := range menuIDs {
			if mid > 0 && !seen[mid] {
				seen[mid] = true
				if err := tx.Create(&model.RoleMenu{RoleID: id, MenuID: mid}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil {
		invalidateRolePermsCache()
	}
	return err
}

// DeleteRole 删除角色（预置角色同样可删）。若该角色仍有成员挂载，自动解除关联
// （成员保留但失去该角色权限；app_key 管理员不受影响）。
func DeleteRole(tenantID string, id uint64) error {
	var r model.Role
	if err := config.DB.First(&r, id).Error; err != nil {
		return err
	}
	if r.TenantID != tenantID {
		return errors.New("无权删除其他租户角色")
	}
	// 解除该角色的成员关联（保留成员）+ 菜单授权
	if err := config.DB.Where("role_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
		return err
	}
	if err := config.DB.Where("role_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
		return err
	}
	if err := config.DB.Delete(&model.Role{}, id).Error; err != nil {
		return err
	}
	invalidateRolePermsCache()
	return nil
}

// ====================== 用户（租户内成员） ======================

// RoleBrief 成员角色摘要
type RoleBrief struct {
	ID      uint64 `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	IsAdmin bool   `json:"is_admin"` // 计算字段：权限覆盖全量=管理员角色
}

// GetUserRoles 查询成员角色列表
func GetUserRoles(userID uint64) []RoleBrief {
	var rels []model.UserRole
	if err := config.DB.Where("user_id = ?", userID).Find(&rels).Error; err != nil {
		return nil
	}
	items := make([]RoleBrief, 0, len(rels))
	for _, rel := range rels {
		var r model.Role
		if err := config.DB.First(&r, rel.RoleID).Error; err != nil {
			continue
		}
		items = append(items, RoleBrief{ID: r.ID, Name: r.Name, Code: r.Code, IsAdmin: IsAdminRole(&r)})
	}
	return items
}

// SetUserRoles 覆盖式设置成员角色（删除旧关联 + 写入新关联）；权限缓存失效
func SetUserRoles(tenantID string, userID uint64, roleIDs []uint64) error {
	// 校验角色归属
	valid := map[uint64]bool{}
	var roles []model.Role
	if err := config.DB.Where("tenant_id = ?", tenantID).Find(&roles).Error; err != nil {
		return err
	}
	for _, r := range roles {
		valid[r.ID] = true
	}
	ids := make([]uint64, 0, len(roleIDs))
	for _, id := range roleIDs {
		if id > 0 && valid[id] {
			ids = append(ids, id)
		}
	}
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Create(&model.UserRole{UserID: userID, RoleID: id}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateUser 新建成员（同时写入成员-角色关联）
func CreateUser(u *model.User, roleIDs []uint64) error {
	now := time.Now()
	u.CreatedAt = now
	u.UpdatedAt = now
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		for _, id := range roleIDs {
			if id == 0 {
				continue
			}
			if err := tx.Create(&model.UserRole{UserID: u.ID, RoleID: id}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetUserByUsername 按用户名查（租户内唯一）
func GetUserByUsername(tenantID, username string) (*model.User, error) {
	var u model.User
	err := config.DB.Where("tenant_id = ? AND username = ?", tenantID, username).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByID 按 ID 查
func GetUserByID(id uint64) (*model.User, error) {
	var u model.User
	if err := config.DB.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// UserRow 成员列表条目（Roles 为多角色摘要）
type UserRow struct {
	model.User
	Roles []RoleBrief `json:"roles"`
}

// ListUsers 租户成员列表（支持用户名模糊 / 角色包含 / 状态筛选）
func ListUsers(tenantID, q string, roleID uint64, status int) ([]UserRow, error) {
	var users []model.User
	where := "tenant_id = ?"
	args := []interface{}{tenantID}
	if q != "" {
		where += " AND username LIKE ?"
		args = append(args, "%"+q+"%")
	}
	if roleID > 0 {
		where += " AND id IN (SELECT user_id FROM user_roles WHERE role_id = ?)"
		args = append(args, roleID)
	}
	if status >= 0 {
		where += " AND status = ?"
		args = append(args, status)
	}
	if err := config.DB.Where(where, args...).Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	rows := make([]UserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, UserRow{User: u, Roles: GetUserRoles(u.ID)})
	}
	return rows, nil
}

// UpdateUserStatus 启用/禁用成员
func UpdateUserStatus(tenantID string, id uint64, status int) error {
	return config.DB.Model(&model.User{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_at": time.Now()}).Error
}

// UpdateUserPassword 重置密码
func UpdateUserPassword(tenantID string, id uint64, hash string) error {
	return config.DB.Model(&model.User{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"password_hash": hash, "updated_at": time.Now()}).Error
}

// UpdateUsername 修改成员用户名（租户内唯一性由调用方校验）
func UpdateUsername(tenantID string, id uint64, username string) error {
	return config.DB.Model(&model.User{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"username": username, "updated_at": time.Now()}).Error
}

// UpdateUserFields 通用更新成员资料字段（nickname/phone/email/remark/username 等）
func UpdateUserFields(tenantID string, id uint64, fields map[string]interface{}) error {
	fields["updated_at"] = time.Now()
	return config.DB.Model(&model.User{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

// UpdateUserLastLogin 记录最近登录
func UpdateUserLastLogin(id uint64) error {
	now := time.Now()
	return config.DB.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]interface{}{"last_login_at": now, "updated_at": now}).Error
}

// DeleteUser 删除成员
// DeleteUser 删除成员（软删除；同步清理成员-角色关联）
func DeleteUser(tenantID string, id uint64) error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&model.User{}).Error; err != nil {
			return err
		}
		invalidateRolePermsCache()
		return nil
	})
}
