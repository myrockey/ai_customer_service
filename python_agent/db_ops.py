"""P2-5：数据库操作与租户工具模块（从 main.py 拆分）
DB 表管理 / 会话 / Token 用量 / 配额 / Prompt DB 操作 / 会话清理
"""
import asyncio
import logging
from datetime import datetime, timedelta, timezone
from typing import Optional

import psycopg
from fastapi import FastAPI, Request

from config import DEFAULT_SYSTEM_PROMPT, GO_BUSINESS_API, INTERNAL_API_TOKEN

logger = logging.getLogger("agent.db_ops")


# ====================== 租户工具函数 ======================
def tenant_from_thread(thread_id: str) -> str:
    """从 thread_id / user_id 提取租户 ID（格式 {tenant_id}:{uid}）"""
    if ":" in thread_id:
        return thread_id.split(":", 1)[0]
    return ""


def tenant_from_request(request: Request) -> str:
    """从管理端透传头 X-Tenant-Id 提取租户"""
    return (request.headers.get("X-Tenant-Id") or "").strip()


def collection_name(tenant_id: str) -> str:
    """Qdrant 集合按租户隔离：kb_{tenant_id}"""
    return f"kb_{tenant_id}" if tenant_id else "kb_default"
# ====================== 数据库表 & 会话/提示词/知识库操作 ======================
# ====================== 数据库表 & 会话/提示词/知识库操作 ======================
# 表结构统一由 deploy/init/postgres/init.sql 导入（docker compose 首次启动
# 经 docker-entrypoint-initdb.d 自动执行），代码不维护 DDL，避免双源漂移。
# agent 启动时仅校验表存在性，缺表报清晰错误，不静默自动建表。
_AGENT_TABLES = ("platform_settings", "session_meta", "prompts", "prompt_versions",
                 "kb_documents", "token_usage")


async def verify_agent_tables(conn: psycopg.AsyncConnection):
    """校验 agent 依赖的表已存在（由 init SQL 导入）；缺表抛错提示执行 SQL"""
    async with conn.cursor() as cur:
        missing = []
        for t in _AGENT_TABLES:
            await cur.execute("SELECT to_regclass(%s)", (t,))
            if (await cur.fetchone())[0] is None:
                missing.append(t)
        if missing:
            raise RuntimeError(
                "agent 依赖表缺失: "
                + ", ".join(missing)
                + "。请先导入 deploy/init/postgres/init.sql（docker compose 首次启动会自动执行）"
            )
        logger.info("agent 依赖表校验通过: %s", ", ".join(_AGENT_TABLES))



async def touch_session(conn: psycopg.AsyncConnection, thread_id: str):
    """更新会话最后活跃时间与消息计数"""
    tenant_id = thread_id.split(":", 1)[0] if ":" in thread_id else ""
    async with conn.cursor() as cur:
        await cur.execute("""
            INSERT INTO session_meta (thread_id, tenant_id, last_active_at, message_count)
            VALUES (%s, %s, now(), 1)
            ON CONFLICT (thread_id) DO UPDATE SET
                last_active_at = now(),
                message_count = session_meta.message_count + 1
        """, (thread_id, tenant_id))


def extract_usage_metadata(messages) -> Optional[dict]:
    """从最终消息列表提取最后一个带 usage_metadata 的 AI 消息用量（best-effort）
    返回含输入缓存拆分：cache_hit_tokens/cache_miss_tokens 取自 response_metadata.usage
    （DeepSeek/通义等返回 prompt_cache_hit_tokens/prompt_cache_miss_tokens；不返回则 0=全部按未命中计）"""
    for msg in reversed(messages or []):
        um = getattr(msg, "usage_metadata", None)
        if um:
            rm = getattr(msg, "response_metadata", {}) or {}
            usage = rm.get("usage") or {}
            return {
                # OpenAI 兼容接口模型名在 response_metadata.model；部分实现放在 model_name
                "model": rm.get("model", "") or rm.get("model_name", "") or um.get("model_name", "") or "",
                "input_tokens": int(um.get("input_tokens") or 0),
                "output_tokens": int(um.get("output_tokens") or 0),
                "total_tokens": int(um.get("total_tokens") or 0),
                "cache_hit_tokens": int(usage.get("prompt_cache_hit_tokens") or 0),
                "cache_miss_tokens": int(usage.get("prompt_cache_miss_tokens") or 0),
            }
    return None


async def record_token_usage(conn: psycopg.AsyncConnection, tenant_id: str, thread_id: str,
                             messages, fallback_model: str = "", source: str = "platform",
                             chat_input_cache_price: float = 0.0, chat_input_price: float = 0.0,
                             chat_output_price: float = 0.0) -> None:
    """P1-1：记录一次对话的 LLM token 用量（异步 best-effort，失败仅告警不影响主流程）
    fallback_model：流式场景 chunk 的 response_metadata 不含 model，用租户生效模型名兜底
    source：platform=平台模型 / custom=租户自定义模型（平台模型专项统计用）
    单价（元/百万tokens，0=未定价）：输入分「命中缓存/未命中」两档 + 输出；
    费用按发生时单价快照，价格调整不影响历史记录。
    输入拆分：API 返回 cache_hit/cache_miss 则分档计价；不返回则全部按未命中价计。"""
    try:
        um = extract_usage_metadata(messages)
        if not um or (um["input_tokens"] == 0 and um["output_tokens"] == 0):
            return
        model = um["model"] or fallback_model or ""
        total = um["total_tokens"] or (um["input_tokens"] + um["output_tokens"])
        hit = um["cache_hit_tokens"] or 0
        miss = um["cache_miss_tokens"] or 0
        if hit == 0 and miss == 0:
            miss = um["input_tokens"]  # 无缓存拆分 → 全部输入按未命中价
        cost = (hit / 1_000_000.0 * chat_input_cache_price
                + miss / 1_000_000.0 * chat_input_price
                + um["output_tokens"] / 1_000_000.0 * chat_output_price)
        async with conn.cursor() as cur:
            await cur.execute("""
                INSERT INTO token_usage (tenant_id, thread_id, model, source, input_tokens, output_tokens, total_tokens, cost)
                VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
            """, (tenant_id, thread_id, model, source, um["input_tokens"], um["output_tokens"],
                  total, round(cost, 4)))
    except Exception as e:
        logger.warning("record token usage skipped: %s", e)


# ====================== P1-2 租户配额 ======================
_tenant_quota_cache: dict = {}
_TENANT_QUOTA_TTL = 30  # 秒


async def get_tenant_quota(app, tenant_id: str) -> dict:
    """获取租户配额（带 30s 缓存），失败返回默认不限制"""
    import time
    now = time.time()
    cached = _tenant_quota_cache.get(tenant_id)
    if cached and (now - cached.get("_ts", 0)) < _TENANT_QUOTA_TTL:
        return cached
    default = {"max_concurrent_sessions": 0, "daily_message_limit": 0, "max_kb_docs": 0, "max_kb_size_mb": 0}
    try:
        headers = {}
        if INTERNAL_API_TOKEN:
            headers["X-Internal-Token"] = INTERNAL_API_TOKEN
        resp = await app.state.http_client.get(
            f"{GO_BUSINESS_API}/api/agent/tenant-quota",
            params={"tenant_id": tenant_id}, headers=headers, timeout=5.0)
        data = resp.json()
        if data.get("code") == 0 and data.get("data"):
            d = data["data"]
            d["_ts"] = now
            _tenant_quota_cache[tenant_id] = d
            return d
    except Exception as e:
        logger.warning("get tenant quota failed: %s", str(e))
    return default


async def check_kb_quota(conn: psycopg.AsyncConnection, app, tenant_id: str) -> Optional[dict]:
    """知识库配额校验（文档数/容量），超限返回错误 dict，否则 None"""
    q = await get_tenant_quota(app, tenant_id)
    max_docs = int(q.get("max_kb_docs") or 0)
    max_size = int(q.get("max_kb_size_mb") or 0)
    if max_docs <= 0 and max_size <= 0:
        return None
    async with conn.cursor() as cur:
        await cur.execute(
            "SELECT count(*), COALESCE(SUM(file_size), 0) FROM kb_documents WHERE tenant_id = %s",
            (tenant_id,))
        row = await cur.fetchone()
    docs, size = row[0], row[1] or 0
    if max_docs > 0 and docs >= max_docs:
        return {"code": 429, "msg": f"知识库文档数已达上限（{max_docs} 篇），请删除部分文档后再上传"}
    if max_size > 0 and size >= max_size * 1024 * 1024:
        return {"code": 429, "msg": f"知识库容量已达上限（{max_size} MB），请清理后重试"}
    return None


# ====================== 平台设置（key-value，平台管理员动态配置） ======================
async def get_platform_setting(conn: psycopg.AsyncConnection, key: str, default: str = "") -> str:
    """读取平台设置，未配置时返回 default"""
    try:
        async with conn.cursor() as cur:
            await cur.execute("SELECT value FROM platform_settings WHERE key = %s", (key,))
            row = await cur.fetchone()
        return row[0] if row and row[0] else default
    except Exception:
        return default


async def set_platform_setting(conn: psycopg.AsyncConnection, key: str, value: str) -> None:
    """写入/更新平台设置（UPSERT）"""
    async with conn.cursor() as cur:
        await cur.execute(
            "INSERT INTO platform_settings (key, value, updated_at) VALUES (%s, %s, now()) "
            "ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()",
            (key, value))


async def check_concurrent_sessions(conn: psycopg.AsyncConnection, app, tenant_id: str) -> Optional[dict]:
    """并发会话配额校验（实时在线连接数，由 Go ws hub 统计），超限返回错误 dict，否则 None"""
    q = await get_tenant_quota(app, tenant_id)
    limit = int(q.get("max_concurrent_sessions") or 0)
    if limit <= 0:
        return None
    try:
        headers = {}
        if INTERNAL_API_TOKEN:
            headers["X-Internal-Token"] = INTERNAL_API_TOKEN
        resp = await app.state.http_client.get(
            f"{GO_BUSINESS_API}/api/agent/tenant-concurrency",
            params={"tenant_id": tenant_id}, headers=headers, timeout=5.0)
        data = resp.json()
        if data.get("code") == 0 and data.get("data"):
            cnt = int(data["data"].get("concurrent_sessions") or 0)
            if cnt >= limit:
                return {"code": 429, "msg": f"当前在线会话数已达上限（{limit}），请稍后再试"}
    except Exception as e:
        logger.warning("check concurrent sessions failed: %s", str(e))
    return None


async def load_active_prompt(conn: psycopg.AsyncConnection, tenant_id: str = "") -> str:
    """读取指定租户当前激活的 Prompt；租户无则回退全局（tenant_id=''）；再无则返回默认"""
    async with conn.cursor() as cur:
        if tenant_id:
            await cur.execute(
                "SELECT content FROM prompts WHERE tenant_id = %s AND is_active = true ORDER BY updated_at DESC LIMIT 1",
                (tenant_id,))
            row = await cur.fetchone()
            if row:
                return row[0]
        await cur.execute("SELECT content FROM prompts WHERE tenant_id = '' AND is_active = true ORDER BY updated_at DESC LIMIT 1")
        row = await cur.fetchone()
    return row[0] if row else DEFAULT_SYSTEM_PROMPT


async def seed_default_prompt(conn: psycopg.AsyncConnection):
    """全局 Prompt 为空时写入默认 Prompt 并激活（租户级未设置时回退使用）"""
    async with conn.cursor() as cur:
        await cur.execute("SELECT count(*) FROM prompts WHERE tenant_id = ''")
        (cnt,) = await cur.fetchone()
        if cnt == 0:
            await cur.execute(
                "INSERT INTO prompts (tenant_id, key, title, content, is_active) VALUES ('', %s, %s, %s, true)",
                ("default", "默认客服Prompt", DEFAULT_SYSTEM_PROMPT),
            )
            logger.info("✅ seeded default prompt")


async def upsert_prompt(conn: psycopg.AsyncConnection, tenant_id: str, key: str, title: str, content: str, is_active: bool):
    async with conn.cursor() as cur:
        await cur.execute("""
            INSERT INTO prompts (tenant_id, key, title, content, is_active, updated_at)
            VALUES (%s, %s, %s, %s, %s, now())
            ON CONFLICT (tenant_id, key) DO UPDATE SET
                title = EXCLUDED.title,
                content = EXCLUDED.content,
                is_active = EXCLUDED.is_active,
                updated_at = now()
        """, (tenant_id, key, title, content, is_active))
        if is_active:
            await cur.execute("UPDATE prompts SET is_active = false WHERE tenant_id = %s AND key <> %s", (tenant_id, key))


async def activate_prompt(conn: psycopg.AsyncConnection, tenant_id: str, key: str) -> Optional[str]:
    """激活指定租户的 Prompt 并返回其内容；不存在返回 None"""
    async with conn.cursor() as cur:
        await cur.execute("UPDATE prompts SET is_active = false WHERE tenant_id = %s", (tenant_id,))
        await cur.execute(
            "UPDATE prompts SET is_active = true, updated_at = now() WHERE tenant_id = %s AND key = %s",
            (tenant_id, key))
        await cur.execute("SELECT content FROM prompts WHERE tenant_id = %s AND key = %s", (tenant_id, key))
        row = await cur.fetchone()
    return row[0] if row else None


async def delete_prompt(conn: psycopg.AsyncConnection, tenant_id: str, key: str) -> bool:
    """删除指定租户的 Prompt，返回是否删除了激活项（需重建 Agent）"""
    async with conn.cursor() as cur:
        await cur.execute("SELECT is_active FROM prompts WHERE tenant_id = %s AND key = %s", (tenant_id, key))
        row = await cur.fetchone()
        if not row:
            return False
        was_active = bool(row[0])
        await cur.execute("DELETE FROM prompts WHERE tenant_id = %s AND key = %s", (tenant_id, key))
    return was_active


async def cleanup_sessions(conn: psycopg.AsyncConnection, checkpointer, days: int) -> dict:
    """回收超过 days 天未活跃的 LangGraph 会话：
    通过 session_meta 找到过期线程 → 调用 checkpointer.adelete_thread 清理 checkpoint → 删除 session_meta
    """
    cutoff = datetime.now(timezone.utc) - timedelta(days=days)
    async with conn.cursor() as cur:
        await cur.execute("SELECT thread_id FROM session_meta WHERE last_active_at < %s", (cutoff,))
        rows = await cur.fetchall()
    thread_ids = [r[0] for r in rows]

    deleted = 0
    for tid in thread_ids:
        try:
            await checkpointer.adelete_thread(tid)
            deleted += 1
        except Exception as e:
            logger.warning("delete thread %s failed: %s", tid, e)

    if thread_ids:
        async with conn.cursor() as cur:
            await cur.execute("DELETE FROM session_meta WHERE thread_id = ANY(%s)", (thread_ids,))
    return {"scanned": len(thread_ids), "deleted": deleted, "threads": thread_ids}


async def session_cleanup_loop(app: FastAPI):
    """后台定时回收过期会话"""
    if SESSION_CLEANUP_INTERVAL <= 0:
        logger.info("⏸ periodic session cleanup disabled")
        return
    await asyncio.sleep(SESSION_CLEANUP_INTERVAL)
    while True:
        try:
            # 多实例互斥：Redis 分布式锁（SETNX），抢不到锁则跳过本轮（其他实例在执行）
            if not await _acquire_cleanup_lock(app):
                logger.info("⏭ cleanup lock held by another instance, skip")
                await asyncio.sleep(SESSION_CLEANUP_INTERVAL)
                continue
            async with app.state.db_pool.connection() as conn:
                res = await cleanup_sessions(conn, app.state.checkpointer, SESSION_RETENTION_DAYS)
            logger.info("🔁 periodic session cleanup: %s", res)
        except Exception:
            logger.exception("periodic session cleanup failed")
        finally:
            await _release_cleanup_lock(app)
        # P2-2：同时清理过期的知识库检索缓存
        try:
            expired = _kb_cache_cleanup()
            if expired > 0:
                logger.info(f"🧹 KB cache cleanup: removed {expired} expired entries, total {len(_kb_cache)}")
        except Exception:
            logger.exception("KB cache cleanup failed")
        await asyncio.sleep(SESSION_CLEANUP_INTERVAL)


_CLEANUP_LOCK_KEY = "cs:lock:session_cleanup"
_CLEANUP_LOCK_TTL = 300  # 秒，超时自动释放防死锁


async def _acquire_cleanup_lock(app) -> bool:
    """Redis SETNX 抢锁；Redis 不可用 → 本轮不执行清理（fail-closed，Redis 为强制依赖）"""
    rc = getattr(getattr(app, "state", None), "redis_client", None)
    if rc is None:
        logger.warning("cleanup lock skipped: redis_client 未初始化")
        return False
    try:
        ok = await rc.set(_CLEANUP_LOCK_KEY, "1", nx=True, ex=_CLEANUP_LOCK_TTL)
        return bool(ok)
    except Exception as e:
        logger.warning("cleanup lock acquire failed, skip this round: %s", e)
        return False


async def _release_cleanup_lock(app) -> None:
    rc = getattr(getattr(app, "state", None), "redis_client", None)
    if rc is None:
        return
    try:
        await rc.delete(_CLEANUP_LOCK_KEY)
    except Exception:
        pass
