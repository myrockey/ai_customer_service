"""P2-5：Prompt 管理路由（从 main.py 拆分，按租户隔离）"""
import json
import logging

from fastapi import APIRouter, Request

from agent_factory import invalidate_platform_prompt_agents, rebuild_tenant_agent
from db_ops import (activate_prompt, delete_prompt, load_active_prompt,
                    tenant_from_request, upsert_prompt)
from db_pool import _pool_conn
from models import PromptReq
from redis_pubsub import INVALIDATE_CHANNEL

logger = logging.getLogger("agent.routers_prompt")
router = APIRouter()


def _tenant_scope(request: Request) -> str:
    """Prompt 管理租户作用域：Go 透传 __platform__（平台管理员管理平台级全局 Prompt）→ 全局 tenant_id=''"""
    tid = tenant_from_request(request)
    return "" if tid == "__platform__" else tid


# ====================== Prompt 管理（按租户隔离；__platform__=平台级全局） ======================
@router.get("/agent/prompts")
async def prompts_list(request: Request):
    tenant_id = _tenant_scope(request)
    keyword = (request.query_params.get("keyword") or "").strip()
    page = max(1, int(request.query_params.get("page", 1)))
    page_size = min(max(1, int(request.query_params.get("page_size", 10))), 100)
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            if keyword:
                where_sql = (" WHERE tenant_id = %s AND (title ILIKE %s OR content ILIKE %s) ")
                conds = (tenant_id, f"%{keyword}%", f"%{keyword}%")
            else:
                where_sql = " WHERE tenant_id = %s "
                conds = (tenant_id,)
            await cur.execute("SELECT count(*) FROM prompts" + where_sql, conds)
            (total,) = await cur.fetchone()
            await cur.execute(
                "SELECT key, title, content, is_active, updated_at FROM prompts" + where_sql +
                " ORDER BY updated_at DESC LIMIT %s OFFSET %s",
                conds + (page_size, (page - 1) * page_size))
            rows = await cur.fetchall()
    return {"code": 0, "data": {
        "list": [
            {"key": r[0], "title": r[1], "content": r[2], "is_active": r[3], "updated_at": r[4].isoformat()}
            for r in rows
        ],
        "total": total, "page": page, "page_size": page_size,
    }}


@router.get("/agent/prompts/active")
async def prompts_active(request: Request):
    tenant_id = _tenant_scope(request)
    async with _pool_conn(request.app) as conn:
        content = await load_active_prompt(conn, tenant_id)
    return {"code": 0, "data": {"content": content}}


@router.post("/agent/prompts")
async def prompts_upsert(req: PromptReq, request: Request):
    tenant_id = _tenant_scope(request)
    try:
        async with _pool_conn(request.app) as conn:
            await upsert_prompt(conn, tenant_id, req.key.strip(), req.title.strip() or req.key.strip(),
                                req.content, req.is_active)
        if req.is_active:
            if tenant_id:
                # 租户级：立即重建该租户 Agent（下次请求即用新 Prompt）
                await rebuild_tenant_agent(request.app, tenant_id)
            else:
                # 平台级：无『平台级 Agent』；仅使无自有 Prompt 的租户 Agent 失效（懒重建读新平台 Prompt）
                await invalidate_platform_prompt_agents(request.app)
            # 广播到其他 Agent 实例（多实例下 Prompt 变更即时生效；本实例已在上方处理）
            redis_client = getattr(request.app.state, "redis_client", None)
            if redis_client is not None:
                try:
                    scope = "all" if tenant_id else "prompt"
                    await redis_client.publish(
                        INVALIDATE_CHANNEL,
                        json.dumps({"tenant_id": tenant_id, "scope": scope}),
                    )
                    logger.info("📢 prompt 变更广播已发送: tenant_id=%r scope=%s", tenant_id, scope)
                except Exception as e:
                    logger.warning("prompt 变更广播失败: %s", e)
        return {"code": 0, "msg": "ok"}
    except Exception as e:
        logger.exception("prompts upsert error")
        return {"code": 500, "msg": str(e)}


@router.post("/agent/prompts/{key}/activate")
async def prompts_activate(key: str, request: Request):
    tenant_id = _tenant_scope(request)
    try:
        async with _pool_conn(request.app) as conn:
            content = await activate_prompt(conn, tenant_id, key)
        if content is None:
            return {"code": 404, "msg": "prompt 不存在"}
        await rebuild_tenant_agent(request.app, tenant_id)
        return {"code": 0, "data": {"active_key": key, "prompt_len": len(content)}}
    except Exception as e:
        logger.exception("prompts activate error")
        return {"code": 500, "msg": str(e)}


@router.delete("/agent/prompts/{key}")
async def prompts_delete(key: str, request: Request):
    tenant_id = _tenant_scope(request)
    try:
        async with _pool_conn(request.app) as conn:
            was_active = await delete_prompt(conn, tenant_id, key)
        if was_active:
            await rebuild_tenant_agent(request.app, tenant_id)
        return {"code": 0, "msg": "deleted", "data": {"rebuilt": was_active}}
    except Exception as e:
        logger.exception("prompts delete error")
        return {"code": 500, "msg": str(e)}


# ====================== Prompt 版本管理 ======================
@router.post("/agent/prompts/{key}/versions")
async def prompt_version_create(key: str, request: Request):
    """创建 Prompt 版本快照（保存当前 Prompt 内容到版本表）"""
    tenant_id = _tenant_scope(request)
    try:
        body = await request.json()
        remark = body.get("remark", "")
        # 读取当前 Prompt 内容
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "SELECT content FROM prompts WHERE key = %s AND tenant_id = %s",
                    (key, tenant_id))
                row = await cur.fetchone()
                if not row:
                    return {"code": 404, "msg": "Prompt 不存在"}
                content = row[0]
                # 获取当前最大版本号
                await cur.execute(
                    "SELECT COALESCE(MAX(version), 0) FROM prompt_versions WHERE prompt_key = %s AND tenant_id = %s",
                    (key, tenant_id))
                max_ver = (await cur.fetchone())[0]
                new_version = max_ver + 1
                # 插入版本记录
                await cur.execute(
                    "INSERT INTO prompt_versions (prompt_key, tenant_id, version, content, remark, created_by) "
                    "VALUES (%s, %s, %s, %s, %s, %s)",
                    (key, tenant_id, new_version, content, remark, "admin"))
        logger.info("Prompt version created tenant=%s key=%s version=%d", tenant_id, key, new_version)
        return {"code": 0, "data": {"prompt_key": key, "version": new_version}}
    except Exception as e:
        logger.exception("prompt version create error")
        return {"code": 500, "msg": str(e)}


@router.get("/agent/prompts/{key}/versions")
async def prompt_version_list(key: str, request: Request):
    """查询 Prompt 版本列表（按版本号倒序）"""
    tenant_id = _tenant_scope(request)
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "SELECT id, prompt_key, version, content, remark, created_by, created_at "
                "FROM prompt_versions WHERE prompt_key = %s AND tenant_id = %s ORDER BY version DESC",
                (key, tenant_id))
            rows = await cur.fetchall()
    return {"code": 0, "data": [
        {"id": r[0], "prompt_key": r[1], "version": r[2], "content": r[3],
         "remark": r[4], "created_by": r[5], "created_at": r[6].isoformat() if r[6] else None}
        for r in rows
    ]}


@router.post("/agent/prompts/{key}/versions/{version_id}/rollback")
async def prompt_version_rollback(key: str, version_id: int, request: Request):
    """回滚 Prompt 到指定版本"""
    tenant_id = _tenant_scope(request)
    try:
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                # 读取指定版本内容
                await cur.execute(
                    "SELECT content, version FROM prompt_versions WHERE id = %s AND prompt_key = %s AND tenant_id = %s",
                    (version_id, key, tenant_id))
                row = await cur.fetchone()
                if not row:
                    return {"code": 404, "msg": "版本不存在"}
                content, version = row[0], row[1]
                # 更新当前 Prompt
                await cur.execute(
                    "UPDATE prompts SET content = %s, updated_at = NOW() WHERE key = %s AND tenant_id = %s",
                    (content, key, tenant_id))
                if cur.rowcount == 0:
                    return {"code": 404, "msg": "Prompt 不存在"}
        # 重建 Agent 使 Prompt 生效
        await rebuild_tenant_agent(request.app, tenant_id)
        logger.info("Prompt rollback tenant=%s key=%s version=%d", tenant_id, key, version)
        return {"code": 0, "data": {"prompt_key": key, "rollback_to_version": version}}
    except Exception as e:
        logger.exception("prompt version rollback error")
        return {"code": 500, "msg": str(e)}
