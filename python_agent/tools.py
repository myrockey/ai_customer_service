"""
P4-3：Agent 工具函数模块
从 main.py 拆分，包含 search_kb、query_order、transfer_to_human 三个工具。
采用 app 注入模式：在 main.py 中调用 set_app(app) 注入全局实例。
"""
from langchain_core.tools import tool
from langgraph.types import interrupt

from cache import _kb_cache_get, _kb_cache_set
from config import GO_BUSINESS_API
from db_pool import _pool_conn

# 模块级 app 引用，由 main.py 注入
_app = None
_logger = None
_tenant_var = None
_pending_ticket_var = None
_thread_var = None


def set_app(app, logger, tenant_var, pending_ticket_var=None, thread_var=None):
    """注入全局 app、logger、tenant_var（在 main.py 中调用）"""
    global _app, _logger, _tenant_var, _pending_ticket_var, _thread_var
    _app = app
    _logger = logger
    _tenant_var = tenant_var
    _pending_ticket_var = pending_ticket_var
    _thread_var = thread_var


async def _record_kb_hits(tid: str, thread_id: str, query: str, docs: list) -> None:
    """KB 闭环：检索结果落库（异步旁路，失败不影响对话）。
    docs 为空 → 插入一条未命中记录（doc_id 为空）。
    docs 非空 → 每个命中文档插一条记录（doc_id/doc_title/score）。
    """
    try:
        if docs:
            rows = [
                (tid, thread_id, d.metadata.get("doc_id", ""), d.metadata.get("title", ""), query,
                 float(getattr(d, "metadata", {}).get("score", 0) or 0))
                for d in docs
            ]
        else:
            rows = [(tid, thread_id, "", "", query, 0.0)]
        async with _pool_conn(_app) as conn:
            async with conn.cursor() as cur:
                await cur.executemany(
                    "INSERT INTO kb_hits (tenant_id, thread_id, doc_id, doc_title, query, score) "
                    "VALUES (%s, %s, %s, %s, %s, %s)",
                    rows,
                )
    except Exception as e:
        if _logger:
            _logger.warning(f"[KB Hit] 记录失败 tenant={tid}: {str(e)[:120]}")


@tool
async def search_kb(query: str) -> str:
    """搜索知识库，获取关于平台、课程、政策等官方信息。
    若工具返回"知识库暂未就绪"，说明知识库当前不可用：请如实告知用户"知识库暂时无法访问"，
    不要编造或猜测产品价格、政策、库存等具体信息，并建议用户稍后重试或转接人工客服。
    Args:
        query: 搜索问题或关键词
    """
    tid = _tenant_var.get() or ""
    thread_id = (_thread_var.get() if _thread_var is not None else "") or ""

    # P2-2：先查缓存
    cached = _kb_cache_get(tid, query)
    if cached is not None:
        _logger.debug(f"[KB Cache HIT] tenant={tid}, query={query[:30]}")
        return cached

    retriever = _app.state.tenant_retrievers.get(tid)
    if retriever is None:
        return "知识库暂未就绪，请稍后再试。"
    try:
        docs = await retriever.ainvoke(query)
    except Exception as e:
        # 检索期降级：embedding 调用失败（key 错误/服务不可用）不抛穿对话，如实告知
        _logger.warning(f"[KB Degrade] tenant={tid} 检索失败，降级为纯LLM: {str(e)[:150]}")
        return "知识库暂未就绪，请稍后再试。"
    if not docs:
        result = "未找到相关信息，建议转接人工客服。"
    else:
        result = "\n".join(f"- {doc.page_content}" for doc in docs)

    # KB 闭环：检索结果落库（命中明细 / 未命中查询）
    await _record_kb_hits(tid, thread_id, query, docs)

    # P2-2：写入缓存
    _kb_cache_set(tid, query, result)
    _logger.debug(f"[KB Cache MISS] tenant={tid}, query={query[:30]}, cached")
    return result


@tool
async def query_order(order_id: str) -> str:
    """根据订单号查询订单状态和详情，调用Go业务系统获取真实订单。
    Args:
        order_id: 订单号，如 ORD-2024-001
    """
    http_client = _app.state.http_client
    try:
        resp = await http_client.get(f"{GO_BUSINESS_API}/api/business/order?order_id={order_id}", timeout=5)
        resp.raise_for_status()
        data = resp.json()
        return str(data.get("data", "未获取订单数据"))
    except Exception as e:
        _logger.warning(f"query_order error: {e}")
        return f"查询订单业务异常：{str(e)}"


@tool
async def transfer_to_human(reason: str) -> str:
    """将用户转接给人工客服，触发LangGraph HITL中断。
    Args:
        reason: 转接原因
    """
    # 待处理工单已存在（排队中未接管）：不再重复触发中断，正常提示等待，人工接入后自动转交
    if _pending_ticket_var is not None and _pending_ticket_var.get():
        return ("您已提交人工客服，正在等待人工接入。人工客服接入后我会将完整对话转交，"
                "排队期间我仍可继续为您解答问题。")
    approval = interrupt({
        "action": "transfer_to_human",
        "reason": reason,
        "message": f"用户请求转接人工客服，原因：{reason}。是否转接？"
    })
    if approval.get("confirmed"):
        return (f"已为您转接人工客服，预计等待 {approval.get('wait_time', 3)} 分钟。"
                f"工单号：{approval.get('ticket_id', 'N/A')}")
    return "转接已取消，我继续为您服务。"
