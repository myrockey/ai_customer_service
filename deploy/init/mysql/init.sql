-- ============================================================================
-- 智能客服系统 - MySQL 初始化脚本
-- 覆盖全部业务表（9 张）：model_providers / platform_config / messages /
-- tickets / tenants / audit_logs / messages_archive / ticket_logs / orders
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
  `tenant_id` varchar(64) NOT NULL DEFAULT '' COMMENT '租户ID',
  `priority` tinyint(1) DEFAULT 1 COMMENT '0=低 1=中 2=高 3=紧急',
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
  PRIMARY KEY (`id`),
  UNIQUE KEY `uni_tenants_tenant_id` (`tenant_id`),
  UNIQUE KEY `uni_tenants_app_key` (`app_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='租户表';

-- 管理端操作审计日志表
CREATE TABLE IF NOT EXISTS `audit_logs` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `tenant_id` varchar(64) DEFAULT NULL COMMENT '租户ID（平台管理员记 t_admin）',
  `operator` varchar(64) DEFAULT NULL COMMENT '操作人标识',
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

INSERT INTO `orders` (`id`,`order_id`,`user_name`,`item`,`amount`,`status`,`create_date`,`created_at`) VALUES (1,'ORD-2024-001','小明','VIP 年费会员',799,'已完成','2024-01-15','2026-09-03 19:33:02');
INSERT INTO `orders` (`id`,`order_id`,`user_name`,`item`,`amount`,`status`,`create_date`,`created_at`) VALUES (2,'ORD-2024-002','小明','Python 实战课程',199,'配送中','2024-03-20','2026-09-03 19:33:02');
