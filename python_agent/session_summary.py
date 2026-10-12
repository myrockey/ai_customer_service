# -*- coding: utf-8 -*-
"""
会话自动摘要后台任务（🚀②）：
- 定时（SESSION_SUMMARY_INTERVAL，默认 600s）调用 go 内部接口拉取"待生成摘要的候选会话"
- 每个会话取最近 40 条消息 → 租户模型生成 ≤80 字中文摘要 → 回写 go 落库
- 多实例互斥：Redis 分布式锁；单会话失败跳过下轮重试，不影响服务
"""
import asyncio
import logging
import os

from config import GO_BUSINESS_API, INTERNAL_API_TOKEN
from agent_factory import get_tenant_model

logger = logging.getLogger("agent.session_summary")

SUMMARY_PROMPT = """请用不超过80字的中文，概括以下智能客服会话的主要内容。要求：
1. 说清客户的核心诉求、问题是否解决、是否需要人工跟进
2. 只输出摘要本身，不要任何前缀、标题或解释
3. 不要出现"摘要："之类的引导语

会话记录：
{history}"""

MAX_CANDIDATES = int(os.getenv("SESSION_SUMMARY_MAX_PER_ROUND", "20"))
HISTORY_LIMIT = 40
MESSAGE_INTERVAL = int(os.getenv("SESSION_SUMMARY_INTERVAL", "600"))

_LOCK_KEY = "cs:lock:session_summary"
_LOCK_TTL = 300


async def _acquire_lock(app) -> bool:
    rc = getattr(getattr(app, "state", None), "redis_client", None)
    if rc is None:
        return False
    try:
        ok = await rc.set(_LOCK_KEY, "1", nx=True, ex=_LOCK_TTL)
        return bool(ok)
    except Exception as e:
        logger.warning("summary lock acquire failed: %s", e)
        return False


async def _release_lock(app) -> None:
    rc = getattr(getattr(app, "state", None), "redis_client", None)
    if rc is None:
        return
    try:
        await rc.delete(_LOCK_KEY)
    except Exception:
        pass


async def _internal_headers() -> dict:
    h = {"Content-Type": "application/json"}
    if INTERNAL_API_TOKEN:
        h["X-Internal-Token"] = INTERNAL_API_TOKEN
    return h


def _format_history(msgs: list) -> str:
    lines = []
    for m in msgs:
        role = m.get("role", "")
        content = (m.get("content") or "").strip().replace("\n", " ")
        if not content:
            continue
        who = "客户" if role in ("user", "human") else "客服"
        lines.append(f"{who}: {content[:200]}")
    return "\n".join(lines)


async def _generate_summary(app, tenant_id: str, history: str) -> str:
    """用租户模型生成摘要（失败抛异常，由调用方跳过该会话）"""
    model = await get_tenant_model(app, tenant_id)
    resp = await model.ainvoke(SUMMARY_PROMPT.format(history=history[:4000]))
    text = (resp.content or "").strip() if hasattr(resp, "content") else str(resp).strip()
    # 去掉可能的前缀/引号，截断 200 字兜底
    for p in ("摘要：", "摘要:", "总结：", "总结:"):
        if text.startswith(p):
            text = text[len(p):].strip()
    text = text.strip("\"'“”")
    return text[:200]


async def _process_one(app, cand: dict) -> bool:
    """处理单个候选会话，成功返回 True"""
    thread_id = cand.get("thread_id", "")
    tenant_id = cand.get("tenant_id", "")
    msgs = cand.get("messages") or []
    if not thread_id or not tenant_id or not msgs:
        return False
    history = _format_history(msgs)
    if not history:
        return False
    try:
        summary = await _generate_summary(app, tenant_id, history)
    except Exception as e:
        logger.warning("summary generate failed tenant=%s thread=%s: %s", tenant_id, thread_id, str(e)[:120])
        return False
    if not summary:
        return False
    try:
        resp = await app.state.http_client.post(
            f"{GO_BUSINESS_API}/api/agent/session-summaries",
            headers=await _internal_headers(),
            json={"thread_id": thread_id, "tenant_id": tenant_id, "summary": summary},
            timeout=10.0,
        )
        data = resp.json()
        if data.get("code") != 0:
            logger.warning("summary save failed thread=%s: %s", thread_id, data.get("msg"))
            return False
        logger.info("✅ 会话摘要已生成 thread=%s tenant=%s summary=%s", thread_id, tenant_id, summary[:40])
        return True
    except Exception as e:
        logger.warning("summary save error thread=%s: %s", thread_id, str(e)[:120])
        return False


async def _run_round(app) -> None:
    """执行一轮：拉候选 → 逐会话生成 → 回写"""
    try:
        resp = await app.state.http_client.get(
            f"{GO_BUSINESS_API}/api/agent/session-summaries/pending",
            params={"limit": MAX_CANDIDATES},
            headers=await _internal_headers(),
            timeout=10.0,
        )
        data = resp.json()
    except Exception as e:
        logger.warning("summary candidates fetch error: %s", str(e)[:120])
        return
    cands = data.get("data") or []
    if not cands:
        return
    done = 0
    for cand in cands:
        try:
            if await _process_one(app, cand):
                done += 1
        except Exception as e:
            logger.warning("summary round item error: %s", str(e)[:120])
    logger.info("🔁 会话摘要本轮处理 %d/%d", done, len(cands))


async def session_summary_loop(app):
    """后台定时任务：会话自动摘要"""
    if MESSAGE_INTERVAL <= 0:
        logger.info("⏸ session summary loop disabled")
        return
    await asyncio.sleep(MESSAGE_INTERVAL)
    while True:
        try:
            if not await _acquire_lock(app):
                logger.info("⏭ summary lock held by another instance, skip")
                await asyncio.sleep(MESSAGE_INTERVAL)
                continue
            try:
                await _run_round(app)
            finally:
                await _release_lock(app)
        except Exception:
            logger.exception("session summary round failed")
        await asyncio.sleep(MESSAGE_INTERVAL)
