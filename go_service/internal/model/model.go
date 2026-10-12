package model

import (
	"time"

	"gorm.io/plugin/soft_delete"
)

type Order struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	OrderID    string    `gorm:"column:order_id;size:64;unique" json:"order_id"`
	UserName   string    `gorm:"column:user_name;size:128" json:"user_name"`
	Item       string    `gorm:"column:item;size:255" json:"item"`
	Amount     float64   `gorm:"column:amount;type:decimal(12,2)" json:"amount"`
	Status     string    `gorm:"column:status;size:32" json:"status"`
	CreateDate string    `gorm:"column:create_date;size:32" json:"create_date"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
}

type Ticket struct {
	ID          uint64         `gorm:"primaryKey" json:"id"`
	ThreadID    string         `gorm:"column:thread_id;size:255;index:idx_ticket_thread_status,priority:1" json:"thread_id"`
	TicketNo    string         `gorm:"column:ticket_no;size:64;unique" json:"ticket_no"`
	UserID      string         `gorm:"column:user_id;size:255" json:"user_id"`
	Reason      string         `gorm:"column:reason;type:text" json:"reason"`
	Status      int            `gorm:"column:status;type:tinyint(1);index:idx_ticket_thread_status,priority:2" json:"status"` //0待处理 1已转接/人工接管中 2已取消 3已关闭
	Priority    int                  `gorm:"column:priority;type:tinyint(1);default:1" json:"priority"` // 0=低，1=中，2=高，3=紧急
	TenantID    string               `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	HandlerUID  uint64               `gorm:"column:handler_uid;default:0" json:"handler_uid"`   // 接管客服子账号ID（0=app_key管理员）
	HandlerName string               `gorm:"column:handler_name;size:64" json:"handler_name"`   // 接管客服名（冗余快照，改名不影响历史工单）
	ClosedAt    *time.Time           `gorm:"column:closed_at" json:"closed_at,omitempty"`       // 关闭/取消时刻：工单详情消息窗口上界
	CreatedAt   time.Time      `gorm:"column:created_at" json:"created_at"`
	UnreadCount int64          `gorm:"-" json:"unread_count"` // 未读消息数，不映射到数据库
	DeletedAt   soft_delete.DeletedAt `gorm:"column:deleted_at;index" json:"-"` // 软删除
}

// TicketLog 工单操作历史
type TicketLog struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	TicketID    uint64    `gorm:"column:ticket_id;index" json:"ticket_id"`
	ThreadID    string    `gorm:"column:thread_id;size:255" json:"thread_id"`
	TenantID    string    `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	Action      string    `gorm:"column:action;size:32" json:"action"`
	OldStatus   int       `gorm:"column:old_status;type:tinyint(1)" json:"old_status"`
	NewStatus   int       `gorm:"column:new_status;type:tinyint(1)" json:"new_status"`
	OldPriority int       `gorm:"column:old_priority;type:tinyint(1)" json:"old_priority"`
	NewPriority int       `gorm:"column:new_priority;type:tinyint(1)" json:"new_priority"`
	Operator    string    `gorm:"column:operator;size:64" json:"operator"`
	Remark      string    `gorm:"column:remark;size:500" json:"remark"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
}

// 工单状态常量
const (
	TicketStatusPending  = 0 // 待处理（等待人工确认）
	TicketStatusHandling = 1 // 已转接 / 人工接管中
	TicketStatusCanceled = 2 // 已取消
	TicketStatusClosed   = 3 // 已关闭（人工会话结束）
)

// Message 会话消息（用户/机器人/人工客服/系统）
// TicketID 归属工单：人工会话（转接诉求、接管中对话）归属对应工单；
// 待处理期间（未接管）AI 对话不归属任何工单（NULL），实现工单会话隔离
type Message struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	ThreadID  string    `gorm:"column:thread_id;size:255;index" json:"thread_id"`
	Role      string    `gorm:"column:role;size:16" json:"role"` // user / ai / human / system
	Content   string    `gorm:"column:content;type:text" json:"content"`
	TenantID  string    `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	TicketID  *uint64   `gorm:"column:ticket_id;index" json:"ticket_id,omitempty"` // 归属工单ID，NULL=不归属
	IsRead    int       `gorm:"column:is_read;type:tinyint(1);default:0" json:"is_read"` // 0=未读，1=已读
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

// ArchivedMessage 归档消息表（P1-4：messages 超保留期后按月归档，保证业务表查询性能）
type ArchivedMessage struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	ThreadID   string    `gorm:"column:thread_id;size:255;index" json:"thread_id"`
	Role       string    `gorm:"column:role;size:16" json:"role"`
	Content    string    `gorm:"column:content;type:text" json:"content"`
	TenantID   string    `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	TicketID   *uint64   `gorm:"column:ticket_id;index" json:"ticket_id,omitempty"`
	IsRead     int       `gorm:"column:is_read;type:tinyint(1);default:0" json:"is_read"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
	ArchivedAt time.Time `gorm:"column:archived_at" json:"archived_at"`
}

func (ArchivedMessage) TableName() string { return "messages_archive" }

// MessageWithArchive 会话详情消息（messages 与 messages_archive 合并视图，后台会话记录用）
type MessageWithArchive struct {
	ID        uint64    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	TenantID  string    `json:"tenant_id"`
	TicketID  *uint64   `json:"ticket_id,omitempty"`
	IsRead    int       `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
	Archived  bool      `json:"archived"` // true = 已归档（来自 messages_archive）
}

// Tenant 租户配置（config.yaml 种子；运行时以 DB tenants 表为准）
type Tenant struct {
	TenantID        string `yaml:"tenant_id"`
	Name            string `yaml:"name"`
	AppKey          string `yaml:"app_key"`
	AppSecret       string `yaml:"app_secret"`
	IsPlatformAdmin bool   `yaml:"is_platform_admin"`
}

// TenantRow tenants 表模型（显式表名，避免 gorm 复数化）
type TenantRow struct {
	ID              uint64    `gorm:"primaryKey" json:"id"`
	TenantID        string    `gorm:"column:tenant_id;size:64;unique" json:"tenant_id"`
	Name            string    `gorm:"column:name;size:128" json:"name"`
	AppKey          string    `gorm:"column:app_key;size:64;unique" json:"app_key"`
	AppSecretHash   string    `gorm:"column:app_secret_hash;size:255" json:"-"`
	IsPlatformAdmin bool      `gorm:"column:is_platform_admin" json:"is_platform_admin"`
	Status          int            `gorm:"column:status;type:tinyint(1)" json:"status"` // 1正常 0禁用
	// RBAC 预置角色初始化标记：0=从未初始化（首次访问种 admin/staff），1=已初始化（即使角色被删光也不补种）
	RbacSeeded      bool           `gorm:"column:rbac_seeded;type:tinyint(1);default:0" json:"-"`
	CreatedAt       time.Time      `gorm:"column:created_at" json:"created_at"`
	DeletedAt       soft_delete.DeletedAt `gorm:"column:deleted_at;index" json:"-"` // 软删除
	// 模型配置（BYOK）
	ModelProvider string `gorm:"column:model_provider;size:32" json:"model_provider"` // platform=平台默认, custom=租户自定义
	ModelProviderKey string `gorm:"column:model_provider_key;size:64" json:"model_provider_key"` // 模型提供商标识，如 deepseek/qwen
	// 模型提供商 ID（关联 model_providers.id，普通字段不建外键）：
	// llm_provider_id>0 → 使用该提供商的 default_model/default_api_base，API Key 取本行 model_api_key
	// =0/null → 兼容旧数据：走下方手填字段
	LlmProviderID      uint64 `gorm:"column:llm_provider_id" json:"llm_provider_id"`
	ModelName     string `gorm:"column:model_name;size:128" json:"model_name"`         // 模型名称，为空用平台默认
	ModelAPIKey   string `gorm:"column:model_api_key;size:512" json:"-"`               // AES加密后的API Key（租户自身密钥），不返显
	ModelAPIBase  string `gorm:"column:model_api_base;size:256" json:"model_api_base"` // 自定义API Base URL
	// 模型单价（元/百万 tokens，0=未定价）：chat 输入分「命中缓存/未命中」两档，输出统一；embedding 统一
	// 存租户行：平台默认模型单价=t_admin 行，租户自定义=租户行自身
	ChatInputCachePrice float64 `gorm:"column:chat_input_cache_price;type:decimal(10,4);default:0" json:"chat_input_cache_price"`
	ChatInputPrice      float64 `gorm:"column:chat_input_price;type:decimal(10,4);default:0" json:"chat_input_price"`
	ChatOutputPrice     float64 `gorm:"column:chat_output_price;type:decimal(10,4);default:0" json:"chat_output_price"`
	EmbeddingPrice      float64 `gorm:"column:embedding_price;type:decimal(10,4);default:0" json:"embedding_price"`
	// Embedding 模型配置（BYOK，用于知识库向量化）
	EmbeddingProvider   string `gorm:"column:embedding_provider;size:32" json:"embedding_provider"`     // platform=平台默认, custom=租户自定义
	EmbeddingProviderKey string `gorm:"column:embedding_provider_key;size:64" json:"embedding_provider_key"` // embedding 提供商标识
	EmbeddingProviderID uint64 `gorm:"column:embedding_provider_id" json:"embedding_provider_id"`
	EmbeddingModelName  string `gorm:"column:embedding_model_name;size:128" json:"embedding_model_name"` // embedding模型名称
	EmbeddingAPIKey     string `gorm:"column:embedding_api_key;size:512" json:"-"`                       // AES加密后的embedding API Key
	EmbeddingAPIBase    string `gorm:"column:embedding_api_base;size:256" json:"embedding_api_base"`     // embedding API Base URL

	// 配额（P1-2）：0 表示不限制
	MaxConcurrentSessions int `gorm:"column:max_concurrent_sessions;default:100" json:"max_concurrent_sessions"`
	DailyMessageLimit     int `gorm:"column:daily_message_limit;default:1000" json:"daily_message_limit"`
	MaxKbDocs             int `gorm:"column:max_kb_docs;default:100" json:"max_kb_docs"`
	MaxKbSizeMB           int `gorm:"column:max_kb_size_mb;default:100" json:"max_kb_size_mb"`
}

func (TenantRow) TableName() string { return "tenants" }

// PlatformConfig 平台级配置（key-value 结构）
type PlatformConfig struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	ConfigKey   string    `gorm:"column:config_key;size:64;unique" json:"config_key"`
	ConfigValue string    `gorm:"column:config_value;size:512" json:"config_value"`
	Description string    `gorm:"column:description;size:255" json:"description"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (PlatformConfig) TableName() string { return "platform_config" }

// ExtractRule 客户信息提取规则（平台级配置，存 platform_config key=cs_extract_rule_{field}）
type ExtractRule struct {
	Field   string `json:"field"`   // phone / email（固定字段，规则可自定义正则）
	Pattern string `json:"pattern"` // 正则表达式
}

// CustomerProfile 客户信息（会话维度：正则自动提取 phone/email + 后台人工补全 name/address/remark）
type CustomerProfile struct {
	ID            uint64         `gorm:"primaryKey" json:"id"`
	ThreadID      string         `gorm:"column:thread_id;size:255;unique" json:"thread_id"`
	TenantID      string         `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	Name          string         `gorm:"column:name;size:64" json:"name"`
	Phone         string         `gorm:"column:phone;size:32" json:"phone"`
	Address       string         `gorm:"column:address;size:255" json:"address"`
	Email         string         `gorm:"column:email;size:128" json:"email"`
	Remark        string         `gorm:"column:remark;size:500" json:"remark"`
	ExtractStatus int            `gorm:"column:extract_status;type:tinyint(1);default:0" json:"extract_status"` // 0=未提取 1=自动提取 2=人工填写
	CreatedAt     time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt     soft_delete.DeletedAt `gorm:"column:deleted_at;index" json:"-"` // 软删除
}

func (CustomerProfile) TableName() string { return "customer_profiles" }

// QuickReply 快捷回复话术（租户级）
type QuickReply struct {
	ID         uint64         `gorm:"primaryKey" json:"id"`
	TenantID   string         `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	Category   string         `gorm:"column:category;size:50;default:通用" json:"category"` // 分类：退款/物流/投诉/欢迎...
	Title      string         `gorm:"column:title;size:100" json:"title"`                 // 话术标题
	Content    string         `gorm:"column:content;type:text" json:"content"`            // 回复正文
	Sort       int            `gorm:"column:sort;default:0" json:"sort"`
	IsActive   int            `gorm:"column:is_active;type:tinyint(1);default:1" json:"is_active"` // 1启用 0停用
	CreatedAt  time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt  soft_delete.DeletedAt `gorm:"column:deleted_at;index" json:"-"` // 软删除
}

func (QuickReply) TableName() string { return "quick_replies" }

// AgentStatus 客服在线状态（工作台：在线/忙碌/离线 + 当前处理量）
// 子账号客服（users.id）接入；app_key 管理员（uid=0）不在池内，不参与自动分配
type AgentStatus struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	TenantID      string    `gorm:"column:tenant_id;size:64;uniqueIndex:uni_agent_tenant_user,priority:2" json:"tenant_id"`
	UserID        uint64    `gorm:"column:user_id;uniqueIndex:uni_agent_tenant_user,priority:1" json:"user_id"`
	Status        int       `gorm:"column:status;type:tinyint(1);default:0" json:"status"` // 0离线 1在线 2忙碌
	HandlingCount int       `gorm:"column:handling_count;default:0" json:"handling_count"` // 当前处理中工单数
	MaxConcurrent int       `gorm:"column:max_concurrent;default:3" json:"max_concurrent"` // 最大同时处理数
	UpdatedAt     time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (AgentStatus) TableName() string { return "agent_status" }

// AgentStatusRow 在线客服视图（join 成员昵称，前端状态条/分配用）
type AgentStatusRow struct {
	AgentStatus
	Nickname string `json:"nickname"`
	Username string `json:"username"`
}

// SatisfactionRating 会话满意度评价（用户端提交，一会话一评可覆盖）
type SatisfactionRating struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TenantID  string    `gorm:"column:tenant_id;size:64;index;uniqueIndex:uni_rating_thread,priority:1" json:"tenant_id"`
	ThreadID  string    `gorm:"column:thread_id;size:255;uniqueIndex:uni_rating_thread,priority:2" json:"thread_id"`
	TicketID  uint64    `gorm:"column:ticket_id;default:0" json:"ticket_id"` // 工单会话关联
	UserKey   string    `gorm:"column:user_key;size:64" json:"user_key"`
	Score     int       `gorm:"column:score;type:tinyint(1)" json:"score"` // 1-5 星
	Tags      string    `gorm:"column:tags;size:255" json:"tags"`          // 逗号分隔原因标签
	Comment   string    `gorm:"column:comment;size:500" json:"comment"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SatisfactionRating) TableName() string { return "satisfaction_ratings" }

// SessionTag 会话标签（运营人员给会话打标，同租户同会话唯一，逗号分隔多标签）
type SessionTag struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TenantID  string    `gorm:"column:tenant_id;size:64;index;uniqueIndex:uni_session_tag_thread,priority:1" json:"tenant_id"`
	ThreadID  string    `gorm:"column:thread_id;size:255;uniqueIndex:uni_session_tag_thread,priority:2" json:"thread_id"`
	Tags      string    `gorm:"column:tags;size:255" json:"tags"` // 逗号分隔
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SessionTag) TableName() string { return "session_tags" }

// SessionSummary 会话摘要（AI 自动生成，agent 后台任务回写）
type SessionSummary struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	TenantID  string    `gorm:"column:tenant_id;size:64;index;uniqueIndex:uni_session_summary_thread,priority:1" json:"tenant_id"`
	ThreadID  string    `gorm:"column:thread_id;size:255;uniqueIndex:uni_session_summary_thread,priority:2" json:"thread_id"`
	Summary   string    `gorm:"column:summary;type:text" json:"summary"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SessionSummary) TableName() string { return "session_summaries" }
// ModelProvider 模型提供商（平台管理员可增删改）
// 仅维护提供商元信息：名称 + 请求地址 + 默认模型名。API Key 不存此表，
// 租户的 Key 存 tenants.model_api_key / tenants.embedding_api_key（AES 密文）。
type ModelProvider struct {
	ID             uint64    `gorm:"primaryKey" json:"id"`
	// 联合唯一 (provider_key, provider_type)：同一提供商可同时提供 chat 与 embedding
	ProviderKey    string    `gorm:"column:provider_key;size:64;uniqueIndex:uni_model_providers_key_type" json:"provider_key"`
	ProviderName   string    `gorm:"column:provider_name;size:128" json:"provider_name"`
	DefaultModel   string    `gorm:"column:default_model;size:128" json:"default_model"`
	DefaultAPIBase string    `gorm:"column:default_api_base;size:256" json:"default_api_base"`
	ProviderType   string    `gorm:"column:provider_type;size:32;uniqueIndex:uni_model_providers_key_type" json:"provider_type"` // chat/embedding/both
	SortOrder      int       `gorm:"column:sort_order;type:int" json:"sort_order"`
	Status         int       `gorm:"column:status;type:tinyint(1)" json:"status"` // 1启用 0禁用
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ModelProvider) TableName() string { return "model_providers" }

// AuditLog 管理端操作审计日志（P1-3，MySQL 业务库）
type AuditLog struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	TenantID   string    `gorm:"column:tenant_id;size:64;index" json:"tenant_id"` // 平台管理员操作记 t_admin
	Operator   string    `gorm:"column:operator;size:64" json:"operator"`         // 操作人标识（tenant_id 或 app_key）
	UserID     uint64    `gorm:"column:user_id" json:"user_id"`                   // 子账号用户ID（0=app_key管理员），用于区分同名历史操作
	Action     string    `gorm:"column:action;size:32" json:"action"`             // create/update/delete/takeover/login
	ObjectType string    `gorm:"column:object_type;size:32;index" json:"object_type"` // ticket/prompt/knowledge/tenant/model_config/model_provider/session
	ObjectID   string    `gorm:"column:object_id;size:128" json:"object_id"`      // 对象标识（工单号/租户ID/文档ID等）
	Detail     string    `gorm:"column:detail;type:text" json:"detail"`           // 变更摘要（JSON）
	IP         string    `gorm:"column:ip;size:64" json:"ip"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

// ====================== RBAC：用户 / 角色 / 权限 ======================

// User 租户内成员账号（子账号）。租户管理员仍用 tenants.app_key/app_secret 登录（隐式管理员角色）
// 成员可挂多个角色（多角色权限取并集），角色关系存 user_roles 关联表
// 软删用 gorm.io/plugin/soft_delete（bigint 毫秒时间戳，0=未删）：
// 唯一索引 uni_users_tenant_username 含 deleted_at，软删行不再占用用户名、可重建同名；查询自动过滤 deleted_at != 0
type User struct {
	ID           uint64               `gorm:"primaryKey" json:"id"`
	TenantID     string               `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	Username     string               `gorm:"column:username;size:64" json:"username"`
	PasswordHash string               `gorm:"column:password_hash;size:255" json:"-"`
	Nickname     string               `gorm:"column:nickname;size:64" json:"nickname"`
	Phone        string               `gorm:"column:phone;size:32" json:"phone"`
	Email        string               `gorm:"column:email;size:128" json:"email"`
	Remark       string               `gorm:"column:remark;size:500" json:"remark"`
	Status       int                  `gorm:"column:status;type:tinyint(1);default:1" json:"status"` // 1正常 0禁用
	LastLoginAt  *time.Time           `gorm:"column:last_login_at" json:"last_login_at"`
	CreatedAt    time.Time            `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    time.Time            `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt    soft_delete.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (User) TableName() string { return "users" }

// UserRole 成员-角色关联（多对多）：一个成员可挂多个角色，权限取并集
type UserRole struct {
	UserID uint64 `gorm:"column:user_id;primaryKey" json:"user_id"`
	RoleID uint64 `gorm:"column:role_id;primaryKey" json:"role_id"`
}

func (UserRole) TableName() string { return "user_roles" }

// Role 角色：权限经 sys_role_menu 关联 menus（角色存菜单勾选集合）
// DeletedAt：软删除（删除保留记录，查询/分配自动过滤）
type Role struct {
	ID          uint64   `gorm:"primaryKey" json:"id"`
	TenantID    string   `gorm:"column:tenant_id;size:64;index" json:"tenant_id"`
	Code        string   `gorm:"column:code;size:32" json:"code"` // admin/staff/custom
	Name        string   `gorm:"column:name;size:64" json:"name"`
	Description string   `gorm:"column:description;size:255" json:"description"`
	MenuIDs     []uint64 `gorm:"-" json:"menu_ids"`                  // 关联菜单ID（授权回显/保存）
	IsAdmin     bool     `gorm:"-" json:"is_admin"`                   // 计算字段：权限覆盖全量=管理员角色（admin 角色可删可改，以权限为准）
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt   soft_delete.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

func (Role) TableName() string { return "roles" }

