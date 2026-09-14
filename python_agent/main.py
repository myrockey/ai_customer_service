import os
# 强制utf‑8，解决httpx2 header ascii编码报错
os.environ["PYTHONIOENCODING"] = "utf-8"
os.environ["LANG"] = "en_US.UTF-8"
os.environ["LC_ALL"] = "en_US.UTF-8"

from dotenv import load_dotenv
load_dotenv("../.env")

import asyncio
import logging
import uuid
from contextlib import asynccontextmanager
from datetime import datetime, timedelta, timezone
from typing import Optional

import httpx
import psycopg
from psycopg_pool import AsyncConnectionPool
from fastapi import FastAPI, HTTPException, Request, UploadFile, File, Form
from pydantic import BaseModel, Field
from qdrant_client import QdrantClient
from qdrant_client.http import models as qmodels
from qdrant_client.http.models import Distance, VectorParams

from langchain.tools import tool
from langchain.agents import create_agent
from langchain.agents.middleware import before_model, after_model
from langchain.chat_models import init_chat_model
from langchain_core.messages import HumanMessage, AIMessage
import openai
from langchain_openai import OpenAIEmbeddings
from langchain_qdrant import QdrantVectorStore

# 初始化结构化日志
from logger import init_logger, get_logger, get_logger_from_headers
init_logger(level="INFO", output="json")
log = get_logger()
from langchain_text_splitters import RecursiveCharacterTextSplitter
# ✅使用异步Checkpointer
from langgraph.checkpoint.postgres.aio import AsyncPostgresSaver
from checkpoint_safe import SafeAsyncPostgresSaver
from langgraph.types import interrupt, Command

from contextvars import ContextVar

# P2-5：拆分模块导入
from db_ops import (tenant_from_thread, collection_name, verify_agent_tables, touch_session,
                    record_token_usage, check_concurrent_sessions, load_active_prompt,
                    seed_default_prompt, cleanup_sessions, session_cleanup_loop)
from agent_factory import current_model_name, current_model_source, current_chat_prices, get_tenant_agent, get_platform_model, get_platform_embeddings

# 协程上下文，每个请求独立trace_id
trace_id_var: ContextVar[str] = ContextVar("trace_id", default="-")
# 协程上下文：当前请求所属租户（多租户隔离）
tenant_var: ContextVar[str] = ContextVar("tenant_id", default="")
# 协程上下文：线程存在待处理工单（未接管）——agent 不再重复触发转人工中断，正常回答
pending_ticket_var: ContextVar[bool] = ContextVar("pending_ticket", default=False)


async def _record_usage_after_chat(request, conn, tenant_id: str, thread_id: str, messages):
    """对话完成后统一记录 token 用量：模型名/来源/单价一次获取（共享 300s 配置缓存），
    费用按发生时单价快照（价格调整不影响历史账单）"""
    in_cache, in_miss, out_price = await current_chat_prices(request.app, tenant_id)
    await record_token_usage(conn, tenant_id, thread_id, messages,
                             fallback_model=await current_model_name(request.app, tenant_id),
                             source=await current_model_source(request.app, tenant_id),
                             chat_input_cache_price=in_cache, chat_input_price=in_miss,
                             chat_output_price=out_price)


class TraceIdStreamHandler(logging.StreamHandler):
    def format(self, record):
        base_msg = super().format(record)
        try:
            tid = trace_id_var.get()
        except LookupError:
            tid = "-"
        return f"{tid} | {base_msg}"


# 清理全部原有handler，包括uvicorn相关
root_logger = logging.getLogger()
for h in root_logger.handlers[:]:
    root_logger.removeHandler(h)

for logger_name in ("uvicorn", "uvicorn.access", "uvicorn.error"):
    lib_logger = logging.getLogger(logger_name)
    for h in lib_logger.handlers[:]:
        lib_logger.removeHandler(h)

root_logger.setLevel(logging.INFO)
formatter = logging.Formatter("%(asctime)s | %(name)s | %(levelname)s | %(message)s")
console_handler = TraceIdStreamHandler()
console_handler.setFormatter(formatter)
root_logger.addHandler(console_handler)

logger = logging.getLogger("agent")


# ====================== 常量&环境变量配置（已拆分到 config.py） ======================
from config import *


# ====================== Tools（已拆分到 tools.py，采用 app 注入模式） ======================
import tools



# ====================== FastAPI Lifespan 启动/销毁 ======================
@asynccontextmanager
async def lifespan(app: FastAPI):
    # 初始化state占位
    app.state.db_pool: Optional[AsyncConnectionPool] = None
    app.state.checkpointer_conn: Optional[psycopg.AsyncConnection] = None
    app.state.checkpointer: Optional[AsyncPostgresSaver] = None
    app.state.qdrant_client: Optional[QdrantClient] = None
    app.state.embeddings = None
    app.state.vec_dim = 0
    app.state.text_splitter = None
    app.state.tenant_stores = {}
    app.state.tenant_retrievers = {}
    app.state.tenant_agents = {}
    app.state.agent = None
    app.state.model = None
    app.state.tools = None
    app.state.cleanup_task: Optional[asyncio.Task] = None
    app.state.http_client: Optional[httpx.AsyncClient] = None

    logger.info("==== Agent Service starting ====")
    try:
        # 1. 初始化异步Postgres连接池（业务查询并发安全）+ AsyncPostgresSaver（会话checkpoint独立连接）
        app.state.db_pool = AsyncConnectionPool(
            POSTGRES_DSN, min_size=2, max_size=10, open=True,
            kwargs={"autocommit": True},
        )
        app.state.checkpointer_conn = await psycopg.AsyncConnection.connect(POSTGRES_DSN, autocommit=True)
        # 并发安全包装：AsyncPostgresSaver 单连接不支持并发 aput，多租户并发聊天会
        # 触发 "another command is already in progress"（warning/偶发接口报错），加锁串行化
        app.state.checkpointer = SafeAsyncPostgresSaver(app.state.checkpointer_conn)
        await app.state.checkpointer.setup()
        logger.info("✅ AsyncPostgresSaver checkpointer setup done (concurrency-safe wrapper)")

        # 扩展表：会话元数据 / Prompt / 知识库文档（含租户维度）
        async with app.state.db_pool.connection() as conn:
            await verify_agent_tables(conn)
            await seed_default_prompt(conn)
            global_prompt = await load_active_prompt(conn, "")
        logger.info("✅ global prompt loaded (len=%d)", len(global_prompt))

        # 2. 异步 http 客户端（必须先于 embedding/model 初始化：get_tenant_model_config 依赖它）
        app.state.http_client = httpx.AsyncClient(timeout=10.0)

        # 3. Qdrant 基础：客户端 + embeddings（集合按租户懒创建）
        app.state.qdrant_client = QdrantClient(url=QDRANT_URL)

        # 平台默认 Embedding 模型（DB 单一数据源，后台「模型提供商/租户模型配置」维护；
        # 模型不可用时降级启动，知识库上传/检索时再重试）
        try:
            app.state.embeddings = await get_platform_embeddings(app)
        except Exception as e:
            app.state.embeddings = None
            logger.warning("⚠️ 平台默认 Embedding 模型不可用（服务降级启动，请检查后台模型配置）: %s", e)
        # 探测 embedding 维度；模型不可用（如额度耗尽）时降级启动，不阻塞服务
        # 后续向量模型恢复后，Qdrant 集合创建/上传会自动重试
        try:
            test_embedding = await app.state.embeddings.aembed_query("test")
            app.state.vec_dim = len(test_embedding)
            logger.info("✅ embedding dim=%d", app.state.vec_dim)
        except Exception as e:
            app.state.vec_dim = 0
            logger.warning("⚠️ embedding 探测失败，知识库/向量检索暂不可用（服务降级启动）: %s", e)

        app.state.text_splitter = RecursiveCharacterTextSplitter(
            chunk_size=TEXT_SPLITTER_CHUNK_SIZE,
            chunk_overlap=TEXT_SPLITTER_CHUNK_OVERLAP
        )

        # 4. 平台默认对话模型（DB 单一数据源；不可用时降级启动，请求时懒构建重试）
        try:
            app.state.model = await get_platform_model(app)
        except Exception as e:
            app.state.model = None
            logger.warning("⚠️ 平台默认对话模型不可用（服务降级启动，请检查后台模型配置）: %s", e)
        app.state.tools = [tools.search_kb, tools.query_order, tools.transfer_to_human]

        # 6. 启动后台会话回收任务
        app.state.cleanup_task = asyncio.create_task(session_cleanup_loop(app))
        logger.info("✅ session cleanup task started (retention=%dd, interval=%ds)",
                    SESSION_RETENTION_DAYS, SESSION_CLEANUP_INTERVAL)

        # 7. Redis：缓存失效广播订阅（多实例下模型切换全量即时生效）——强制依赖，失败即启动失败
        import redis_pubsub
        app.state.redis_client = redis_pubsub.make_redis_client()
        # 连接检查：Ping 失败 → 抛错，启动失败（fail-fast）
        await app.state.redis_client.ping()
        app.state.redis_task = asyncio.create_task(
            redis_pubsub.invalidate_listener(app, app.state.redis_client))
        logger.info("✅ Redis 缓存失效广播订阅任务已启动")

        # 8. 知识库异步索引 worker（Redis 队列消费）：上传/重索引不阻塞请求
        import kb_index_worker
        # 启动兜底：扫描 status=0 遗留记录重新入队（进程崩溃/重启恢复）
        await kb_index_worker.requeue_pending(app)
        n_workers = max(1, int(os.getenv("KB_INDEX_WORKERS", "2")))
        app.state.kb_workers = []
        for i in range(n_workers):
            t = asyncio.create_task(kb_index_worker.index_worker_loop(app, i))
            app.state.kb_workers.append(t)
        logger.info("✅ KB index workers started (%d)", n_workers)

    except Exception as e:
        logger.exception(f"❌ 服务启动初始化失败:{str(e)}")
        raise

    yield

    # ============ 服务关闭：释放全部资源 ============
    logger.info("==== Agent Service shutdown, release resources ====")
    if app.state.cleanup_task is not None:
        app.state.cleanup_task.cancel()
        try:
            await app.state.cleanup_task
        except asyncio.CancelledError:
            pass
    if getattr(app.state, "redis_task", None) is not None:
        app.state.redis_task.cancel()
        try:
            await app.state.redis_task
        except asyncio.CancelledError:
            pass
    # 停止知识库索引 worker
    for t in getattr(app.state, "kb_workers", []) or []:
        t.cancel()
    for t in getattr(app.state, "kb_workers", []) or []:
        try:
            await t
        except asyncio.CancelledError:
            pass
    if getattr(app.state, "redis_client", None) is not None:
        await app.state.redis_client.aclose()
    if app.state.http_client is not None:
        await app.state.http_client.aclose()
    if app.state.qdrant_client is not None:
        app.state.qdrant_client.close()
    if app.state.checkpointer_conn is not None:
        await app.state.checkpointer_conn.close()
    if app.state.db_pool is not None:
        await app.state.db_pool.close()


# ====================== 数据库连接池快捷方式 ======================
from db_pool import _pool_conn


# ====================== P2-2：知识库检索缓存（已拆分到 cache.py） ======================
from cache import _kb_cache_key, _kb_cache_get, _kb_cache_set, _kb_cache_cleanup, KB_CACHE_TTL


app = FastAPI(title="CustomerServiceAgent Internal Service", lifespan=lifespan)
# ====================== P2-5：拆分模块注册 ======================
from routers_prompt import router as prompt_router
from routers_kb import router as kb_router
from routers_cache import router as cache_router

app.include_router(prompt_router)
app.include_router(kb_router)
app.include_router(cache_router)



# P4-3：注入 tools 模块的全局依赖（app/logger/tenant_var/pending_ticket_var）
tools.set_app(app, logger, tenant_var, pending_ticket_var)


# ====================== Pydantic Schema（已拆分到 models.py） ======================
from models import *


# ====================== API接口 ======================
def _friendly_llm_error(e: Exception) -> str:
    """LLM 调用异常 → 用户可读错误提示（生产不暴露内部细节/密钥）"""
    if PROD_MODE:
        if isinstance(e, openai.RateLimitError):
            return "模型服务当前繁忙（限流），请稍后重试"
        if isinstance(e, openai.APITimeoutError):
            return "模型响应超时，请稍后重试"
        if isinstance(e, openai.APIConnectionError):
            return "模型服务连接失败，请稍后重试"
        return "agent服务内部异常"
    return f"agent error:{str(e)}"


@app.post("/agent/chat", response_model=AgentResp)
async def agent_chat(req: ChatRequest, request: Request):
    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    thread_id = req.thread_id.strip()
    user_message = req.user_message.strip()
    if not thread_id or not user_message:
        return AgentResp(
            success=False,
            is_interrupt=False,
            content="thread_id 和 user_message 不能为空"
        )

    tenant_id = tenant_from_thread(thread_id)
    tenant_var.set(tenant_id)
    pending_ticket_var.set(req.has_pending_ticket)
    agent = await get_tenant_agent(request.app, tenant_id)

    config = {"configurable": {"thread_id": thread_id}}
    try:
        async with _pool_conn(request.app) as conn:
            await touch_session(conn, thread_id)
        result = await agent.ainvoke(
            {"messages": [HumanMessage(content=user_message)]},
            config=config
        )
        messages = result.get("messages", [])
        if not messages:
            return AgentResp(success=False, is_interrupt=False, content="Agent执行完成，但未返回消息")

        state = await agent.aget_state(config)
        if state.tasks and state.tasks[0].interrupts:
            interrupt_info = state.tasks[0].interrupts[0].value
            return AgentResp(
                success=True,
                is_interrupt=True,
                interrupt_data=interrupt_info,
                content=None
            )
        last_msg = messages[-1]
        content = last_msg.content if hasattr(last_msg, "content") else ""
        async with _pool_conn(request.app) as conn:
            await _record_usage_after_chat(request, conn, tenant_id, thread_id, messages)
        return AgentResp(
            success=True,
            is_interrupt=False,
            content=content
        )
    except Exception as e:
        logger.exception("/agent/chat error")
        err_msg = _friendly_llm_error(e)
        return AgentResp(success=False, is_interrupt=False, content=err_msg)


@app.post("/agent/chat/stream")
async def agent_chat_stream(req: ChatRequest, request: Request):
    """P2-1：Agent 对话流式输出（SSE）
    使用 LangGraph astream(stream_mode="messages") 逐 token 推送 LLM 回复
    SSE 事件格式：
      data: {"type":"chunk","content":"token片段"}\n\n
      data: {"type":"end","content":"完整内容"}\n\n
      data: {"type":"error","msg":"错误信息"}\n\n
      data: {"type":"interrupt","data":中断数据}\n\n
    """
    import json as _json
    from fastapi.responses import StreamingResponse

    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    thread_id = req.thread_id.strip()
    user_message = req.user_message.strip()

    async def event_generator():
        if not thread_id or not user_message:
            yield f"data: {_json.dumps({'type':'error','msg':'thread_id 和 user_message 不能为空'}, ensure_ascii=False)}\n\n"
            return

        tenant_id = tenant_from_thread(thread_id)
        tenant_var.set(tenant_id)
        pending_ticket_var.set(req.has_pending_ticket)

        # P1-2：并发活跃会话配额校验
        async with _pool_conn(request.app) as conn:
            quota_err = await check_concurrent_sessions(conn, request.app, tenant_id)
        if quota_err:
            yield f"data: {_json.dumps({'type':'error','msg':quota_err['msg']}, ensure_ascii=False)}\n\n"
            return

        try:
            agent = await get_tenant_agent(request.app, tenant_id)
            config = {"configurable": {"thread_id": thread_id}}
            async with _pool_conn(request.app) as conn:
                await touch_session(conn, thread_id)

            full_content = ""
            is_interrupt = False
            interrupt_data = None
            stream_msgs: list = []

            # 使用 LangGraph astream(stream_mode="messages") 逐 token 输出
            async for msg, metadata in agent.astream(
                {"messages": [HumanMessage(content=user_message)]},
                config=config,
                stream_mode="messages"
            ):
                stream_msgs.append(msg)
                # 只推送 AIMessage 的内容（过滤 HumanMessage 和系统消息）
                if hasattr(msg, "content") and msg.content and not isinstance(msg, HumanMessage):
                    chunk = msg.content if isinstance(msg.content, str) else str(msg.content)
                    if chunk:
                        full_content += chunk
                        yield f"data: {_json.dumps({'type':'chunk','content':chunk}, ensure_ascii=False)}\n\n"

            # 检查是否有中断（人工接管）
            try:
                state = await agent.aget_state(config)
                if state.tasks and state.tasks[0].interrupts:
                    is_interrupt = True
                    interrupt_data = state.tasks[0].interrupts[0].value
            except Exception:
                pass

            if is_interrupt:
                yield f"data: {_json.dumps({'type':'interrupt','data':interrupt_data}, ensure_ascii=False)}\n\n"
            else:
                yield f"data: {_json.dumps({'type':'end','content':full_content}, ensure_ascii=False)}\n\n"
            # P1-1：记录 token 用量（从流式消息中提取 usage_metadata）
            async with _pool_conn(request.app) as conn:
                await _record_usage_after_chat(request, conn, tenant_id, thread_id, stream_msgs)

        except Exception as e:
            logger.exception("/agent/chat/stream error")
            err_msg = _friendly_llm_error(e)
            yield f"data: {_json.dumps({'type':'error','msg':err_msg}, ensure_ascii=False)}\n\n"

    return StreamingResponse(event_generator(), media_type="text/event-stream")


@app.post("/agent/resume", response_model=AgentResp)
async def agent_resume(req: ResumeRequest, request: Request):
    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    thread_id = req.thread_id.strip()
    tenant_id = tenant_from_thread(thread_id)
    tenant_var.set(tenant_id)

    # P1-2：并发活跃会话配额校验
    async with _pool_conn(request.app) as conn:
        quota_err = await check_concurrent_sessions(conn, request.app, tenant_id)
    if quota_err:
        return AgentResp(success=False, is_interrupt=False, content=quota_err["msg"])

    agent = await get_tenant_agent(request.app, tenant_id)

    config = {"configurable": {"thread_id": thread_id}}
    try:
        async with _pool_conn(request.app) as conn:
            await touch_session(conn, thread_id)
        result = await agent.ainvoke(
            Command(resume={
                "confirmed": req.confirmed,
                "wait_time": req.wait_time,
                "ticket_id": req.ticket_id,
            }),
            config=config
        )
        messages = result.get("messages", [])
        if not messages:
            return AgentResp(success=False, is_interrupt=False, content="resume执行完成无返回消息")
        last_msg = messages[-1]
        content = last_msg.content if hasattr(last_msg, "content") else ""
        async with _pool_conn(request.app) as conn:
            await _record_usage_after_chat(request, conn, tenant_id, thread_id, messages)
        return AgentResp(
            success=True,
            is_interrupt=False,
            content=content
        )
    except Exception as e:
        logger.exception("/agent/resume error")
        err_msg = "resume服务内部异常" if PROD_MODE else f"resume error:{str(e)}"
        return AgentResp(success=False, is_interrupt=False, content=err_msg)


# 调试接口，查询checkpoint数据表（异步版本）
@app.get("/agent/checkpoint/{thread_id}")
async def get_checkpoint(thread_id: str, request: Request):
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            await cur.execute("""
                SELECT thread_id, checkpoint_ns, checkpoint_id, parent_checkpoint_id, metadata
                FROM checkpoints
                WHERE thread_id = %s AND checkpoint_ns = '';
            """, (thread_id,))
            rows = await cur.fetchall()
            result = []
            for row in rows:
                result.append({
                    "thread_id": row[0],
                    "checkpoint_ns": row[1],
                    "checkpoint_id": row[2],
                    "parent_checkpoint_id": row[3],
                    "metadata": row[4]
                })
        return {"data": result}


# ====================== 租户预热（创建/启用后调用，避免首次对话长时间初始化） ======================
@app.post("/agent/tenants/prewarm")
async def tenant_prewarm(req: TenantWarmReq, request: Request):
    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    try:
        await get_tenant_agent(request.app, req.tenant_id)
        return {"code": 0, "data": {"tenant_id": req.tenant_id, "status": "ready"}}
    except Exception as e:
        # 降级策略：对话模型或向量模型任一不可用时不阻塞预热（服务保持可用），
        # 对话/索引时再由对应环节明确报错。Go 侧仅记录 warning。
        logger.warning("tenant prewarm degraded [%s]: %s", req.tenant_id, str(e)[:200])
        return {"code": 0, "data": {"tenant_id": req.tenant_id, "status": "degraded", "reason": str(e)[:200]}}


@app.delete("/agent/tenants/{tenant_id}")
async def tenant_cleanup(tenant_id: str, request: Request):
    """清理指定租户的全部业务数据：PG prompts/kb_documents + Qdrant 集合 + 内存缓存"""
    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    try:
        result = {"prompts_deleted": 0, "kb_docs_deleted": 0, "qdrant_collection": ""}
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute("DELETE FROM prompts WHERE tenant_id = %s", (tenant_id,))
                result["prompts_deleted"] = cur.rowcount
            async with conn.cursor() as cur:
                await cur.execute("DELETE FROM kb_documents WHERE tenant_id = %s", (tenant_id,))
                result["kb_docs_deleted"] = cur.rowcount
        # 删除 Qdrant 集合
        col = collection_name(tenant_id)
        try:
            request.app.state.qdrant_client.delete_collection(collection_name=col)
            result["qdrant_collection"] = col
        except Exception as qe:
            logger.warning("delete qdrant collection %s failed: %s", col, qe)
        # 清理内存缓存
        request.app.state.tenant_stores.pop(tenant_id, None)
        request.app.state.tenant_retrievers.pop(tenant_id, None)
        request.app.state.tenant_agents.pop(tenant_id, None)
        logger.info("tenant %s cleanup done: %s", tenant_id, result)
        return {"code": 0, "data": result}
    except Exception as e:
        logger.exception("tenant cleanup error")
        return {"code": 500, "msg": str(e)}


# ====================== 会话清理 / 统计 ======================
@app.post("/agent/session/cleanup")
async def session_cleanup(req: SessionCleanupReq, request: Request):
    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    try:
        async with _pool_conn(request.app) as conn:
            result = await cleanup_sessions(conn, request.app.state.checkpointer, req.days)
        logger.info("manual session cleanup: %s", result)
        return {"code": 0, "data": result}
    except Exception as e:
        logger.exception("/agent/session/cleanup error")
        return {"code": 500, "msg": str(e)}


@app.get("/agent/session/stats")
async def session_stats(request: Request):
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            await cur.execute("""
                SELECT count(*), COALESCE(min(last_active_at), now()), COALESCE(max(last_active_at), now()),
                       COALESCE(sum(message_count), 0)
                FROM session_meta
            """)
            row = await cur.fetchone()
            await cur.execute("SELECT count(*) FROM checkpoints")
            cp = await cur.fetchone()
            await cur.execute(
                "SELECT count(*) FROM session_meta WHERE last_active_at < now() - make_interval(days => %s)",
                (SESSION_RETENTION_DAYS,))
            expired = await cur.fetchone()
    return {"code": 0, "data": {
        "sessions": row[0],
        "oldest_active": row[1].isoformat(),
        "newest_active": row[2].isoformat(),
        "total_messages": int(row[3]),
        "checkpoint_rows": int(cp[0]),
        "retention_days": SESSION_RETENTION_DAYS,
        "cleanup_interval": SESSION_CLEANUP_INTERVAL,
        "expired_sessions": int(expired[0]),
    }}


@app.get("/agent/session/cleanup-count")
async def session_cleanup_count(request: Request):
    """查询指定天数内可清理的过期会话数（前端按钮旁展示预计数量）"""
    try:
        days = int(request.query_params.get("days", "30"))
        if days <= 0 or days > 3650:
            days = 30
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "SELECT count(*) FROM session_meta WHERE last_active_at < now() - make_interval(days => %s)",
                    (days,))
                row = await cur.fetchone()
        return {"code": 0, "data": {"count": int(row[0]), "days": days}}
    except Exception as e:
        logger.exception("/agent/session/cleanup-count error")
        return {"code": 500, "msg": str(e)}


@app.get("/agent/tenant-usage")
async def tenant_usage_report(request: Request):
    """批量返回各租户用量（与配额校验口径一致）：
      active_sessions  最近 30 分钟活跃会话数（并发）
      kb_docs          知识库文档数
      kb_size_bytes    知识库占用字节数
    返回 {tenant_id: {active_sessions, kb_docs, kb_size_bytes}}
    """
    try:
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "SELECT tenant_id, count(*) FROM session_meta "
                    "WHERE last_active_at > now() - interval '30 minutes' "
                    "GROUP BY tenant_id")
                sessions = {r[0]: int(r[1]) for r in await cur.fetchall()}
                await cur.execute(
                    "SELECT tenant_id, count(*), COALESCE(SUM(file_size), 0) "
                    "FROM kb_documents GROUP BY tenant_id")
                kb = {r[0]: (int(r[1]), int(r[2])) for r in await cur.fetchall()}
        data = {}
        for tid in set(sessions) | set(kb):
            s = sessions.get(tid, 0)
            k = kb.get(tid, (0, 0))
            data[tid] = {"active_sessions": s, "kb_docs": k[0], "kb_size_bytes": k[1]}
        return {"code": 0, "data": data}
    except Exception as e:
        logger.exception("/agent/tenant-usage error")
        return {"code": 500, "msg": str(e)}


# ====================== Token 用量统计（P1-1） ======================
@app.get("/agent/token-usage")
async def token_usage_report(request: Request):
    """Token 用量聚合报表
    query 参数：
      days      int    默认 30，统计近 N 天
      group_by  str    默认 day；day=按天聚合, tenant=按租户聚合, model=按模型聚合
      tenant_id str    可选，指定租户（租户管理端注入自身 tenant_id）
      model     str    可选，按模型名筛选
      source    str    可选，platform=平台模型 / custom=租户自定义（平台模型专项统计用）
    """
    trace_id_var.set(request.headers.get("X-Trace-Id", "-"))
    try:
        days = int(request.query_params.get("days", "30"))
        if days <= 0 or days > 365:
            days = 30
        group_by = request.query_params.get("group_by", "day")
        if group_by not in ("day", "tenant", "model"):
            group_by = "day"
        tenant_id = request.query_params.get("tenant_id", "") or None
        model_name = request.query_params.get("model", "") or None
        source = request.query_params.get("source", "") or None

        where = "created_at >= now() - make_interval(days => %s)"
        params: list = [days]
        if tenant_id:
            where += " AND tenant_id = %s"
            params.append(tenant_id)
        if model_name:
            where += " AND model = %s"
            params.append(model_name)
        if source:
            where += " AND source = %s"
            params.append(source)

        if group_by == "tenant":
            cols = "tenant_id AS dim, date_trunc('day', created_at) AS d"
        elif group_by == "model":
            cols = "model AS dim, NULL::timestamptz AS d"
        else:
            cols = "'-' AS dim, date_trunc('day', created_at) AS d"

        sql = f"""
            SELECT {cols},
                   COALESCE(SUM(input_tokens), 0)  AS input_tokens,
                   COALESCE(SUM(output_tokens), 0) AS output_tokens,
                   COALESCE(SUM(total_tokens), 0)  AS total_tokens,
                   COUNT(*)                         AS calls,
                   COALESCE(SUM(cost), 0)           AS cost
            FROM token_usage
            WHERE {where}
            GROUP BY dim, d ORDER BY d DESC NULLS LAST, dim
        """
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(sql, params)
                rows = await cur.fetchall()

        data = []
        for r in rows:
            data.append({
                "dim": r[0],
                "date": r[1].isoformat() if r[1] else None,
                "input_tokens": int(r[2]),
                "output_tokens": int(r[3]),
                "total_tokens": int(r[4]),
                "calls": int(r[5]),
                "cost": float(r[6]) if r[6] else 0.0,
            })

        # 汇总行
        totals_sql = f"""
            SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
                   COALESCE(SUM(total_tokens),0), COUNT(*), COALESCE(SUM(cost),0)
            FROM token_usage WHERE {where}
        """
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(totals_sql, params)
                t = await cur.fetchone()

        return {"code": 0, "data": {
            "days": days,
            "group_by": group_by,
            "rows": data,
            "summary": {
                "input_tokens": int(t[0]),
                "output_tokens": int(t[1]),
                "total_tokens": int(t[2]),
                "calls": int(t[3]),
                "total_cost": float(t[4]) if t[4] else 0.0,
            },
        }}
    except Exception as e:
        logger.exception("/agent/token-usage error")
        return {"code": 500, "msg": str(e)}


# ====================== 平台配置缓存清除（Go 后端调用） ======================
@app.post("/agent/platform/cache-invalidate")
async def platform_cache_invalidate(request: Request):
    """清除平台默认模型配置缓存（平台管理员修改配置后调用）"""
    try:
        # 清除平台配置缓存
        _tenant_model_cache.pop("_platform_config", None)
        # 清除所有以 _platform_ 开头的缓存（平台模型/embeddings 实例）
        keys_to_remove = [k for k in _tenant_model_cache.keys() if k.startswith("_platform_")]
        for k in keys_to_remove:
            _tenant_model_cache.pop(k, None)
        logger.info("✅ 平台默认模型配置缓存已清除，清除 %d 个缓存项", len(keys_to_remove) + 1)
        return {"code": 0, "msg": "缓存已清除", "cleared": len(keys_to_remove) + 1}
    except Exception as e:
        logger.error("❌ 清除平台配置缓存失败: %s", str(e))
        return {"code": 500, "msg": str(e)}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
