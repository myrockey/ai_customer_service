-- ============================================================================
-- 智能客服系统 - MySQL 初始化脚本
-- 覆盖全部业务表（12 张）：model_providers / platform_config / messages /
-- tickets / tenants / audit_logs / messages_archive / ticket_logs / orders / customer_profiles
-- roles / users
-- 与 Go AutoMigrate + ensureTenantSchema 结构完全对齐，Go 启动时幂等跳过；
-- 种子租户由 Go 从 config.yaml 写入（seedTenantsFromConfig），避免密钥落库
-- ============================================================================

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- ============================================================================
-- 模型提供商表（列名与 Go model.ModelProvider 保持一致：provider_key）
-- 唯一键为 (provider_key, provider_type)：同提供商可分 chat / embedding 两条
-- ============================================================================
CREATE TABLE IF NOT EXISTS `model_providers` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `provider_key` varchar(64) NOT NULL COMMENT '提供商代码',
  `provider_name` varchar(128) NOT NULL COMMENT '提供商名称',
  `default_model` varchar(128) DEFAULT NULL COMMENT '默认模型',
  `default_api_base` varchar(256) DEFAULT NULL COMMENT '默认API地址',
  `provider_type` varchar(32) NOT NULL COMMENT '类型：chat embedding',
  `sort_order` int(11) DEFAULT 0 COMMENT '排序',
  `status` tinyint(1) DEFAULT 1 COMMENT '状态：1启用 0禁用',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_model_providers_key_type` (`provider_key`, `provider_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模型提供商表（仅元信息：名称/默认模型/请求地址，不存API Key）';

-- ============================================================================
-- 初始数据：模型提供商（与开发环境一致；不含 custom——custom 由管理员按需新建）
-- API Key 不存此表：租户 Key 存 tenants.model_api_key / tenants.embedding_api_key（AES 密文）
-- ============================================================================
INSERT INTO `model_providers` (`provider_key`, `provider_name`, `default_model`, `default_api_base`, `provider_type`, `sort_order`, `status`) VALUES
('deepseek', 'DeepSeek', 'deepseek-chat', 'https://api.deepseek.com/v1', 'chat', 1, 1),
('openai', 'OpenAI', 'gpt-4o-mini', 'https://api.openai.com/v1', 'chat', 2, 1),
('qwen', '通义千问', 'qwen-turbo', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'chat', 3, 1),
('moonshot', '月之暗面', 'moonshot-v1-8k', 'https://api.moonshot.cn/v1', 'chat', 4, 1),
('agnes', 'AGNES', 'agnes-2.5-flash', 'https://api.agnes-ai.cn/v1', 'chat', 5, 1),
('dashscope', '阿里云百炼', 'qwen3.7-text-embedding-flash', 'https://dashscope.aliyuncs.com/compatible-mode/v1', 'embedding', 1, 1)
ON DUPLICATE KEY UPDATE `provider_name`=VALUES(`provider_name`);

-- ============================================================================
-- 平台默认模型配置表（key-value，与 Go model.PlatformConfig 一致）
-- 空表即可，配置由平台管理员在后台保存后写入
-- ============================================================================
CREATE TABLE IF NOT EXISTS `platform_config` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `config_key` varchar(128) NOT NULL COMMENT '配置键',
  `config_value` varchar(512) DEFAULT '' COMMENT '配置值（API Key 为 AES 密文）',
  `description` varchar(255) DEFAULT '' COMMENT '配置说明',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_config_key` (`config_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='平台默认模型配置表';

-- ============================================================================
-- 种子租户：由 Go 服务启动时从 config.yaml 写入（含 app_secret 哈希）
-- ============================================================================

-- ============================================================================
-- 以下业务表与 Go AutoMigrate / ensureTenantSchema 完全对齐（列、索引、默认值）。
-- 显式建表保证：仅执行初始化脚本（不启动 Go）时表也齐全；Go 启动时幂等跳过。
-- ============================================================================

-- 会话消息表（用户/机器人/人工客服/系统消息）
CREATE TABLE IF NOT EXISTS `messages` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `thread_id` varchar(255) DEFAULT NULL COMMENT '会话线程ID',
  `role` varchar(16) DEFAULT NULL COMMENT 'user/ai/human/system',
  `content` text COMMENT '消息内容',
  `created_at` datetime(3) DEFAULT NULL,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `is_read` tinyint(1) DEFAULT 0 COMMENT '0=未读 1=已读',
  `ticket_id` bigint(20) unsigned DEFAULT NULL COMMENT '归属工单ID，NULL=不归属',
  PRIMARY KEY (`id`),
  KEY `idx_messages_thread_id` (`thread_id`),
  KEY `idx_messages_tenant_id` (`tenant_id`),
  KEY `idx_messages_tenant_thread` (`tenant_id`,`thread_id`(64)),
  KEY `idx_messages_thread_read` (`thread_id`,`is_read`),
  KEY `idx_messages_thread_time` (`thread_id`,`created_at`),
  KEY `idx_messages_created_at` (`created_at`),
  KEY `idx_messages_tenant_created` (`tenant_id`,`created_at`),
  KEY `idx_messages_ticket_id` (`ticket_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话消息表';

-- 工单表（人工客服转接）
CREATE TABLE IF NOT EXISTS `tickets` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `thread_id` varchar(255) DEFAULT NULL COMMENT '会话线程ID',
  `ticket_no` varchar(64) DEFAULT NULL COMMENT '工单号',
  `user_id` varchar(255) COMMENT '用户ID',
  `reason` text COMMENT '转人工原因',
  `status` tinyint(1) DEFAULT NULL COMMENT '0待处理 1已转接/接管中 2已取消 3已关闭',
  `created_at` datetime(3) DEFAULT NULL,
  `deleted_at` bigint(20) NOT NULL DEFAULT 0 COMMENT '软删除时间(ms)，0=未删除',
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `priority` tinyint(1) DEFAULT 1 COMMENT '0=低 1=中 2=高 3=紧急',
  `handler_uid` bigint(20) unsigned DEFAULT 0 COMMENT '接管客服子账号ID（0=app_key管理员）',
  `handler_name` varchar(64) DEFAULT NULL COMMENT '接管客服名（冗余快照）',
  `closed_at` datetime(3) DEFAULT NULL COMMENT '关闭/取消时刻',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_tickets_ticket_no` (`ticket_no`),
  KEY `idx_tickets_tenant_id` (`tenant_id`),
  KEY `idx_tickets_tenant_status` (`tenant_id`,`status`),
  KEY `idx_tickets_tenant_status_time` (`tenant_id`,`status`,`created_at`),
  KEY `idx_tickets_created_at` (`created_at`),
  KEY `idx_ticket_thread_status` (`thread_id`,`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='工单表';

-- 租户表（模型 BYOK 配置 + 配额字段，与 model.TenantRow 一致）
CREATE TABLE IF NOT EXISTS `tenants` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) DEFAULT NULL COMMENT '租户ID',
  `name` varchar(128) COMMENT '租户名称',
  `app_key` varchar(64) DEFAULT NULL COMMENT '应用Key',
  `app_secret_hash` varchar(255) COMMENT 'app_secret 哈希',
  `is_platform_admin` tinyint(1) DEFAULT NULL COMMENT '是否平台管理员',
  `status` tinyint(1) DEFAULT NULL COMMENT '1正常 0禁用',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `model_provider` varchar(32) COMMENT 'platform=平台默认 custom=租户自定义',
  `model_name` varchar(128) COMMENT '模型名称',
  `model_api_key` varchar(512) COMMENT 'AES加密后的API Key',
  `model_api_base` varchar(256) COMMENT '自定义API Base',
  `embedding_provider` varchar(32) COMMENT 'platform=平台默认 custom=租户自定义',
  `embedding_model_name` varchar(128) COMMENT 'embedding模型名称',
  `embedding_api_key` varchar(512) COMMENT 'AES加密后的embedding Key',
  `embedding_api_base` varchar(256) COMMENT 'embedding API Base',
  `embedding_mode` varchar(32) COMMENT 'embedding模式',
  `chat_input_cache_price` decimal(10,4) NOT NULL DEFAULT 0 COMMENT '对话输入单价-命中缓存（元/百万tokens，0=未定价）',
  `chat_input_price` decimal(10,4) NOT NULL DEFAULT 0 COMMENT '对话输入单价（元/百万tokens，0=未定价）',
  `chat_output_price` decimal(10,4) NOT NULL DEFAULT 0 COMMENT '对话输出单价（元/百万tokens，0=未定价）',
  `embedding_price` decimal(10,4) NOT NULL DEFAULT 0 COMMENT 'embedding单价（元/百万tokens，0=未定价）',
  `max_concurrent_sessions` int DEFAULT 100 COMMENT '并发会话数上限，0=不限',
  `daily_message_limit` int DEFAULT 1000 COMMENT '每日消息数上限，0=不限',
  `max_kb_docs` int DEFAULT 100 COMMENT '知识库文档数上限，0=不限',
  `max_kb_size_mb` int DEFAULT 100 COMMENT '知识库容量上限MB，0=不限',
  `model_provider_key` varchar(64) COMMENT '模型提供商标识',
  `embedding_provider_key` varchar(64) COMMENT 'embedding提供商标识',
  `llm_provider_id` bigint unsigned DEFAULT 0 COMMENT '对话模型提供商ID（关联model_providers.id，普通字段）',
  `embedding_provider_id` bigint unsigned DEFAULT 0 COMMENT 'Embedding模型提供商ID（关联model_providers.id，普通字段）',
  `deleted_at` bigint(20) NOT NULL DEFAULT 0 COMMENT '软删除时间(ms)，0=未删除',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_tenants_tenant_id` (`tenant_id`),
  UNIQUE KEY `uni_tenants_app_key` (`app_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户表';

-- 管理端操作审计日志表
CREATE TABLE IF NOT EXISTS `audit_logs` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) DEFAULT NULL COMMENT '租户ID（平台管理员记 t_admin）',
  `operator` varchar(64) DEFAULT NULL COMMENT '操作人标识',
  `user_id` bigint(20) unsigned DEFAULT 0 COMMENT '子账号用户ID（0=app_key管理员），区分同名操作人',
  `action` varchar(32) DEFAULT NULL COMMENT 'create/update/delete/takeover/login',
  `object_type` varchar(32) DEFAULT NULL COMMENT 'ticket/prompt/knowledge/tenant/model_config/model_provider/session',
  `object_id` varchar(128) DEFAULT NULL COMMENT '对象标识',
  `detail` text COMMENT '变更摘要JSON',
  `ip` varchar(64) DEFAULT NULL COMMENT '操作IP',
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_audit_logs_tenant_id` (`tenant_id`),
  KEY `idx_audit_logs_object_type` (`object_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='管理端操作审计日志表';

-- 归档消息表（messages 超保留期后按月归档）
CREATE TABLE IF NOT EXISTS `messages_archive` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `thread_id` varchar(255) DEFAULT NULL COMMENT '会话线程ID',
  `role` varchar(16) DEFAULT NULL COMMENT 'user/ai/human/system',
  `content` text COMMENT '消息内容',
  `tenant_id` varchar(64) DEFAULT NULL COMMENT '租户ID',
  `is_read` tinyint(1) DEFAULT 0 COMMENT '0=未读 1=已读',
  `created_at` datetime(3) DEFAULT NULL,
  `archived_at` datetime(3) DEFAULT NULL COMMENT '归档时间',
  `ticket_id` bigint(20) unsigned DEFAULT NULL COMMENT '归属工单ID',
  PRIMARY KEY (`id`),
  KEY `idx_messages_archive_thread_id` (`thread_id`),
  KEY `idx_messages_archive_tenant_id` (`tenant_id`),
  KEY `idx_messages_archive_ticket_id` (`ticket_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='归档消息表';

-- 工单操作历史表（转接/接管/关闭/取消/优先级变更等）
CREATE TABLE IF NOT EXISTS `ticket_logs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `ticket_id` bigint NOT NULL COMMENT '工单ID',
  `thread_id` varchar(128) NOT NULL COMMENT '会话线程ID',
  `tenant_id` varchar(64) NOT NULL COMMENT '租户ID',
  `action` varchar(32) NOT NULL COMMENT '动作：create/assign/handle/resolve/close/cancel/priority/transfer',
  `old_status` tinyint(1) DEFAULT NULL COMMENT '原状态',
  `new_status` tinyint(1) DEFAULT NULL COMMENT '新状态',
  `old_priority` tinyint(1) DEFAULT NULL COMMENT '原优先级',
  `new_priority` tinyint(1) DEFAULT NULL COMMENT '新优先级',
  `operator` varchar(64) DEFAULT NULL COMMENT '操作人',
  `remark` varchar(500) DEFAULT NULL COMMENT '备注',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_ticket_logs_ticket` (`ticket_id`),
  KEY `idx_ticket_logs_tenant` (`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='工单操作历史表';

-- 订单表（Agent 订单查询工具使用，演示数据可后续维护）
CREATE TABLE IF NOT EXISTS `orders` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `order_id` varchar(64) NOT NULL COMMENT '订单号 如 ORD2024001',
  `user_name` varchar(64) DEFAULT NULL COMMENT '用户名',
  `item` varchar(128) DEFAULT NULL COMMENT '商品',
  `amount` decimal(12,2) DEFAULT 0 COMMENT '金额（元）',
  `status` varchar(32) DEFAULT NULL COMMENT '状态',
  `create_date` varchar(32) DEFAULT NULL COMMENT '下单日期',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `order_id` (`order_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='订单表';

-- 客户信息表（会话维度：正则自动提取 phone/email + 后台人工补全 name/address/remark）
CREATE TABLE IF NOT EXISTS `customer_profiles` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `thread_id` varchar(255) NOT NULL COMMENT '会话线程ID',
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `name` varchar(64) DEFAULT NULL COMMENT '客户姓名（人工填写）',
  `phone` varchar(32) DEFAULT NULL COMMENT '联系电话（正则自动提取）',
  `address` varchar(255) DEFAULT NULL COMMENT '地址（人工填写）',
  `email` varchar(128) DEFAULT NULL COMMENT '邮箱（正则自动提取）',
  `remark` varchar(500) DEFAULT NULL COMMENT '备注（人工填写）',
  `extract_status` tinyint(1) DEFAULT 0 COMMENT '0=未提取 1=自动提取 2=人工填写',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at` bigint(20) NOT NULL DEFAULT 0 COMMENT '软删除时间(ms)，0=未删除',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_customer_profiles_thread` (`thread_id`),
  KEY `idx_customer_profiles_tenant` (`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户信息表';

INSERT INTO `orders` (`id`,`order_id`,`user_name`,`item`,`amount`,`status`,`create_date`,`created_at`) VALUES (1,'ORD-2024-001','小明','VIP 年费会员',799,'已完成','2024-01-15','2026-09-03 19:33:02');
INSERT INTO `orders` (`id`,`order_id`,`user_name`,`item`,`amount`,`status`,`create_date`,`created_at`) VALUES (2,'ORD-2024-002','小明','Python 实战课程',199,'配送中','2024-03-20','2026-09-03 19:33:02');

-- RBAC：角色表（租户内，内置 admin/staff 由 EnsureTenantRoles 懒初始化）
CREATE TABLE IF NOT EXISTS `roles` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `code` varchar(32) NOT NULL DEFAULT '' COMMENT '角色码 admin/staff/custom',
  `name` varchar(64) NOT NULL DEFAULT '' COMMENT '角色名',
  `description` varchar(255) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at` bigint(20) NOT NULL DEFAULT 0 COMMENT '软删除时间(ms)，0=未删除',
  PRIMARY KEY (`id`),
  KEY `idx_roles_tenant` (`tenant_id`),
  KEY `idx_roles_tenant_code` (`tenant_id`,`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='RBAC角色表';

-- RBAC：成员表（租户内子账号，客服等）
CREATE TABLE IF NOT EXISTS `users` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `username` varchar(64) NOT NULL COMMENT '用户名（租户内唯一）',
  `password_hash` varchar(255) NOT NULL COMMENT '密码哈希（SHA256(租户|用户名|密码)）',
  `nickname` varchar(64) DEFAULT NULL COMMENT '昵称/显示名',
  `phone` varchar(32) DEFAULT NULL COMMENT '手机号',
  `email` varchar(128) DEFAULT NULL COMMENT '邮箱',
  `remark` varchar(500) DEFAULT NULL COMMENT '备注',
  `status` tinyint(1) DEFAULT 1 COMMENT '1=正常 0=禁用',
  `last_login_at` datetime DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at` bigint(20) NOT NULL DEFAULT 0 COMMENT '软删除时间(ms)，0=未删除（纳入唯一索引，软删后用户名可复用）',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_users_tenant_username` (`tenant_id`,`username`,`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='RBAC成员表';


-- RBAC：成员-角色关联表（多对多，一个成员可挂多个角色，权限取并集）
CREATE TABLE IF NOT EXISTS `user_roles` (
  `user_id` bigint(20) unsigned NOT NULL COMMENT '成员ID',
  `role_id` bigint(20) unsigned NOT NULL COMMENT '角色ID',
  PRIMARY KEY (`user_id`,`role_id`),
  KEY `idx_user_roles_role` (`role_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='RBAC成员-角色关联表';


-- 快捷回复话术表（租户级客服话术库）
CREATE TABLE IF NOT EXISTS `quick_replies` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `category` varchar(50) NOT NULL DEFAULT '通用' COMMENT '分类：退款/物流/投诉/欢迎...',
  `title` varchar(100) NOT NULL COMMENT '话术标题',
  `content` text NOT NULL COMMENT '回复正文',
  `sort` int NOT NULL DEFAULT 0 COMMENT '排序（小在前）',
  `is_active` tinyint(1) NOT NULL DEFAULT 1 COMMENT '1=启用 0=停用',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `deleted_at` bigint(20) NOT NULL DEFAULT 0 COMMENT '软删除时间(ms)，0=未删除',
  PRIMARY KEY (`id`),
  KEY `idx_quick_replies_tenant` (`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='快捷回复话术表';

-- 客服在线状态表（工作台：在线/忙碌/离线 + 当前处理量）
CREATE TABLE IF NOT EXISTS `agent_status` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `user_id` bigint(20) unsigned NOT NULL COMMENT '客服成员ID（users.id）',
  `status` tinyint(1) NOT NULL DEFAULT 0 COMMENT '0=离线 1=在线 2=忙碌',
  `handling_count` int NOT NULL DEFAULT 0 COMMENT '当前处理中工单数',
  `max_concurrent` int NOT NULL DEFAULT 3 COMMENT '最大同时处理数',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_agent_status_tenant_user` (`tenant_id`,`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客服在线状态表';

-- 会话满意度评价表（用户端提交，同租户同会话唯一，可覆盖）
CREATE TABLE IF NOT EXISTS `satisfaction_ratings` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `thread_id` varchar(255) NOT NULL DEFAULT '' COMMENT '会话ID（用户端 user_id）',
  `ticket_id` bigint(20) unsigned NOT NULL DEFAULT 0 COMMENT '关联工单ID',
  `user_key` varchar(64) NOT NULL DEFAULT '' COMMENT '用户标识',
  `score` tinyint(1) NOT NULL COMMENT '评分 1-5 星',
  `tags` varchar(255) NOT NULL DEFAULT '' COMMENT '原因标签，逗号分隔',
  `comment` varchar(500) NOT NULL DEFAULT '' COMMENT '留言',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_rating_thread` (`tenant_id`,`thread_id`),
  KEY `idx_rating_created` (`tenant_id`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话满意度评价表';

-- 会话标签表（运营人员给会话打标，同租户同会话唯一，逗号分隔多标签）
CREATE TABLE IF NOT EXISTS `session_tags` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `thread_id` varchar(255) NOT NULL DEFAULT '' COMMENT '会话ID（用户端 user_id）',
  `tags` varchar(255) NOT NULL DEFAULT '' COMMENT '标签，逗号分隔',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_session_tag_thread` (`tenant_id`,`thread_id`),
  KEY `idx_session_tag_tenant` (`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话标签表';
CREATE TABLE IF NOT EXISTS `session_summaries` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `thread_id` varchar(255) NOT NULL DEFAULT '' COMMENT '会话ID（用户端 user_id）',
  `summary` text COMMENT '会话摘要（AI 自动生成）',
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_session_summary_thread` (`tenant_id`,`thread_id`),
  KEY `idx_session_summary_tenant` (`tenant_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话摘要表';
-- ====================== 动态菜单权限（RuoYi 模式：菜单=权限载体） ======================
-- 目录(type=1)/页面(type=2)/按钮(type=3)；perm 即权限码，全局唯一。
-- 系统管理组为平台级菜单（perm 留空，仅平台管理员 uid=0 可见）；业务中心组参与租户 RBAC。
CREATE TABLE IF NOT EXISTS `menus` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `parent_id` bigint(20) unsigned NOT NULL DEFAULT 0 COMMENT '父节点ID，0=顶层',
  `group_name` varchar(32) NOT NULL DEFAULT '' COMMENT '侧边栏分组：业务中心/系统管理',
  `name` varchar(64) NOT NULL COMMENT '菜单名',
  `type` tinyint(4) NOT NULL DEFAULT 2 COMMENT '1=目录 2=页面 3=按钮/操作',
  `path` varchar(128) DEFAULT '' COMMENT '前端路由路径(type 1/2)',
  `component` varchar(128) DEFAULT '' COMMENT 'views 相对路径(type 2)',
  `perm` varchar(64) DEFAULT '' COMMENT '权限码(type 2/3)，全局唯一',
  `icon` varchar(64) DEFAULT '' COMMENT '图标',
  `sort` int(11) NOT NULL DEFAULT 0 COMMENT '排序',
  `visible` tinyint(4) NOT NULL DEFAULT 1 COMMENT '1显示 0隐藏(路由保留)',
  `status` tinyint(4) NOT NULL DEFAULT 1 COMMENT '1启用 0停用(隐藏+校验拒绝)',
  `is_builtin` tinyint(4) NOT NULL DEFAULT 0 COMMENT '1=内置(标记+删除提示，不锁)',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_menus_perm` (`perm`),
  KEY `idx_menus_parent` (`parent_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='菜单权限表';

-- 角色-菜单关联（授权=勾选菜单树；角色存 menu_ids）
CREATE TABLE IF NOT EXISTS `sys_role_menu` (
  `role_id` bigint(20) unsigned NOT NULL COMMENT '角色ID',
  `menu_id` bigint(20) unsigned NOT NULL COMMENT '菜单ID',
  PRIMARY KEY (`role_id`,`menu_id`),
  KEY `idx_rm_menu` (`menu_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='角色-菜单关联表';

-- 内置菜单初始数据（is_builtin=1；业务中心组 + 系统管理组）
INSERT INTO `menus` (`id`,`parent_id`,`group_name`,`name`,`type`,`path`,`component`,`perm`,`sort`,`visible`,`status`,`is_builtin`) VALUES
(1,0,'业务中心','会话管理',1,'','',NULL,1,1,1,1),
(2,1,'业务中心','运营看板',2,'/ops','views/Ops.vue','dashboard:view',1,1,1,1),
(10,1,'业务中心','会话列表',2,'/sessions','views/Sessions.vue','session:view',2,1,1,1),
(11,10,'业务中心','编辑客户信息',3,'','','session:profile:edit',1,1,1,1),
(12,10,'业务中心','清理归档',3,'','','session:cleanup',2,1,1,1),
(13,1,'业务中心','客户列表',2,'/customers','views/Customers.vue','customer:view',3,1,1,1),
(5,1,'业务中心','快捷回复',2,'/quick-replies','views/QuickReplies.vue','quickreply:view',4,1,1,1),
(6,5,'业务中心','新增快捷回复',3,'','','quickreply:add',1,1,1,1),
(7,5,'业务中心','修改快捷回复',3,'','','quickreply:edit',2,1,1,1),
(8,5,'业务中心','删除快捷回复',3,'','','quickreply:remove',3,1,1,1),
(9,1,'业务中心','满意度评价',2,'/ratings','views/Ratings.vue','rating:view',5,1,1,1),
(40,0,'业务中心','工单管理',1,'','',NULL,2,1,1,1),
(3,40,'业务中心','工单列表',2,'/tickets','views/Tickets.vue','ticket:view',1,1,1,1),
(4,3,'业务中心','处理工单',3,'','','ticket:handle',1,1,1,1),
(42,0,'业务中心','知识库管理',1,'','',NULL,3,1,1,1),
(18,42,'业务中心','知识库',2,'/knowledge','views/Knowledge.vue','knowledge:view',1,1,1,1),
(19,18,'业务中心','新增文档',3,'','','knowledge:add',1,1,1,1),
(20,18,'业务中心','修改文档',3,'','','knowledge:edit',2,1,1,1),
(21,18,'业务中心','删除文档',3,'','','knowledge:remove',3,1,1,1),
(43,0,'业务中心','模型管理',1,'','',NULL,4,1,1,1),
(22,43,'业务中心','模型配置',2,'/model-config','views/TenantModelConfig.vue','model:view',1,1,1,1),
(23,22,'业务中心','配置模型',3,'','','model:edit',1,1,1,1),
(14,43,'业务中心','Prompt 管理',2,'/prompts','views/Prompt.vue','prompt:view',2,1,1,1),
(15,14,'业务中心','新增 Prompt',3,'','','prompt:add',1,1,1,1),
(16,14,'业务中心','修改 Prompt',3,'','','prompt:edit',2,1,1,1),
(17,14,'业务中心','删除 Prompt',3,'','','prompt:remove',3,1,1,1),
(44,0,'业务中心','统计管理',1,'','',NULL,5,1,1,1),
(24,44,'业务中心','用量统计',2,'/token-usage','views/TokenUsage.vue','usage:view',1,1,1,1),
(25,44,'业务中心','审计日志',2,'/audit-logs','views/AuditLogs.vue','audit:view',2,1,1,1),
(45,0,'业务中心','用户管理',1,'','',NULL,6,1,1,1),
(26,45,'业务中心','用户列表',2,'/members','views/Users.vue','member:view',1,1,1,1),
(27,26,'业务中心','新增用户',3,'','','member:add',1,1,1,1),
(28,26,'业务中心','修改用户',3,'','','member:edit',2,1,1,1),
(29,26,'业务中心','删除用户',3,'','','member:remove',3,1,1,1),
(30,45,'业务中心','角色权限',2,'/roles','views/Roles.vue','role:view',2,1,1,1),
(31,30,'业务中心','新增角色',3,'','','role:add',1,1,1,1),
(32,30,'业务中心','修改角色',3,'','','role:edit',2,1,1,1),
(33,30,'业务中心','删除角色',3,'','','role:remove',3,1,1,1),
(110,0,'系统管理','租户管理',1,'','',NULL,1,1,1,1),
(100,110,'系统管理','租户列表',2,'/tenants','views/Tenants.vue',NULL,1,1,1,1),
(113,0,'系统管理','模型管理',1,'','',NULL,2,1,1,1),
(101,113,'系统管理','模型提供商',2,'/model-providers','views/ModelProviders.vue',NULL,1,1,1,1),
(102,113,'系统管理','平台模型配置',2,'/platform-model','views/PlatformModelConfig.vue',NULL,2,1,1,1),
(103,113,'系统管理','Prompt 管理',2,'/prompts','views/Prompt.vue',NULL,3,1,1,1),
(111,0,'系统管理','统计管理',1,'','',NULL,3,1,1,1),
(104,111,'系统管理','用量统计',2,'/token-usage','views/TokenUsage.vue',NULL,1,1,1,1),
(105,111,'系统管理','审计日志',2,'/audit-logs','views/AuditLogs.vue',NULL,2,1,1,1),
(112,0,'系统管理','平台配置',1,'','',NULL,4,1,1,1),
(106,112,'系统管理','平台设置',2,'/platform-settings','views/PlatformSettings.vue',NULL,1,1,1,1),
(107,112,'系统管理','菜单权限',2,'/menus','views/MenuPermissions.vue',NULL,2,1,1,1);
