package repository

import (
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"

	"customer_service/internal/config"
	"customer_service/internal/model"
	"customer_service/internal/pkg/logger"
	"customer_service/internal/pkg/permission"
)

// ====================== 内置菜单种子（与 deploy/init/mysql/init.sql 保持一致） ======================

// builtinMenus 内置菜单初始数据（is_builtin=1）
// 分类体系（方案A）：group（业务中心/系统管理，平台/租户可见层级）→ 功能域目录(type=1) → 页面(type=2) → 按钮(type=3)
var builtinMenus = []struct {
	id, parent, sort, visible, status, builtin int
	group, name, path, component, perm         string
	typ                                         int
}{
	// ===== 业务中心（租户侧）：按功能域分 7 个目录 =====
	// 会话管理（运营看板作为租户登录首页，置于首位）
	{1, 0, 1, 1, 1, 1, "业务中心", "会话管理", "", "", "", 1},
	{2, 1, 1, 1, 1, 1, "业务中心", "运营看板", "/ops", "views/Ops.vue", "dashboard:view", 2},
	{10, 1, 2, 1, 1, 1, "业务中心", "会话列表", "/sessions", "views/Sessions.vue", "session:view", 2},
	{11, 10, 1, 1, 1, 1, "业务中心", "编辑客户信息", "", "", "session:profile:edit", 3},
	{12, 10, 2, 1, 1, 1, "业务中心", "清理归档", "", "", "session:cleanup", 3},
	{13, 1, 3, 1, 1, 1, "业务中心", "客户列表", "/customers", "views/Customers.vue", "customer:view", 2},
	// 工单管理
	{40, 0, 2, 1, 1, 1, "业务中心", "工单管理", "", "", "", 1},
	{3, 40, 1, 1, 1, 1, "业务中心", "工单列表", "/tickets", "views/Tickets.vue", "ticket:view", 2},
	{4, 3, 1, 1, 1, 1, "业务中心", "处理工单", "", "", "ticket:handle", 3},
	{5, 1, 4, 1, 1, 1, "业务中心", "快捷回复", "/quick-replies", "views/QuickReplies.vue", "quickreply:view", 2},
	{6, 5, 1, 1, 1, 1, "业务中心", "新增快捷回复", "", "", "quickreply:add", 3},
	{7, 5, 2, 1, 1, 1, "业务中心", "修改快捷回复", "", "", "quickreply:edit", 3},
	{8, 5, 3, 1, 1, 1, "业务中心", "删除快捷回复", "", "", "quickreply:remove", 3},
	{9, 1, 5, 1, 1, 1, "业务中心", "满意度评价", "/ratings", "views/Ratings.vue", "rating:view", 2},
	// 知识库管理（知识库文档 RAG）
	{42, 0, 3, 1, 1, 1, "业务中心", "知识库管理", "", "", "", 1},
	{18, 42, 1, 1, 1, 1, "业务中心", "知识库", "/knowledge", "views/Knowledge.vue", "knowledge:view", 2},
	{19, 18, 1, 1, 1, 1, "业务中心", "新增文档", "", "", "knowledge:add", 3},
	{20, 18, 2, 1, 1, 1, "业务中心", "修改文档", "", "", "knowledge:edit", 3},
	{21, 18, 3, 1, 1, 1, "业务中心", "删除文档", "", "", "knowledge:remove", 3},
	// 模型管理（模型配置 + Prompt 管理：Prompt 为模型行为配置，归模型域）
	{43, 0, 4, 1, 1, 1, "业务中心", "模型管理", "", "", "", 1},
	{22, 43, 1, 1, 1, 1, "业务中心", "模型配置", "/model-config", "views/TenantModelConfig.vue", "model:view", 2},
	{23, 22, 1, 1, 1, 1, "业务中心", "配置模型", "", "", "model:edit", 3},
	{14, 43, 2, 1, 1, 1, "业务中心", "Prompt 管理", "/prompts", "views/Prompt.vue", "prompt:view", 2},
	{15, 14, 1, 1, 1, 1, "业务中心", "新增 Prompt", "", "", "prompt:add", 3},
	{16, 14, 2, 1, 1, 1, "业务中心", "修改 Prompt", "", "", "prompt:edit", 3},
	{17, 14, 3, 1, 1, 1, "业务中心", "删除 Prompt", "", "", "prompt:remove", 3},
	// 统计管理（用量/审计）
	{44, 0, 5, 1, 1, 1, "业务中心", "统计管理", "", "", "", 1},
	{24, 44, 1, 1, 1, 1, "业务中心", "用量统计", "/token-usage", "views/TokenUsage.vue", "usage:view", 2},
	{25, 44, 2, 1, 1, 1, "业务中心", "审计日志", "/audit-logs", "views/AuditLogs.vue", "audit:view", 2},
	// 用户管理（用户与角色）
	{45, 0, 6, 1, 1, 1, "业务中心", "用户管理", "", "", "", 1},
	{26, 45, 1, 1, 1, 1, "业务中心", "用户列表", "/members", "views/Users.vue", "member:view", 2},
	{27, 26, 1, 1, 1, 1, "业务中心", "新增用户", "", "", "member:add", 3},
	{28, 26, 2, 1, 1, 1, "业务中心", "修改用户", "", "", "member:edit", 3},
	{29, 26, 3, 1, 1, 1, "业务中心", "删除用户", "", "", "member:remove", 3},
	{30, 45, 2, 1, 1, 1, "业务中心", "角色权限", "/roles", "views/Roles.vue", "role:view", 2},
	{31, 30, 1, 1, 1, 1, "业务中心", "新增角色", "", "", "role:add", 3},
	{32, 30, 2, 1, 1, 1, "业务中心", "修改角色", "", "", "role:edit", 3},
	{33, 30, 3, 1, 1, 1, "业务中心", "删除角色", "", "", "role:remove", 3},

	// ===== 系统管理（平台侧）：租户管理 / 模型管理 / 统计管理 / 平台配置，perm 为 NULL（平台角色体系未启用） =====
	{110, 0, 1, 1, 1, 1, "系统管理", "租户管理", "", "", "", 1},
	{100, 110, 1, 1, 1, 1, "系统管理", "租户列表", "/tenants", "views/Tenants.vue", "", 2},
	{113, 0, 2, 1, 1, 1, "系统管理", "模型管理", "", "", "", 1},
	{101, 113, 1, 1, 1, 1, "系统管理", "模型提供商", "/model-providers", "views/ModelProviders.vue", "", 2},
	{102, 113, 2, 1, 1, 1, "系统管理", "平台模型配置", "/platform-model", "views/PlatformModelConfig.vue", "", 2},
	{103, 113, 3, 1, 1, 1, "系统管理", "Prompt 管理", "/prompts", "views/Prompt.vue", "", 2},
	{111, 0, 3, 1, 1, 1, "系统管理", "统计管理", "", "", "", 1},
	{104, 111, 1, 1, 1, 1, "系统管理", "用量统计", "/token-usage", "views/TokenUsage.vue", "", 2},
	{105, 111, 2, 1, 1, 1, "系统管理", "审计日志", "/audit-logs", "views/AuditLogs.vue", "", 2},
	{112, 0, 4, 1, 1, 1, "系统管理", "平台配置", "", "", "", 1},
	{106, 112, 1, 1, 1, 1, "系统管理", "平台设置", "/platform-settings", "views/PlatformSettings.vue", "", 2},
	{107, 112, 2, 1, 1, 1, "系统管理", "菜单权限", "/menus", "views/MenuPermissions.vue", "", 2},
}

// SeedBuiltinMenus 幂等种入内置菜单（已存在 is_builtin=1 节点则不重复插入）
func SeedBuiltinMenus() error {
	var cnt int64
	if err := config.DB.Model(&model.Menu{}).Where("is_builtin = 1").Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	now := time.Now()
	menus := make([]model.Menu, 0, len(builtinMenus))
	for _, m := range builtinMenus {
		var perm *string
		if m.perm != "" {
			p := m.perm
			perm = &p
		}
		menus = append(menus, model.Menu{
			ID: uint64(m.id), ParentID: uint64(m.parent), GroupName: m.group, Name: m.name,
			Type: m.typ, Path: m.path, Component: m.component, Perm: perm,
			Sort: m.sort, Visible: m.visible, Status: m.status, IsBuiltin: m.builtin,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	return config.DB.Create(&menus).Error
}

// ResetBuiltinMenus 重置内置菜单：删除现有 is_builtin=1 节点（含角色关联），重新种入初始状态。
// 不动自定义菜单（is_builtin=0）与角色关联中指向自定义节点的部分。
func ResetBuiltinMenus() error {
	var ids []uint64
	if err := config.DB.Model(&model.Menu{}).Where("is_builtin = 1").Pluck("id", &ids).Error; err != nil {
		return err
	}
	return config.DB.Transaction(func(tx *gorm.DB) error {
		if len(ids) > 0 {
			if err := tx.Where("menu_id IN ?", ids).Delete(&model.RoleMenu{}).Error; err != nil {
				return err
			}
			if err := tx.Where("is_builtin = 1").Delete(&model.Menu{}).Error; err != nil {
				return err
			}
		}
		now := time.Now()
		menus := make([]model.Menu, 0, len(builtinMenus))
		for _, m := range builtinMenus {
			var perm *string
			if m.perm != "" {
				p := m.perm
				perm = &p
			}
			menus = append(menus, model.Menu{
				ID: uint64(m.id), ParentID: uint64(m.parent), GroupName: m.group, Name: m.name,
				Type: m.typ, Path: m.path, Component: m.component, Perm: perm,
				Sort: m.sort, Visible: m.visible, Status: m.status, IsBuiltin: m.builtin,
				CreatedAt: now, UpdatedAt: now,
			})
		}
		if err := tx.Create(&menus).Error; err != nil {
			return err
		}
		// 重置后为各租户 admin 角色补全业务中心授权（内置菜单被删后角色关联一并清空，需恢复管理员全权）
		adminRoleIDs := []uint64{}
		tx.Model(&model.Role{}).Where("code = ?", "admin").Pluck("id", &adminRoleIDs)
		if len(adminRoleIDs) > 0 {
			var bizIDs []uint64
			tx.Model(&model.Menu{}).Where("group_name = ?", "业务中心").Pluck("id", &bizIDs)
			for _, rid := range adminRoleIDs {
				for _, mid := range bizIDs {
					if err := tx.Create(&model.RoleMenu{RoleID: rid, MenuID: mid}).Error; err != nil {
						return err
					}
				}
			}
		}
		invalidateRolePermsCache()
		invalidateMenuCache()
		return nil
	})
}

// ====================== 菜单 CRUD ======================

// ===== 菜单缓存（读多写少，写时失效） =====
// 菜单表仅在管理后台修改，读取（登录/刷新/菜单树）频率远高于写入：
// 缓存全量列表与启用列表两份数据，任何写路径（增删改/批量/重置）统一 invalidateMenuCache() 失效，
// 前端无需退出重新登录，刷新即拿到最新菜单，且对 DB 零重复压力。
var (
	menuCacheMu   sync.Mutex
	menuListCache []model.Menu // 管理页全量（含停用/隐藏）
	menuActiveCache []model.Menu // 启用状态全量（菜单树数据源）
	menuCacheLoaded bool
)

func invalidateMenuCache() {
	menuCacheMu.Lock()
	defer menuCacheMu.Unlock()
	menuListCache = nil
	menuActiveCache = nil
	menuCacheLoaded = false
}

// cachedMenus 返回缓存的全量菜单与启用菜单（懒加载，线程安全）
func cachedMenus() (list, active []model.Menu, err error) {
	menuCacheMu.Lock()
	defer menuCacheMu.Unlock()
	if menuCacheLoaded {
		return menuListCache, menuActiveCache, nil
	}
	var ms []model.Menu
	if err = config.DB.Order("group_name ASC, sort ASC, id ASC").Find(&ms).Error; err != nil {
		return nil, nil, err
	}
	act := make([]model.Menu, 0, len(ms))
	for _, m := range ms {
		if m.Status == 1 {
			act = append(act, m)
		}
	}
	menuListCache = ms
	menuActiveCache = act
	menuCacheLoaded = true
	return ms, act, nil
}

// ListMenus 全量菜单（管理页，走缓存）
func ListMenus() ([]model.Menu, error) {
	ms, _, err := cachedMenus()
	return ms, err
}

// GetMenu 按 ID 查菜单
func GetMenu(id uint64) (*model.Menu, error) {
	var m model.Menu
	if err := config.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateMenu 新增菜单节点
func CreateMenu(m *model.Menu) error {
	now := time.Now()
	m.CreatedAt = now
	m.UpdatedAt = now
	if err := config.DB.Create(m).Error; err != nil {
		return err
	}
	invalidateRolePermsCache()
	invalidateMenuCache()
	return nil
}

// UpdateMenu 更新菜单节点
func UpdateMenu(id uint64, fields map[string]interface{}) error {
	fields["updated_at"] = time.Now()
	if err := config.DB.Model(&model.Menu{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		return err
	}
	invalidateRolePermsCache()
	invalidateMenuCache()
	return nil
}

// BatchUpdateMenus 批量更新菜单（status/visible/sort 等），用于菜单管理页批量操作
func BatchUpdateMenus(ids []uint64, fields map[string]interface{}) error {
	if len(ids) == 0 || len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now()
	if err := config.DB.Model(&model.Menu{}).Where("id IN ?", ids).Updates(fields).Error; err != nil {
		return err
	}
	invalidateRolePermsCache()
	invalidateMenuCache()
	return nil
}

// DeleteMenu 删除菜单节点（级联子节点 + 角色关联）。返回被删节点 perm 集合（前端提示引用）。
func DeleteMenu(id uint64) ([]string, error) {
	var delIDs []uint64
	collectChildren(&delIDs, id)
	var removed []model.Menu
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", delIDs).Find(&removed).Error; err != nil {
			return err
		}
		if err := tx.Where("menu_id IN ?", delIDs).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id IN ?", delIDs).Delete(&model.Menu{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	perms := make([]string, 0, len(removed))
	for _, m := range removed {
		if m.Perm != nil && *m.Perm != "" {
			perms = append(perms, *m.Perm)
		}
	}
	invalidateRolePermsCache()
	invalidateMenuCache()
	return perms, nil
}

// collectChildren 收集节点及其全部子孙 ID
func collectChildren(ids *[]uint64, parent uint64) {
	var children []model.Menu
	config.DB.Where("parent_id = ?", parent).Find(&children)
	for _, c := range children {
		*ids = append(*ids, c.ID)
		collectChildren(ids, c.ID)
	}
	*ids = append(*ids, parent)
}

// ====================== 角色-菜单关联 ======================

// RoleMenuIDs 角色关联的菜单 ID 集合
func RoleMenuIDs(roleID uint64) ([]uint64, error) {
	var ids []uint64
	err := config.DB.Model(&model.RoleMenu{}).Where("role_id = ?", roleID).Pluck("menu_id", &ids).Error
	return ids, err
}

// SetRoleMenus 覆盖式设置角色菜单关联；权限缓存失效
func SetRoleMenus(roleID uint64, menuIDs []uint64) error {
	ids := make([]uint64, 0, len(menuIDs))
	seen := map[uint64]bool{}
	for _, id := range menuIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Create(&model.RoleMenu{RoleID: roleID, MenuID: id}).Error; err != nil {
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

// PermsOfMenuIDs 由菜单 ID 集合推导权限码（status=1 且 perm 非空）
func PermsOfMenuIDs(menuIDs []uint64) ([]string, error) {
	if len(menuIDs) == 0 {
		return nil, nil
	}
	var menus []model.Menu
	if err := config.DB.Where("id IN ? AND status = 1 AND perm IS NOT NULL AND perm != ''", menuIDs).Find(&menus).Error; err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, m := range menus {
		if m.Perm != nil {
			set[*m.Perm] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	return out, nil
}

// RoleMenuPerms 角色关联的权限码集合
func RoleMenuPerms(roleID uint64) ([]string, error) {
	ids, err := RoleMenuIDs(roleID)
	if err != nil {
		return nil, err
	}
	return PermsOfMenuIDs(ids)
}

// ====================== 菜单树（前端渲染） ======================

// MenuNode 菜单树节点
type MenuNode struct {
	model.Menu
	Children []*MenuNode `json:"children,omitempty"`
}

// MenuGroup 侧边栏分组：title=组名，children=顶层节点树
type MenuGroup struct {
	Title    string      `json:"title"`
	Group    string      `json:"group"`
	Children []*MenuNode `json:"children"`
}

// 菜单树范围（按登录身份隔离可见菜单）
const (
	MenuScopeAll        = "all"          // 演示模式（未开启鉴权，无租户上下文）：返回全部菜单
	MenuScopePlatform   = "platform"     // 平台管理员（app_key 平台租户）：仅系统管理组（平台级菜单）
	MenuScopeTenantAll  = "tenant-admin" // 租户管理员（app_key 普通租户）：业务中心组全部节点（隐式全权）
	MenuScopeTenantUser = "tenant-user"  // 租户子账号：业务中心组按权限码过滤
)

// MenuTreeForUser 构造当前用户可访问的菜单树。
//   - MenuScopePlatform：仅返回「系统管理」组（平台级菜单），不做权限过滤
//   - MenuScopeTenantAll：返回「业务中心」组全部节点（租户管理员隐式全权）
//   - MenuScopeTenantUser：返回「业务中心」组中权限码命中的节点（status=1），并自动保留父链
//   - MenuScopeAll：返回全部节点（演示模式无租户上下文，兼容旧行为）
func MenuTreeForUser(perms []string, scope string) []MenuGroup {
	_, act, err := cachedMenus()
	if err != nil {
		return nil
	}
	var all []model.Menu
	switch scope {
	case MenuScopePlatform:
		for _, m := range act {
			if m.GroupName == "系统管理" {
				all = append(all, m)
			}
		}
	case MenuScopeTenantAll, MenuScopeTenantUser:
		for _, m := range act {
			if m.GroupName == "业务中心" {
				all = append(all, m)
			}
		}
	default: // MenuScopeAll
		all = act
	}
	if len(all) == 0 {
		return nil
	}
	// 子账号：构造可见集合（含父链）；管理员 / 平台 / 演示模式全可见
	visible := map[uint64]bool{}
	if scope == MenuScopeTenantUser {
		permSet := map[string]bool{}
		for _, p := range perms {
			permSet[p] = true
		}
		byID := map[uint64]model.Menu{}
		for _, m := range all {
			byID[m.ID] = m
		}
		var mark func(id uint64)
		mark = func(id uint64) {
			if visible[id] {
				return
			}
			visible[id] = true
			if p := byID[id].ParentID; p > 0 {
				mark(p)
			}
		}
		for _, m := range all {
			if m.Status == 1 && m.Type != 1 && m.Perm != nil && permSet[*m.Perm] {
				mark(m.ID)
			}
		}
	}
	// 分组
	groupOrder := []string{}
	groupIdx := map[string]int{}
	for _, m := range all {
		if scope != MenuScopeTenantUser || visible[m.ID] {
			if _, ok := groupIdx[m.GroupName]; !ok {
				groupIdx[m.GroupName] = len(groupOrder)
				groupOrder = append(groupOrder, m.GroupName)
			}
		}
	}
	groups := make([]MenuGroup, 0, len(groupOrder))
	nodesByGroup := map[string][]*MenuNode{}
	for _, g := range groupOrder {
		groups = append(groups, MenuGroup{Title: g, Group: g})
		nodesByGroup[g] = []*MenuNode{}
	}
	for i := range all {
		m := all[i]
		if scope != MenuScopeTenantUser || visible[m.ID] {
			n := &MenuNode{Menu: m}
			nodesByGroup[m.GroupName] = append(nodesByGroup[m.GroupName], n)
		}
	}
	// 组装父子
	for g := range nodesByGroup {
		byID := map[uint64]*MenuNode{}
		for _, n := range nodesByGroup[g] {
			byID[n.ID] = n
		}
		var roots []*MenuNode
		for _, n := range nodesByGroup[g] {
			if n.ParentID > 0 {
				if p, ok := byID[n.ParentID]; ok {
					p.Children = append(p.Children, n)
					continue
				}
			}
			roots = append(roots, n)
		}
		for gi := range groups {
			if groups[gi].Group == g {
				groups[gi].Children = roots
			}
		}
	}
	return groups
}

// HasPerm 权限码校验（供 handler 快速判断）
func HasPerm(perms []string, perm string) bool {
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// EnsureRoleHasAdminMenus 确保 admin 角色关联业务中心组全部节点（创建/重置角色时调用）
func EnsureRoleHasAdminMenus(roleID uint64) error {
	var bizIDs []uint64
	if err := config.DB.Model(&model.Menu{}).Where("group_name = ?", "业务中心").Pluck("id", &bizIDs).Error; err != nil {
		return err
	}
	return SetRoleMenus(roleID, bizIDs)
}

// RoleNameByID 查角色名（审计/提示用）
func RoleNameByID(id uint64) string {
	var r model.Role
	if err := config.DB.First(&r, id).Error; err != nil {
		return ""
	}
	return r.Name
}

// ErrMenuNotFound 菜单不存在
var ErrMenuNotFound = errors.New("菜单不存在")

// CheckMenuPermsConsistency 启动校验：menus 表 perm 集合 与 代码权限目录 Catalog 互查缺失（告警，不阻塞）。
// 目的：权限码被代码 RequirePerm 引用但菜单表缺失（无法授权给任何角色）时在日志现形。
func CheckMenuPermsConsistency() {
	var dbPerms []string
	config.DB.Model(&model.Menu{}).Where("perm IS NOT NULL AND perm != ''").Distinct("perm").Pluck("perm", &dbPerms)
	dbSet := map[string]bool{}
	for _, p := range dbPerms {
		dbSet[p] = true
	}
	missingInDB := []string{}
	for _, it := range permission.Catalog {
		if !dbSet[it.Code] {
			missingInDB = append(missingInDB, it.Code)
		}
	}
	codeSet := map[string]bool{}
	for _, it := range permission.Catalog {
		codeSet[it.Code] = true
	}
	orphanInDB := []string{}
	for _, p := range dbPerms {
		if !codeSet[p] {
			orphanInDB = append(orphanInDB, p)
		}
	}
	if len(missingInDB) > 0 {
		logger.Warn("权限码一致性：menus 表缺少以下代码引用权限码（这些接口将无法授权）", logger.Fields{"codes": missingInDB})
	}
	if len(orphanInDB) > 0 {
		logger.Warn("权限码一致性：menus 表存在代码目录未定义的权限码（孤儿权限）", logger.Fields{"codes": orphanInDB})
	}
}
