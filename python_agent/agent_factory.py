"""P2-5：Agent 工厂与租户模型配置模块（从 main.py 拆分）
内容守卫/自动签名中间件、Agent 构建、Qdrant 向量、租户 BYOK 模型配置
"""
import asyncio
import logging
from typing import Optional

from langchain.agents import create_agent
from langchain.agents.middleware import before_model, after_model
from langchain.chat_models import init_chat_model
from langchain_core.messages import HumanMessage, AIMessage
from langchain_openai import OpenAIEmbeddings
from langchain_qdrant import QdrantVectorStore
from qdrant_client import QdrantClient
from qdrant_client.http import models as qmodels
from qdrant_client.http.models import Distance, VectorParams

from config import *
import tools
from db_ops import load_active_prompt, collection_name

logger = logging.getLogger("agent.factory")


# ====================== Middleware ======================
# ====================== Middleware ======================
@before_model
def content_guard(state, runtime):
    last_msg = state["messages"][-1] if state.get("messages") else None
    if not last_msg:
        return None
    content = str(getattr(last_msg, 'content', ''))
    blocked = ["黄X", "X博", "违法"]
    for word in blocked:
        if word in content:
            return {
                "jump_to": "end",
                "messages": [HumanMessage(content="抱歉，我不能处理这个请求。")]
            }
    return None


@after_model
def auto_signature(state, runtime):
    """已废弃：签名改为 Prompt 管理维护（不再代码追加，避免同消息重复累积）
    保留空实现仅为兼容历史导入，middleware 不再挂载。
    """
    return None

# ====================== Agent 构建（支持 Prompt 热更新重建） ======================
# ====================== Agent 构建（支持 Prompt 热更新重建） ======================
def build_agent(system_prompt: str):
    """用指定 system_prompt 重建默认 Agent（模型/工具/中间件保持不变）"""
    agent = create_agent(
        model=app.state.model,
        tools=app.state.tools,
        middleware=[content_guard],
        checkpointer=app.state.checkpointer,
        system_prompt=system_prompt,
    )
    app.state.agent = agent
    logger.info("✅ Agent rebuilt with system_prompt len=%d", len(system_prompt))


# 向量库初始化失败冷却（秒）：embedding/集合不可用时降级为纯 LLM 对话，冷却期内不反复重试刷日志
_vec_fail_ts: dict = {}
_VEC_FAIL_TTL = 60

async def ensure_tenant_vector(app, tenant_id: str) -> QdrantVectorStore | None:
    """确保租户的 Qdrant collection + vector_store 存在（首次自动灌入默认知识库）。

    降级策略：embedding/向量库任一不可用时返回 None（不抛异常），
    对话降级为纯 LLM（无知识库检索，由 search_kb 工具如实告知）；知识库索引由调用方显式报错。
    """
    if tenant_id in app.state.tenant_stores:
        return app.state.tenant_stores[tenant_id]
    # 冷却期内直接降级，不重复尝试
    import time as _t
    last_fail = _vec_fail_ts.get(tenant_id, 0)
    if last_fail and (_t.time() - last_fail) < _VEC_FAIL_TTL:
        return None
    col = collection_name(tenant_id)
    try:
        tenant_emb = await get_tenant_embeddings(app, tenant_id)
        if not app.state.qdrant_client.collection_exists(col):
            # 维度探测：vec_dim 仅在启动时探测一次；若当时平台 embedding 不可用（vec_dim=0）
            # 或后续更换了向量模型，用当前租户 embedding 实时探测维度，无需重启 Agent。
            vec_dim = app.state.vec_dim
            if vec_dim <= 0:
                probe = await tenant_emb.aembed_query("dim-probe")
                vec_dim = len(probe)
                app.state.vec_dim = vec_dim
                logger.info("✅ vec_dim lazily probed via tenant embedding: dim=%d", vec_dim)
            app.state.qdrant_client.create_collection(
                collection_name=col,
                vectors_config=VectorParams(size=vec_dim, distance=Distance.COSINE),
            )
            logger.info("Qdrant collection [%s] created, dim=%d", col, vec_dim)
        vs = QdrantVectorStore(client=app.state.qdrant_client, collection_name=col,
                           embedding=tenant_emb)
        app.state.tenant_stores[tenant_id] = vs
        _vec_fail_ts.pop(tenant_id, None)
        return vs
    except Exception as e:
        # 降级：向量能力不可用不影响服务启动/对话（纯 LLM 模式），知识库功能由上层明确报错
        _vec_fail_ts[tenant_id] = _t.time()
        logger.warning("⚠️ 租户[%s]向量库未就绪，降级为纯LLM对话（知识库不可用）: %s", tenant_id, str(e)[:200])
        return None


def get_tenant_retriever(app, tenant_id: str):
    """获取租户 retriever（缓存；未就绪返回 None）"""
    if tenant_id in app.state.tenant_retrievers:
        return app.state.tenant_retrievers[tenant_id]
    store = app.state.tenant_stores.get(tenant_id)
    if store is None:
        return None
    retriever = store.as_retriever(
        search_type="similarity",
        search_kwargs={"k": RETRIEVER_TOP_K}
    )
    app.state.tenant_retrievers[tenant_id] = retriever
    return retriever


# ====================== 租户模型配置（BYOK） ======================
# 缓存：tenant_id -> {"use_custom": bool, "model_name": str, "api_key": str, "api_base": str, "model": object}
_tenant_model_cache: dict = {}
# 配置缓存有效期（秒），避免每次对话都请求 Go 后端
_MODEL_CONFIG_TTL = 300


_MODEL_CONFIG_TTL = 300  # 5分钟缓存
_PLATFORM_CONFIG_TTL = 300  # 平台默认模型配置缓存


def clear_model_cache(tenant_id: str | None = None, scope: str = "all"):
    """清空模型配置/实例缓存。

    Go 后端修改模型配置成功后调用（POST /agent/cache/clear），
    使新配置立即生效，无需等待 TTL 过期。
    - tenant_id 为空：清全部（平台默认配置变更，所有跟随平台的租户都受影响）
    - tenant_id 非空 + scope：
      - "chat"      ：只清对话模型相关（配置缓存 + _model_ 实例），不清向量
      - "embedding" ：只清向量模型相关（配置缓存 + _emb_ 实例），不清对话
      - "all"       ：全部清除
    说明：租户配置缓存（tenant_id 键）是 chat+embedding 合并存储的，
    任一变更都需刷新（仅多一次 HTTP 拉取）；模型实例缓存按前缀精确清理。
    进程内缓存天然随本函数清空；多 worker/多实例部署时由 Go 广播调用，逐个实例清空。
    """
    if tenant_id:
        # 租户配置缓存合并存储（chat+embedding），任一变更都刷新
        _tenant_model_cache.pop(tenant_id, None)
        for k in list(_tenant_model_cache):
            is_model = k.startswith(f"_model_{tenant_id}:")
            is_emb = k.startswith(f"_emb_{tenant_id}:")
            if (scope == "chat" and is_model) or \
               (scope == "embedding" and is_emb) or \
               (scope == "all" and (is_model or is_emb)):
                _tenant_model_cache.pop(k, None)
        logger.info("✅ tenant model cache cleared: [%s] scope=%s", tenant_id, scope)
    else:
        _tenant_model_cache.clear()
        logger.info("✅ model config cache cleared (all tenants)")


async def get_platform_model_config(app) -> dict:
    """获取平台默认模型配置（= 平台管理员租户 t_admin 的模型配置，DB 单一数据源）"""
    cfg = await get_tenant_model_config(app, "t_admin")
    if not cfg:
        raise RuntimeError("获取平台默认模型配置失败：请先在后台配置模型提供商与平台默认模型")
    return {
        "model_config": {
            "model_provider": cfg.get("model_provider", ""),
            "model_name": cfg.get("model_name", ""),
            "api_base": cfg.get("api_base", ""),
            "api_key": cfg.get("api_key", ""),
            "use_custom": True,  # t_admin 即平台配置，始终生效
            "api_key_configured": bool(cfg.get("api_key")),
        },
        "embedding_config": {
            "embedding_provider": (cfg.get("embedding") or {}).get("provider", ""),
            "embedding_model_name": (cfg.get("embedding") or {}).get("model_name", ""),
            "embedding_api_base": (cfg.get("embedding") or {}).get("api_base", ""),
            "api_key": (cfg.get("embedding") or {}).get("api_key", ""),
            "use_custom": True,
            "api_key_configured": bool((cfg.get("embedding") or {}).get("api_key")),
        },
        "_ts": cfg.get("_ts", 0),
    }


async def get_platform_model(app):
    """获取平台默认模型实例（从平台配置创建，带缓存）"""
    cfg = await get_platform_model_config(app)
    mc = cfg.get("model_config", {})
    model_name = mc.get("model_name") or ""
    api_base = mc.get("api_base") or ""
    api_key = mc.get("api_key") or ""
    if not model_name or not api_key or not api_base:
        logger.error("❌ 平台默认对话模型配置不完整（请检查「平台模型配置」），model=%s base=%s key_configured=%s",
                     model_name, api_base, bool(api_key))
        raise RuntimeError("平台默认对话模型配置不完整，请检查后台模型配置")

    cache_key = f"_platform_model:{model_name}:{api_base}:{api_key[-4:]}"
    cached = _tenant_model_cache.get(cache_key)
    if cached:
        return cached

    try:
        model = init_chat_model(
            model=model_name,
            model_provider="openai",
            temperature=0,
            max_tokens=1024,
            timeout=30,
            max_retries=0,
            api_key=api_key,
            base_url=api_base,
        )
        _tenant_model_cache[cache_key] = model
        logger.info("✅ platform model created: model=%s", model_name)
        return model
    except Exception as e:
        logger.error("❌ 创建平台默认模型失败: %s", str(e))
        raise RuntimeError(f"平台默认对话模型鉴权失败：{str(e)}") from e


async def get_platform_embeddings(app):
    """获取平台默认 embeddings 实例（从平台配置创建，带缓存）"""
    cfg = await get_platform_model_config(app)
    ec = cfg.get("embedding_config", {})
    model_name = ec.get("embedding_model_name") or ""
    api_base = ec.get("embedding_api_base") or ""
    api_key = ec.get("api_key") or ""
    if not model_name or not api_key or not api_base:
        logger.error("❌ 平台默认 Embedding 模型配置不完整（请检查「平台模型配置」），model=%s base=%s key_configured=%s",
                     model_name, api_base, bool(api_key))
        raise RuntimeError("平台默认 Embedding 模型配置不完整，请检查后台模型配置")

    cache_key = f"_platform_emb:{model_name}:{api_base}:{api_key[-4:]}"
    cached = _tenant_model_cache.get(cache_key)
    if cached:
        return cached

    try:
        emb = OpenAIEmbeddings(
            model=model_name,
            api_key=api_key,
            base_url=api_base,
            check_embedding_ctx_length=False,
            chunk_size=10,
        )
        _tenant_model_cache[cache_key] = emb
        logger.info("✅ platform embeddings created: model=%s", model_name)
        return emb
    except Exception as e:
        logger.error("❌ 创建平台默认 embeddings 失败: %s", str(e))
        raise RuntimeError(f"平台默认 Embedding 模型鉴权失败：{str(e)}") from e


async def get_tenant_model_config(app, tenant_id: str) -> dict:
    """从 Go 后端获取租户模型配置，带缓存"""
    import time
    now = time.time()
    cached = _tenant_model_cache.get(tenant_id)
    if cached and (now - cached.get("_ts", 0)) < _MODEL_CONFIG_TTL:
        return cached

    try:
        headers = {}
        if INTERNAL_API_TOKEN:
            headers["X-Internal-Token"] = INTERNAL_API_TOKEN
        resp = await app.state.http_client.get(
            f"{GO_BUSINESS_API}/api/agent/model-config",
            params={"tenant_id": tenant_id},
            headers=headers,
            timeout=5.0,
        )
        data = resp.json()
        # Go 瞬时未就绪可能返回 null/非对象：按失败处理（不抛 NoneType 异常），下次请求重试
        if isinstance(data, dict) and data.get("code") == 0 and data.get("data"):
            cfg = data["data"]
            cfg["_ts"] = now
            _tenant_model_cache[tenant_id] = cfg
            logger.info("✅ tenant model config loaded: [%s] use_custom=%s", tenant_id, cfg.get("use_custom"))
            return cfg
    except Exception as e:
        logger.warning("⚠️ 获取租户模型配置失败 [%s]: %s，使用平台默认模型", tenant_id, str(e))

    # 失败时返回 None（不再回退 .env；上层业务给出明确错误）
    logger.error("❌ 租户模型配置获取失败 [%s]，请检查后台「模型提供商/租户模型配置」", tenant_id)
    return None


def invalidate_tenant_model_cache(tenant_id: str):
    """清除租户模型配置缓存（模型配置变更后调用）"""
    _tenant_model_cache.pop(tenant_id, None)


async def current_model_name(app, tenant_id: str) -> str:
    """获取租户当前生效的 LLM 模型名（token 用量 fallback 用，带缓存）"""
    try:
        cfg = await get_tenant_model_config(app, tenant_id)
        if cfg:
            name = cfg.get("model_name") or ""
            if name:
                return name
    except Exception as e:
        logger.warning("current_model_name 获取失败: %s", str(e))
    return ""


async def current_model_source(app, tenant_id: str) -> str:
    """获取租户当前 LLM 用量来源：'platform'（使用平台模型）/ 'custom'（租户自定义模型）
    token 用量统计用：平台模型专项统计只计 source='platform' 的记录"""
    try:
        cfg = await get_tenant_model_config(app, tenant_id)
        if cfg and cfg.get("use_custom"):
            return "custom"
    except Exception as e:
        logger.warning("current_model_source 获取失败: %s", str(e))
    return "platform"


async def current_chat_prices(app, tenant_id: str):
    """获取租户当前生效 chat 单价（元/百万tokens），返回 (input_cache, input_miss, output)
    输入分「命中缓存/未命中」两档（DeepSeek 等）；平台模型取 t_admin 行单价，租户自定义取租户行单价；
    读取失败/未定价返回 (0,0,0)"""
    try:
        cfg = await get_tenant_model_config(app, tenant_id)
        if cfg:
            try:
                return (float(cfg.get("chat_input_cache_price") or 0),
                        float(cfg.get("chat_input_price") or 0),
                        float(cfg.get("chat_output_price") or 0))
            except (TypeError, ValueError):
                pass
    except Exception as e:
        logger.warning("current_chat_prices 获取失败: %s", str(e))
    return 0.0, 0.0, 0.0


async def get_tenant_model(app, tenant_id: str):
    """获取租户专属模型实例（无自定义配置则用平台默认模型）"""
    cfg = await get_tenant_model_config(app, tenant_id)
    if not cfg:
        raise RuntimeError(f"租户{tenant_id}模型配置获取失败，请检查后台模型配置")
    if not cfg.get("use_custom"):
        return await get_platform_model(app)

    # 租户自定义模型：创建新的模型实例
    model_name = cfg.get("model_name")
    api_key = cfg.get("api_key")
    api_base = cfg.get("api_base")

    # 校验平台emb配置非空
    if not model_name or not api_key or not api_base:
        logger.error("❌ 租户[%s]自定义对话模型环境，变量缺失", tenant_id)
        # 不fallback，直接抛异常，上层业务捕获后返回错误给用户
        raise RuntimeError(f"租户{tenant_id}自定义对话模型环境，变量缺失")

    # 缓存 key：用配置内容做 hash，避免配置变更后还用旧模型
    cache_key = f"{tenant_id}:{model_name}:{api_base}:{api_key[-4:]}"
    cached_model = _tenant_model_cache.get(f"_model_{cache_key}")
    if cached_model:
        return cached_model

    try:
        model = init_chat_model(
            model=model_name,
            model_provider="openai",
            temperature=0,
            max_tokens=1024,
            timeout=30,
            max_retries=0,
            api_key=api_key,
            base_url=api_base,
        )
        _tenant_model_cache[f"_model_{cache_key}"] = model
        logger.info("✅ tenant custom model created: [%s] model=%s", tenant_id, model_name)
        return model
    except Exception as e:
        # logger.error("❌ 创建租户自定义模型失败 [%s]: %s，回退到平台默认模型", tenant_id, str(e))
        # return app.state.model
        logger.error("❌ 租户[%s]自定义对话模型校验失败，对话不可用: %s", tenant_id, str(e))
        # 不fallback，直接抛异常，上层业务捕获后返回错误给用户
        raise RuntimeError(f"租户{tenant_id}自定义对话模型鉴权失败：{str(e)}") from e


async def get_tenant_embeddings(app, tenant_id: str):
    """获取租户专属 embeddings 实例（无自定义配置则用平台默认）"""
    cfg = await get_tenant_model_config(app, tenant_id)
    if not cfg:
        raise RuntimeError(f"租户{tenant_id}模型配置获取失败，请检查后台模型配置")
    emb_cfg = cfg.get("embedding", {}) if isinstance(cfg, dict) else {}
    if not emb_cfg.get("use_custom"):
        return await get_platform_embeddings(app)

    model_name = emb_cfg.get("model_name") or ""
    api_key = emb_cfg.get("api_key") or ""
    api_base = emb_cfg.get("api_base") or ""

    cache_key = f"_emb_{tenant_id}:{model_name}:{api_base}:{api_key[-4:]}"
    cached = _tenant_model_cache.get(cache_key)
    if cached:
        return cached

    try:
        emb = OpenAIEmbeddings(
            model=model_name,
            api_key=api_key,
            base_url=api_base,
            check_embedding_ctx_length=False,
            chunk_size=10,
        )
        _tenant_model_cache[cache_key] = emb
        logger.info("✅ tenant custom embeddings created: [%s] model=%s", tenant_id, model_name)
        return emb
    except Exception as e:
        # logger.error("❌ 创建租户自定义 embeddings 失败 [%s]: %s，回退到平台默认", tenant_id, str(e))
        # return app.state.embeddings
        # 这里会捕获：空key、401鉴权失败、模型不存在、网络错误
        logger.error("❌ 租户[%s]自定义embedding校验失败，BYOK不可用: %s", tenant_id, str(e))
        # 不fallback，直接抛异常，上层业务捕获后返回错误给用户
        raise RuntimeError(f"租户{tenant_id}自定义向量模型鉴权失败：{str(e)}") from e


async def get_tenant_agent(app, tenant_id: str):
    """获取租户专属 Agent（懒构建：集合/知识库/retriever/Prompt/Agent 均按租户隔离）"""
    # 配置签名懒检查：Redis 广播丢失时的最终一致兜底（TTL 内跳过，失败不阻断）
    sig_version = await _check_config_version(app, tenant_id)
    if tenant_id in app.state.tenant_agents:
        return app.state.tenant_agents[tenant_id]
    await ensure_tenant_vector(app, tenant_id)
    get_tenant_retriever(app, tenant_id)
    async with app.state.db_pool.connection() as conn:
        prompt = await load_active_prompt(conn, tenant_id)
    # 使用租户专属模型（BYOK：有自定义配置则用租户的，否则用平台默认）
    model = await get_tenant_model(app, tenant_id)
    agent = create_agent(
        model=model,
        tools=app.state.tools,
        middleware=[content_guard],
        checkpointer=app.state.checkpointer,
        system_prompt=prompt,
    )
    app.state.tenant_agents[tenant_id] = agent
    # 记录本次构建对应的配置签名：
    # - 签名检查路径：_check_config_version 返回的 remote version
    # - 广播路径：TTL 内跳过检查返回 None，从刚拉取的配置缓存补记，避免 10s 后误判一次变化
    if not sig_version:
        cached_cfg = _tenant_model_cache.get(tenant_id) or {}
        sig_version = cached_cfg.get("config_version") or None
    if sig_version:
        _agent_sigs[tenant_id] = sig_version
    logger.info("✅ tenant agent built: [%s] prompt_len=%d", tenant_id, len(prompt))
    return agent


# ====================== 配置签名懒检查（广播丢失兜底） ======================
# 与 Redis 广播互补：广播负责即时，签名负责最终一致。
# Agent 构建时记录 config_version，每次调用前（10s TTL）与 Go 当前版本对比，
# 不一致 → 清除该租户缓存并重建，最多延迟 10s 发现配置变化。
_agent_sigs: dict = {}          # tenant_id -> 构建 Agent 时的 config_version
_sig_check_ts: dict = {}        # tenant_id -> 上次检查时间（monotonic）
_SIG_CHECK_TTL = 10             # 秒


async def _fetch_config_version(app, tenant_id: str) -> str | None:
    """从 Go 拉取当前配置签名（无缓存，保证新鲜）"""
    headers = {}
    if INTERNAL_API_TOKEN:
        headers["X-Internal-Token"] = INTERNAL_API_TOKEN
    resp = await app.state.http_client.get(
        f"{GO_BUSINESS_API}/api/agent/model-config",
        params={"tenant_id": tenant_id},
        headers=headers,
        timeout=5.0,
    )
    data = resp.json()
    if data.get("code") == 0 and data.get("data"):
        return data["data"].get("config_version") or None
    return None


async def _check_config_version(app, tenant_id: str) -> str | None:
    """懒检查配置签名；变化则清除该租户缓存（下次调用重建 Agent）。
    返回当前 remote version（TTL 内或拉取失败返回 None，不阻断聊天）。"""
    import time
    now = time.monotonic()
    if now - _sig_check_ts.get(tenant_id, 0) < _SIG_CHECK_TTL:
        return None
    _sig_check_ts[tenant_id] = now
    try:
        remote = await _fetch_config_version(app, tenant_id)
    except Exception as e:
        logger.warning("⚠️ 配置签名检查失败 [%s]（跳过本轮）: %s", tenant_id, str(e))
        return None
    if remote is None:
        return None
    if remote != _agent_sigs.get(tenant_id):
        # 配置已变化（可能广播丢失）：清该租户缓存，get_tenant_agent 走重建路径
        logger.info("🔄 配置签名变化 [%s] %s -> %s（广播兜底，重建 Agent）",
                    tenant_id, _agent_sigs.get(tenant_id, "(none)"), remote)
        clear_model_cache(tenant_id, "all")
        app.state.tenant_agents.pop(tenant_id, None)
        app.state.tenant_retrievers.pop(tenant_id, None)
    return remote


async def rebuild_tenant_agent(app, tenant_id: str):
    """按租户最新 Prompt 重建其 Agent（Prompt 管理变更后调用）"""
    if tenant_id in app.state.tenant_agents:
        del app.state.tenant_agents[tenant_id]
    return await get_tenant_agent(app, tenant_id)


async def invalidate_platform_prompt_agents(app) -> int:
    """平台级 Prompt 变更：仅使『无激活自有 Prompt』的租户 Agent 失效（懒重建读新平台 Prompt）。

    有自有 Prompt 的租户不受平台级 Prompt 变更影响，Agent 缓存保留。
    返回失效的 Agent 数量。
    """
    async with app.state.db_pool.connection() as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "SELECT DISTINCT tenant_id FROM prompts WHERE tenant_id <> '' AND is_active = true")
            rows = await cur.fetchall()
    with_own = {r[0] for r in rows}
    agents = getattr(app.state, "tenant_agents", {})
    affected = [t for t in list(agents.keys()) if t not in with_own]
    for t in affected:
        agents.pop(t, None)
    logger.info("✅ 平台级 Prompt 变更：%d 个租户 Agent 失效（无自有 Prompt）；有自有 Prompt 的 %d 个租户保留",
                len(affected), len(with_own))
    return len(affected)
