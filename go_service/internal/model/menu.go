package model

import (
	"time"
)

// Menu 菜单权限节点（RuoYi 模式：菜单=权限载体）
// type: 1=目录 2=页面 3=按钮/操作；perm 为权限码，全局唯一（系统管理组 perm 为 NULL）。
type Menu struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	ParentID  uint64    `gorm:"column:parent_id;index" json:"parent_id"`
	GroupName string    `gorm:"column:group_name;size:32" json:"group_name"` // 侧边栏分组：业务中心/系统管理
	Name      string    `gorm:"column:name;size:64" json:"name"`
	Type      int       `gorm:"column:type" json:"type"`
	Path      string    `gorm:"column:path;size:128" json:"path"`
	Component string    `gorm:"column:component;size:128" json:"component"`
	Perm      *string   `gorm:"column:perm;size:64;uniqueIndex" json:"perm"`
	Icon      string    `gorm:"column:icon;size:64" json:"icon"`
	Sort      int       `gorm:"column:sort" json:"sort"`
	Visible   int       `gorm:"column:visible" json:"visible"` // 1显示 0隐藏
	Status    int       `gorm:"column:status" json:"status"`   // 1启用 0停用
	IsBuiltin int       `gorm:"column:is_builtin" json:"is_builtin"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// RoleMenu 角色-菜单关联
type RoleMenu struct {
	RoleID uint64 `gorm:"primaryKey;column:role_id" json:"role_id"`
	MenuID uint64 `gorm:"primaryKey;column:menu_id" json:"menu_id"`
}

func (RoleMenu) TableName() string { return "sys_role_menu" }
