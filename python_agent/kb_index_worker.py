"""知识库异步索引 worker：消费 Redis 队列，执行分片 → embedding → Qdrant 写入。

- 队列：Redis List `cs:kb:index:queue`，消息 {"doc_id": "...", "tenant_id": "..."}
- 上传/重索引只入队并标记 status=0，本 worker 异步完成向量化后更新 status=1/2
- 幂等：处理前先按 doc_id 删除旧 Qdrant points，重复消费不产生重复向量
- 崩溃兜底：main.py 启动时扫描 status=0 记录重新入队
"""
import asyncio
import json
import logging
import uuid

from qdrant_client.http import models as qmodels

from agent_factory import ensure_tenant_vector
from db_ops import collection_name
from db_pool import _pool_conn
from routers_kb import KB_INDEX_QUEUE, read_kb_file_text

logger = logging.getLogger("agent.kb_worker")

MAX_TEXT_CHARS = 1_000_000  # 单文档参与分片的文本上限（约 1MB 字符），超长截断防 embedding 超时


async def _index_one(app, doc_id: str, tenant_id: str) -> None:
    """处理单个文档：分片 → embedding → Qdrant → 更新状态（失败标记 status=2）"""
    # 1) 读 DB 记录（content 列已废弃，全文以共享卷原文件为唯一数据源）
    async with _pool_conn(app) as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "SELECT title, file_name FROM kb_documents WHERE doc_id = %s AND tenant_id = %s",
                (doc_id, tenant_id))
            row = await cur.fetchone()
    if not row:
        logger.warning("KB worker: doc not found %s/%s, skip", tenant_id, doc_id)
        return
    title, file_name = row[0], (row[1] or "")
    content = read_kb_file_text(tenant_id, doc_id, file_name).strip()
    if not content:
        await _mark_failed(app, doc_id, tenant_id, "原文件缺失或无法解析，请重新上传")
        return

    try:
        # 2) 标记索引中（status=3，前端进度弹窗可显示"索引中"）
        async with _pool_conn(app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "UPDATE kb_documents SET status = 3, error_msg = NULL, updated_at = CURRENT_TIMESTAMP "
                    "WHERE doc_id = %s AND tenant_id = %s",
                    (doc_id, tenant_id))

        # 3) 幂等：先删旧向量（防重复消费/重索引残留）
        try:
            app.state.qdrant_client.delete(
                collection_name=collection_name(tenant_id),
                points_selector=qmodels.FilterSelector(filter=qmodels.Filter(must=[
                    qmodels.FieldCondition(key="doc_id", match=qmodels.MatchValue(value=doc_id))
                ])),
            )
        except Exception:
            pass

        # 4) 分片（超长截断）
        text = content[:MAX_TEXT_CHARS]
        chunks = app.state.text_splitter.create_documents([text])
        if not chunks:
            await _mark_failed(app, doc_id, tenant_id, "文档分片结果为空")
            return
        texts = [ch.page_content for ch in chunks]
        ids = [str(uuid.uuid4()) for _ in range(len(texts))]

        # 5) embedding + 写入 Qdrant
        vs = await ensure_tenant_vector(app, tenant_id)
        if vs is None:
            await _mark_failed(app, doc_id, tenant_id, "向量模型/向量库不可用，索引失败（请检查后台模型配置）")
            return
        await vs.aadd_texts(
            texts,
            metadatas=[{"doc_id": doc_id, "title": title, "source": "kb",
                        "tenant_id": tenant_id}] * len(texts),
            ids=ids,
        )

        # 6) 更新状态
        async with _pool_conn(app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "UPDATE kb_documents SET chunk_count = %s, status = 1, error_msg = NULL, updated_at = CURRENT_TIMESTAMP "
                    "WHERE doc_id = %s AND tenant_id = %s",
                    (len(texts), doc_id, tenant_id))
        app.state.tenant_retrievers.pop(tenant_id, None)
        logger.info("KB worker indexed tenant=%s doc_id=%s chunks=%d", tenant_id, doc_id, len(texts))
    except Exception as e:
        logger.exception("KB worker index error doc_id=%s", doc_id)
        await _mark_failed(app, doc_id, tenant_id, str(e))


async def _mark_failed(app, doc_id: str, tenant_id: str, err: str) -> None:
    try:
        async with _pool_conn(app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "UPDATE kb_documents SET status = 2, error_msg = %s, updated_at = CURRENT_TIMESTAMP "
                    "WHERE doc_id = %s AND tenant_id = %s",
                    (err[:500], doc_id, tenant_id))
    except Exception:
        pass


async def index_worker_loop(app, worker_id: int) -> None:
    """单 worker 循环：BRPOP 消费队列，处理失败不影响后续消息"""
    r = getattr(app.state, "redis_client", None)
    if r is None:
        logger.error("KB worker[%d]: redis client unavailable, worker exits", worker_id)
        return
    logger.info("🔧 KB index worker[%d] started", worker_id)
    while True:
        try:
            item = await r.blpop(KB_INDEX_QUEUE, timeout=5)
            if item is None:
                continue
            try:
                data = json.loads(item[1])
                await _index_one(app, data.get("doc_id", ""), data.get("tenant_id", ""))
            except Exception as e:
                logger.warning("KB worker[%d] task error: %s", worker_id, e)
        except asyncio.CancelledError:
            logger.info("KB worker[%d] cancelled", worker_id)
            raise
        except Exception as e:
            logger.warning("KB worker[%d] loop error: %s", worker_id, e)
            await asyncio.sleep(2)


async def requeue_pending(app) -> int:
    """启动兜底：扫描 status=3（上次处理中崩溃）的记录重新入队；
    注意：status=0 是"待手动索引"（上传后不自动入队），不在此处自动消费。"""
    n = 0
    try:
        async with _pool_conn(app) as conn:
            async with conn.cursor() as cur:
                await cur.execute("SELECT doc_id, tenant_id FROM kb_documents WHERE status = 3")
                rows = await cur.fetchall()
        r = getattr(app.state, "redis_client", None)
        if r is None:
            return 0
        for doc_id, tenant_id in rows:
            await r.lpush(KB_INDEX_QUEUE, json.dumps({"doc_id": doc_id, "tenant_id": tenant_id}))
            n += 1
        if n:
            logger.info("KB worker: requeued %d interrupted docs (status=3)", n)
    except Exception as e:
        logger.warning("KB requeue pending error: %s", e)
    return n
