"""模型配置缓存管理。

⚠️ 强制 Redis 后，Go 侧缓存失效已统一走 Redis 广播（cs:model:cache:invalidate），
不再 HTTP 直调本接口。本 HTTP 入口保留仅用于手工调试/排障
（POST /agent/cache/clear?tenant_id=&scope=）。

清除逻辑（clear_tenant_caches）同时被 HTTP 入口与 Redis 订阅消费共用，行为一致：
模型/Embedding 配置变更后，不仅配置缓存要清，**已构建的租户 Agent
（LangGraph 图绑定旧模型实例）与向量检索器（绑定旧 Embedding 实例）
也必须清空**，否则聊天仍会走旧模型（表现：切换模型不生效/一直超时）。
"""
import logging

from fastapi import APIRouter, Request

from agent_factory import clear_model_cache

logger = logging.getLogger("agent.routers_cache")
router = APIRouter()


def clear_tenant_caches(app, tenant_id: str | None, scope: str = "all") -> dict:
    """按租户+scope 清除模型缓存（HTTP 入口与 Redis 广播订阅共用，保证两路行为一致）。

    - tenant_id 为空：清全部（平台默认配置变更，所有跟随平台的租户都受影响）
    - tenant_id 非空 + scope：
      - "chat"      ：只清对话模型缓存 + 该租户 Agent（Agent 绑定对话模型实例）
      - "embedding" ：只清向量模型缓存 + 该租户检索器 + Agent（Agent 持有旧 retriever 引用，必须一并重建）
      - "all"       ：全部清除
    """
    clear_model_cache(tenant_id, scope)
    # 检索结果缓存（kb_cache）与向量模型绑定：向量配置变更时一并清空，避免旧答案残留
    try:
        from cache import clear_kb_cache
        n_kb = clear_kb_cache()
        if n_kb:
            logger.info("✅ kb cache cleared: %d items", n_kb)
    except Exception as e:
        logger.warning("清 kb_cache 失败: %s", e)
    agents = getattr(app.state, "tenant_agents", {})
    retr = getattr(app.state, "tenant_retrievers", {})
    if tenant_id:
        n_agents = n_retr = 0
        if scope in ("chat", "all") and tenant_id in agents:
            agents.pop(tenant_id, None)
            n_agents = 1
        if scope in ("embedding", "all"):
            if tenant_id in retr:
                retr.pop(tenant_id, None)
                n_retr = 1
            if tenant_id in agents:
                agents.pop(tenant_id, None)
                n_agents = 1  # Agent 持有旧 retriever 引用，向量模型变更必须重建
        logger.info("✅ caches cleared: config, agents=%d, retrievers=%d (tenant=%s, scope=%s)",
                    n_agents, n_retr, tenant_id, scope)
    else:
        n_agents = len(agents)
        n_retr = len(retr)
        agents.clear()
        retr.clear()
        logger.info("✅ caches cleared: config, agents=%d, retrievers=%d (all tenants)",
                    n_agents, n_retr)
    return {"code": 0, "msg": "cache cleared"}


@router.post("/agent/cache/clear")
async def cache_clear(request: Request, tenant_id: str | None = None, scope: str = "all"):
    """清空模型配置/实例/Agent/检索器缓存（由 Go 在配置保存成功后调用）。"""
    try:
        return clear_tenant_caches(request.app, tenant_id, scope)
    except Exception as e:
        logger.warning("清空模型缓存失败: %s", e)
        return {"code": 500, "msg": "清空缓存失败: " + str(e)}
