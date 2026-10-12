package permission

// ====================== 租户内 RBAC 权限目录 ======================
// 权限码为代码常量（固定目录），与 menus 表 perm 字段一一对应（菜单=权限载体）。
// 权限码按若依标准三段式：xxx:view（查看）/ xxx:add（新增）/ xxx:edit（修改）/ xxx:remove（删除）。
// 平台管理员侧（租户管理/模型提供商/提取规则）不在此目录，由 PlatformAdminOnly 中间件管理。

// 会话
const (
	SessionView        = "session:view"         // 会话列表 + 聊天记录
	SessionProfileEdit = "session:profile:edit" // 编辑客户信息 / 重新提取
	SessionCleanup     = "session:cleanup"      // 会话数据维护：清理过期会话 / 归档旧消息
)

// 工单
const (
	TicketView   = "ticket:view"   // 查看工单
	TicketHandle = "ticket:handle" // 接管/回复/关闭/优先级/操作日志（操作类，不拆分）
)

// 客户
const (
	CustomerView = "customer:view" // 客户列表
)

// 知识库
const (
	KnowledgeView   = "knowledge:view"   // 查看知识库
	KnowledgeAdd    = "knowledge:add"    // 上传文档/分段
	KnowledgeEdit   = "knowledge:edit"   // 修改文档/分段
	KnowledgeRemove = "knowledge:remove" // 删除文档/分段
)

// Prompt
const (
	PromptView   = "prompt:view"   // 查看 Prompt
	PromptAdd    = "prompt:add"    // 新增 Prompt
	PromptEdit   = "prompt:edit"   // 修改 Prompt / 激活
	PromptRemove = "prompt:remove" // 删除 Prompt
)

// 模型（无新增/删除接口，仅查看 + 配置保存）
const (
	ModelView = "model:view" // 查看模型配置
	ModelEdit = "model:edit" // 配置模型
)

// 用量 / 审计
const (
	UsageView = "usage:view" // 用量统计
	AuditView = "audit:view" // 审计日志
)

// 快捷回复
const (
	QuickReplyView   = "quickreply:view"   // 查看话术
	QuickReplyAdd    = "quickreply:add"    // 新增话术
	QuickReplyEdit   = "quickreply:edit"   // 修改话术
	QuickReplyRemove = "quickreply:remove" // 删除话术
)

// 满意度
const (
	RatingView = "rating:view" // 查看满意度评价
)

// 成员 / 角色
const (
	MemberView   = "member:view"   // 查看成员
	MemberAdd    = "member:add"    // 新增成员
	MemberEdit   = "member:edit"   // 修改成员（禁用/重置密码/分配角色）
	MemberRemove = "member:remove" // 删除成员
	RoleView     = "role:view"     // 查看角色与权限
	RoleAdd      = "role:add"      // 新建角色
	RoleEdit     = "role:edit"     // 修改角色（授权）
	RoleRemove   = "role:remove"   // 删除角色
)

// 运营看板
const (
	DashboardView = "dashboard:view" // 运营看板（首页）
)

// Item 权限目录条目（管理端矩阵展示 / 启动校验基准）
type Item struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// Catalog 权限目录（与 menus 表初始数据 perm 一一对应；启动时互查缺失告警）
var Catalog = []Item{
	{Code: SessionView, Name: "查看会话记录", Group: "会话"},
	{Code: SessionProfileEdit, Name: "编辑客户信息", Group: "会话"},
	{Code: SessionCleanup, Name: "清理归档", Group: "会话"},
	{Code: TicketView, Name: "查看工单", Group: "工单"},
	{Code: TicketHandle, Name: "处理工单", Group: "工单"},
	{Code: CustomerView, Name: "查看客户列表", Group: "客户"},
	{Code: DashboardView, Name: "查看运营看板", Group: "运营"},
	{Code: KnowledgeView, Name: "查看知识库", Group: "知识库"},
	{Code: KnowledgeAdd, Name: "新增知识文档", Group: "知识库"},
	{Code: KnowledgeEdit, Name: "修改知识文档", Group: "知识库"},
	{Code: KnowledgeRemove, Name: "删除知识文档", Group: "知识库"},
	{Code: PromptView, Name: "查看 Prompt", Group: "Prompt"},
	{Code: PromptAdd, Name: "新增 Prompt", Group: "Prompt"},
	{Code: PromptEdit, Name: "修改 Prompt", Group: "Prompt"},
	{Code: PromptRemove, Name: "删除 Prompt", Group: "Prompt"},
	{Code: ModelView, Name: "查看模型配置", Group: "模型"},
	{Code: ModelEdit, Name: "配置模型", Group: "模型"},
	{Code: UsageView, Name: "查看用量统计", Group: "统计"},
	{Code: AuditView, Name: "查看审计日志", Group: "统计"},
	{Code: QuickReplyView, Name: "查看快捷回复", Group: "会话"},
	{Code: QuickReplyAdd, Name: "新增快捷回复", Group: "会话"},
	{Code: QuickReplyEdit, Name: "修改快捷回复", Group: "会话"},
	{Code: QuickReplyRemove, Name: "删除快捷回复", Group: "会话"},
	{Code: RatingView, Name: "查看满意度评价", Group: "会话"},
	{Code: MemberView, Name: "查看成员", Group: "成员角色"},
	{Code: MemberAdd, Name: "新增成员", Group: "成员角色"},
	{Code: MemberEdit, Name: "修改成员", Group: "成员角色"},
	{Code: MemberRemove, Name: "删除成员", Group: "成员角色"},
	{Code: RoleView, Name: "查看角色", Group: "成员角色"},
	{Code: RoleAdd, Name: "新建角色", Group: "成员角色"},
	{Code: RoleEdit, Name: "修改角色", Group: "成员角色"},
	{Code: RoleRemove, Name: "删除角色", Group: "成员角色"},
}

// AllCodes 全部权限码
func AllCodes() []string {
	codes := make([]string, 0, len(Catalog))
	for _, it := range Catalog {
		codes = append(codes, it.Code)
	}
	return codes
}

// AdminPerms 内置「管理员」角色权限（全部）
func AdminPerms() []string { return AllCodes() }

// StaffPerms 内置「客服」角色权限：会话 / 工单 / 客户 / 运营看板 / 知识库查看
func StaffPerms() []string {
	return []string{
		SessionView, SessionProfileEdit,
		TicketView, TicketHandle,
		CustomerView,
		DashboardView,
		KnowledgeView,
		QuickReplyView,
		RatingView,
	}
}
