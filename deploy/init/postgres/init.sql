-- ============================================================================
-- 智能客服系统 - PostgreSQL 初始化脚本
-- 结构与 python_agent/main.py ensure_agent_tables 保持完全一致（v2 结构）
-- 注意：prompts 主键为 (tenant_id, key) 复合主键，prompt_versions 记录 Prompt 版本历史
-- ============================================================================

-- 启用 pgvector 扩展
CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================================
-- 会话元数据表（LangGraph 会话清理/过期回收）
-- ============================================================================
CREATE TABLE IF NOT EXISTS session_meta (
    thread_id      VARCHAR(128) PRIMARY KEY,
    tenant_id      VARCHAR(64) NOT NULL DEFAULT '',
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    message_count  INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_session_meta_tenant_active ON session_meta (tenant_id, last_active_at);

-- ============================================================================
-- Prompt 表（租户级，复合主键保证租户隔离）
-- ============================================================================
CREATE TABLE IF NOT EXISTS prompts (
    tenant_id  VARCHAR(64) NOT NULL DEFAULT '',
    key        VARCHAR(128) NOT NULL,
    title      VARCHAR(128) NOT NULL,
    content    TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key)
);

CREATE INDEX IF NOT EXISTS idx_prompts_tenant ON prompts (tenant_id);

-- ============================================================================
-- Prompt 版本历史表（与 agent prompt_version_* 接口一致）
-- ============================================================================
CREATE TABLE IF NOT EXISTS prompt_versions (
    id          BIGSERIAL PRIMARY KEY,
    prompt_key  VARCHAR(128) NOT NULL,
    tenant_id   VARCHAR(64) NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1,
    content     TEXT NOT NULL,
    remark      VARCHAR(500),
    created_by  VARCHAR(64),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_prompt_versions_prompt ON prompt_versions (tenant_id, prompt_key);
CREATE INDEX IF NOT EXISTS idx_prompt_versions_key ON prompt_versions (prompt_key, tenant_id);
CREATE INDEX IF NOT EXISTS idx_prompt_versions_tenant ON prompt_versions (tenant_id);

COMMENT ON COLUMN prompt_versions.prompt_key IS 'Prompt 标识';
COMMENT ON COLUMN prompt_versions.version IS '版本号（从1递增）';
COMMENT ON COLUMN prompt_versions.content IS 'Prompt 内容快照';
COMMENT ON COLUMN prompt_versions.remark IS '版本备注';
COMMENT ON TABLE prompt_versions IS 'Prompt 版本历史表';


-- ============================================================================
-- 知识库文档表（与 agent 知识库上传/检索接口一致，带扩展字段 file_name）
-- ============================================================================
CREATE TABLE IF NOT EXISTS kb_documents (
    id          BIGSERIAL PRIMARY KEY,
    doc_id      VARCHAR(64) NOT NULL UNIQUE,
    tenant_id   VARCHAR(64) NOT NULL DEFAULT '',
    title       VARCHAR(255) NOT NULL,
    chunk_count INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    file_name   VARCHAR(255),
    file_size   INT DEFAULT 0,
    category    VARCHAR(64) DEFAULT '默认',
    status      SMALLINT DEFAULT 1,
    error_msg   TEXT,
    file_url    VARCHAR(512),
    updated_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_kb_tenant ON kb_documents (tenant_id);
CREATE INDEX IF NOT EXISTS idx_kb_category ON kb_documents (category);
CREATE INDEX IF NOT EXISTS idx_kb_status ON kb_documents (status);

-- ============================================================================
-- Token 用量计量表（P1-1：租户级计费与用量报表）
-- ============================================================================
-- ============================================================================
-- 平台设置表（平台管理员动态配置，如知识库上传大小限制 kb_max_file_mb）
-- ============================================================================
CREATE TABLE IF NOT EXISTS platform_settings (
    key         VARCHAR(64) PRIMARY KEY,
    value       VARCHAR(512) NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE platform_settings IS '平台级 key-value 设置（agent 侧读写）';

-- ============================================================================
-- Token 用量计量表（按租户/会话/模型记录，用于计费与用量报表）
-- ============================================================================
CREATE TABLE IF NOT EXISTS token_usage (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     VARCHAR(64) NOT NULL DEFAULT '',
    thread_id     VARCHAR(255) NOT NULL DEFAULT '',
    model         VARCHAR(128) NOT NULL DEFAULT '',
    -- source: platform=平台模型 / custom=租户自定义（平台模型专项统计据此过滤）
    source        VARCHAR(16) NOT NULL DEFAULT 'platform',
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens  BIGINT NOT NULL DEFAULT 0,
    -- cost: 发生时按当时单价算好的费用快照（元），0=未定价；价格调整不影响历史记录
    cost          NUMERIC(12,4) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_token_usage_tenant_date ON token_usage (tenant_id, created_at);

-- ============================================================================
-- LangGraph Checkpoint 表（由 LangGraph 自动创建，这里预留）
-- ============================================================================

-- ============================================================================
-- 默认 Prompt 由 Agent 启动时自动 seed（seed_default_prompt），无需手动插入
-- ============================================================================
