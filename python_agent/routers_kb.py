"""P2-5：知识库后台管理路由（从 main.py 拆分，按租户隔离）

上传/重索引采用「落盘 + DB 登记 + Redis 队列异步索引」：
- 上传：提取文本 → 原文件落盘到共享卷（nginx 静态服务）→ DB 记 status=0 → 入队
- 后台 worker（kb_index_worker）消费队列：分片 → embedding → Qdrant → status=1
- 下载/预览：前端直接用 nginx 静态 file_url，不再经 Go/Python 查库取 content
"""
import json
import logging
import os
import uuid

from fastapi import APIRouter, File, Form, Request, UploadFile
from fastapi.responses import JSONResponse
from qdrant_client.http import models as qmodels

from agent_factory import ensure_tenant_vector
from db_ops import (check_kb_quota, collection_name, get_platform_setting,
                    set_platform_setting, tenant_from_request)
from db_pool import _pool_conn
from config import INTERNAL_API_TOKEN
from kb_parser import extract_text

logger = logging.getLogger("agent.routers_kb")
router = APIRouter()

# 单文件上传大小上限默认值（MB），平台管理员可通过 platform_settings 的 kb_max_file_mb 动态配置
DEFAULT_MAX_FILE_MB = 10

# 知识库原文件存储目录（共享卷，nginx 以 /kb_files/ 静态服务）
KB_FILE_DIR = os.getenv("KB_FILE_DIR", "/app/data/kb_files")
KB_FILE_URL_PREFIX = "/kb_files/"
# Redis 异步索引队列
KB_INDEX_QUEUE = "cs:kb:index:queue"


async def _kb_max_file_mb(app) -> int:
    """读取知识库单文件大小上限（MB），平台设置优先，env KB_MAX_FILE_MB 兜底"""
    try:
        async with _pool_conn(app) as conn:
            v = await get_platform_setting(conn, "kb_max_file_mb", "")
        if v and v.strip().isdigit():
            return max(1, int(v))
    except Exception:
        pass
    try:
        return max(1, int(os.getenv("KB_MAX_FILE_MB", DEFAULT_MAX_FILE_MB)))
    except Exception:
        return DEFAULT_MAX_FILE_MB


def _file_ext(file_name: str) -> str:
    """从文件名取小写扩展名（含点，无扩展名返回 ''）"""
    name = (file_name or "").strip()
    if "." not in name:
        return ""
    return "." + name.rsplit(".", 1)[1].lower()


def kb_file_url(tenant_id: str, doc_id: str, file_name: str) -> str:
    """生成知识库原文件静态下载 URL（nginx /kb_files/ 直接服务）"""
    ext = _file_ext(file_name)
    if not ext:
        return ""
    return f"{KB_FILE_URL_PREFIX}{tenant_id}/{doc_id}{ext}"


def save_kb_file(tenant_id: str, doc_id: str, file_name: str, raw: bytes) -> str:
    """落盘原文件到共享卷，返回绝对路径"""
    ext = _file_ext(file_name) or ".bin"
    d = os.path.join(KB_FILE_DIR, tenant_id)
    os.makedirs(d, exist_ok=True)
    path = os.path.join(d, doc_id + ext)
    with open(path, "wb") as f:
        f.write(raw)
    return path


def delete_kb_file(tenant_id: str, doc_id: str, file_name: str) -> None:
    """删除共享卷中的原文件（尽力而为）"""
    try:
        ext = _file_ext(file_name)
        if ext:
            path = os.path.join(KB_FILE_DIR, tenant_id, doc_id + ext)
            if os.path.exists(path):
                os.remove(path)
    except Exception:
        pass


def read_kb_file_text(tenant_id: str, doc_id: str, file_name: str) -> str:
    """从共享卷读取原文件并提取文本（无文件/解析失败返回 ''）"""
    try:
        ext = _file_ext(file_name) or ".bin"
        p = os.path.join(KB_FILE_DIR, tenant_id, doc_id + ext)
        if not os.path.exists(p):
            return ""
        with open(p, "rb") as f:
            raw = f.read()
        return extract_text(file_name, raw) or ""
    except Exception:
        return ""


async def enqueue_index(app, doc_id: str, tenant_id: str) -> bool:
    """推入 Redis 异步索引队列；Redis 不可用返回 False（强制依赖）"""
    r = getattr(app.state, "redis_client", None)
    if r is None:
        return False
    try:
        await r.lpush(KB_INDEX_QUEUE, json.dumps({"doc_id": doc_id, "tenant_id": tenant_id}))
        return True
    except Exception as e:
        logger.warning("KB enqueue failed doc_id=%s: %s", doc_id, e)
        return False


# ====================== 知识库后台管理（按租户隔离） ======================
@router.post("/agent/kb/upload")
async def kb_upload(request: Request, title: str = Form(None), content: str = Form(None),
                    category: str = Form("默认"),
                    file: UploadFile = File(None)):
    tenant_id = tenant_from_request(request)
    doc_id = uuid.uuid4().hex
    text = ""
    src_name = "text"
    file_size = 0
    file_url = ""
    raw = b""

    if file is not None:
        # 单文件大小限制（平台管理员动态配置 kb_max_file_mb）
        max_mb = await _kb_max_file_mb(request.app)
        raw = await file.read()
        if len(raw) > max_mb * 1024 * 1024:
            return JSONResponse(status_code=400, content={
                "code": 400,
                "msg": f"文件超过大小限制（{max_mb} MB），请压缩或拆分后上传",
            })
        src_name = file.filename or "text"
        try:
            text = extract_text(src_name, raw)
        except RuntimeError as e:
            return {"code": 400, "msg": str(e)}
        if not text:
            return {"code": 400, "msg": "文件无可提取文本"}
        file_size = len(raw)
        # 原文件落盘（nginx 静态服务，供下载/预览）
        try:
            save_kb_file(tenant_id, doc_id, src_name, raw)
            file_url = kb_file_url(tenant_id, doc_id, src_name)
        except Exception as e:
            logger.exception("kb file save error")
            return {"code": 500, "msg": "文件保存失败: " + str(e)}
    else:
        # 兼容 JSON 文本粘贴请求体
        ct = request.headers.get("content-type", "")
        if "application/json" in ct:
            try:
                body = await request.json()
                title = title or body.get("title")
                content = body.get("content")
                category = body.get("category", category)
            except Exception:
                pass
        text = (content or "").strip()
        src_name = "text.txt"
        file_size = len(text.encode("utf-8"))
        # 文本粘贴同样落盘为 txt（content 列已废弃，文件为唯一数据源）
        if text:
            try:
                save_kb_file(tenant_id, doc_id, src_name, text.encode("utf-8"))
                file_url = kb_file_url(tenant_id, doc_id, src_name)
            except Exception as e:
                logger.exception("kb text save error")
                return {"code": 500, "msg": "文件保存失败: " + str(e)}

    if not text:
        return {"code": 400, "msg": "内容为空"}

    # P1-2：知识库配额校验（文档数/容量）
    async with _pool_conn(request.app) as conn:
        quota_err = await check_kb_quota(conn, request.app, tenant_id)
    if quota_err:
        # 配额超限时清理已落盘文件
        delete_kb_file(tenant_id, doc_id, src_name)
        return quota_err

    doc_title = (title or src_name).strip() or "未命名文档"
    try:
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "INSERT INTO kb_documents (doc_id, tenant_id, title, chunk_count, file_name, file_size, category, status, file_url) "
                    "VALUES (%s, %s, %s, 0, %s, %s, %s, 0, %s)",
                    (doc_id, tenant_id, doc_title, src_name, file_size, category, file_url or None),
                )
        # 上传只登记 status=0（待索引），不自动入队：
        # 由用户在知识库后台勾选后手动触发「开始索引/重索引」，避免大文件上传即消耗 embedding 配额，
        # 也便于统一批量控制索引进度（前端弹窗轮询）。
        logger.info("KB upload registered tenant=%s doc_id=%s title=%s size=%d file_url=%s (待手动索引)",
                    tenant_id, doc_id, doc_title, file_size, file_url)
        return {"code": 0, "data": {"doc_id": doc_id, "title": doc_title, "chunk_count": 0,
                                     "char_count": len(text), "category": category, "file_name": src_name,
                                     "file_size": file_size, "status": 0, "file_url": file_url,
                                     "msg": "已上传，待手动索引"}}
    except Exception as e:
        logger.exception("kb upload error")
        delete_kb_file(tenant_id, doc_id, src_name)
        return {"code": 500, "msg": str(e)}


@router.get("/agent/kb/documents")
async def kb_documents(request: Request, category: str = None, keyword: str = None,
                       page: int = 1, page_size: int = 10):
    tenant_id = tenant_from_request(request)
    keyword = (keyword or "").strip()
    page = max(1, page)
    page_size = min(max(1, page_size), 100)
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            where_sql = " WHERE tenant_id = %s"
            conds = [tenant_id]
            if category:
                where_sql += " AND category = %s"
                conds.append(category)
            if keyword:
                # content 列已废弃（文件为唯一数据源），仅按标题/文件名搜索
                where_sql += " AND (title ILIKE %s OR file_name ILIKE %s)"
                conds.extend([f"%{keyword}%", f"%{keyword}%"])
            # 总数
            await cur.execute("SELECT count(*) FROM kb_documents" + where_sql, tuple(conds))
            (total,) = await cur.fetchone()
            # 分页数据：列表不拉取 content 大字段（预览/下载时按 doc_id 单独查），避免大字段随列表传输
            base_sql = ("SELECT doc_id, title, chunk_count, created_at, file_name, file_size, category, status, error_msg, updated_at, file_url "
                        "FROM kb_documents" + where_sql + " ORDER BY created_at DESC LIMIT %s OFFSET %s")
            await cur.execute(base_sql, tuple(conds) + (page_size, (page - 1) * page_size))
            rows = await cur.fetchall()
    return {"code": 0, "data": {
        "list": [
            {"doc_id": r[0], "title": r[1], "chunk_count": r[2],
             "created_at": r[3].isoformat() if r[3] else None,
             "file_name": r[4], "file_size": r[5] or 0, "category": r[6] or "默认",
             "status": r[7] if r[7] is not None else 0, "error_msg": r[8],
             "updated_at": r[9].isoformat() if r[9] else None,
             "file_url": r[10] or kb_file_url(tenant_id, r[0], r[4])}
            for r in rows
        ],
        "total": total, "page": page, "page_size": page_size,
    }}


# ====================== 平台设置（平台管理员动态配置，如知识库上传大小限制） ======================
@router.get("/agent/platform-settings")
async def platform_settings_get(request: Request):
    """平台设置列表（内部接口，需 X-Internal-Token）"""
    if request.headers.get("X-Internal-Token") != INTERNAL_API_TOKEN:
        return JSONResponse(status_code=403, content={"code": 403, "msg": "forbidden"})
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            await cur.execute("SELECT key, value FROM platform_settings ORDER BY key")
            rows = await cur.fetchall()
    return {"code": 0, "data": [{"key": r[0], "value": r[1]} for r in rows]}


@router.put("/agent/platform-settings")
async def platform_settings_put(request: Request):
    """写入平台设置（内部接口，需 X-Internal-Token）"""
    if request.headers.get("X-Internal-Token") != INTERNAL_API_TOKEN:
        return JSONResponse(status_code=403, content={"code": 403, "msg": "forbidden"})
    body = await request.json()
    key = (body.get("key") or "").strip()
    value = (body.get("value") or "").strip()
    if not key:
        return {"code": 400, "msg": "key 不能为空"}
    if key == "kb_max_file_mb" and not (value.isdigit() and 1 <= int(value) <= 100):
        return {"code": 400, "msg": "上传大小限制需为 1-100 的整数（MB）"}
    async with _pool_conn(request.app) as conn:
        await set_platform_setting(conn, key, value)
    logger.info("platform setting updated: %s=%s", key, value)
    return {"code": 0, "msg": "保存成功"}


@router.delete("/agent/kb/documents/{doc_id}")
async def kb_delete(doc_id: str, request: Request):
    tenant_id = tenant_from_request(request)
    try:
        # 取 file_name（删共享卷原文件用）
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "SELECT file_name FROM kb_documents WHERE doc_id = %s AND tenant_id = %s",
                    (doc_id, tenant_id))
                row = await cur.fetchone()
        file_name = row[0] if row else ""
        # 从该租户的 Qdrant 集合按 doc_id 元数据删除
        request.app.state.qdrant_client.delete(
            collection_name=collection_name(tenant_id),
            points_selector=qmodels.FilterSelector(filter=qmodels.Filter(must=[
                qmodels.FieldCondition(key="doc_id", match=qmodels.MatchValue(value=doc_id))
            ])),
        )
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "DELETE FROM kb_documents WHERE doc_id = %s AND tenant_id = %s", (doc_id, tenant_id))
        # 删除共享卷原文件
        delete_kb_file(tenant_id, doc_id, file_name)
        # 知识库变更后失效该租户 retriever 缓存
        request.app.state.tenant_retrievers.pop(tenant_id, None)
        logger.info("KB delete tenant=%s doc_id=%s", tenant_id, doc_id)
        return {"code": 0, "data": {"doc_id": doc_id}}
    except Exception as e:
        logger.exception("kb delete error")
        return {"code": 500, "msg": str(e)}


@router.post("/agent/kb/documents/{doc_id}/category")
async def kb_set_category(doc_id: str, request: Request):
    """设置文档分类"""
    tenant_id = tenant_from_request(request)
    try:
        body = await request.json()
        category = body.get("category", "默认")
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "UPDATE kb_documents SET category = %s, updated_at = CURRENT_TIMESTAMP "
                    "WHERE doc_id = %s AND tenant_id = %s",
                    (category, doc_id, tenant_id))
                if cur.rowcount == 0:
                    return {"code": 404, "msg": "文档不存在"}
        logger.info("KB set category tenant=%s doc_id=%s category=%s", tenant_id, doc_id, category)
        return {"code": 0, "data": {"doc_id": doc_id, "category": category}}
    except Exception as e:
        logger.exception("kb set category error")
        return {"code": 500, "msg": str(e)}


@router.post("/agent/kb/documents/{doc_id}/reindex")
async def kb_reindex(doc_id: str, request: Request):
    """重新索引文档：立即删除旧向量（防检索到旧内容），异步入队重建"""
    tenant_id = tenant_from_request(request)
    try:
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "SELECT title FROM kb_documents WHERE doc_id = %s AND tenant_id = %s",
                    (doc_id, tenant_id))
                row = await cur.fetchone()
                if not row:
                    return {"code": 404, "msg": "文档不存在"}
                # 标记待索引（worker 完成后更新为已索引）
                await cur.execute(
                    "UPDATE kb_documents SET status = 0, error_msg = NULL, updated_at = CURRENT_TIMESTAMP "
                    "WHERE doc_id = %s AND tenant_id = %s",
                    (doc_id, tenant_id))

        # 立即删除旧向量（避免重索引期间检索到旧内容）
        try:
            request.app.state.qdrant_client.delete(
                collection_name=collection_name(tenant_id),
                points_selector=qmodels.FilterSelector(filter=qmodels.Filter(must=[
                    qmodels.FieldCondition(key="doc_id", match=qmodels.MatchValue(value=doc_id))
                ])),
            )
        except Exception:
            pass
        request.app.state.tenant_retrievers.pop(tenant_id, None)

        if not await enqueue_index(request.app, doc_id, tenant_id):
            return {"code": 500, "msg": "索引队列不可用，请稍后重试"}
        logger.info("KB reindex queued tenant=%s doc_id=%s", tenant_id, doc_id)
        return {"code": 0, "data": {"doc_id": doc_id, "status": 0, "msg": "已提交索引任务"}}
    except Exception as e:
        logger.exception("kb reindex error")
        return {"code": 500, "msg": str(e)}


@router.get("/agent/kb/categories")
async def kb_categories(request: Request):
    """获取该租户的所有文档分类（含文档数量）"""
    tenant_id = tenant_from_request(request)
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "SELECT COALESCE(NULLIF(category, ''), '默认') AS cat, COUNT(*) as cnt "
                "FROM kb_documents WHERE tenant_id = %s GROUP BY cat ORDER BY cnt DESC",
                (tenant_id,))
            rows = await cur.fetchall()
    return {"code": 0, "data": [{"category": r[0] or "默认", "count": r[1]} for r in rows]}


@router.post("/agent/kb/index")
async def kb_index(request: Request):
    """手动触发索引（上传后默认不自动索引）：
    body: {"doc_ids": ["..."]} 或 {}（空 = 全部 status IN (0,2) 的文档重新入队）
    入队后 status 置 0、清空 error_msg；worker 消费时置 3（索引中），完成置 1/2。
    """
    tenant_id = tenant_from_request(request)
    try:
        body = await request.json() if request.headers.get("content-type", "").startswith("application/json") else {}
    except Exception:
        body = {}
    doc_ids = [str(d) for d in (body.get("doc_ids") or [])]

    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            if doc_ids:
                await cur.execute(
                    "SELECT doc_id FROM kb_documents WHERE tenant_id = %s AND doc_id = ANY(%s)",
                    (tenant_id, doc_ids))
                owned = {r[0] for r in await cur.fetchall()}
                targets = [d for d in doc_ids if d in owned]
            else:
                await cur.execute(
                    "SELECT doc_id FROM kb_documents WHERE tenant_id = %s AND status IN (0, 2)",
                    (tenant_id,))
                targets = [r[0] for r in await cur.fetchall()]
            if not targets:
                return {"code": 0, "msg": "没有待索引的文档", "data": {"queued": 0, "skipped": len(doc_ids)}}
            await cur.execute(
                "UPDATE kb_documents SET status = 0, error_msg = NULL, updated_at = CURRENT_TIMESTAMP "
                "WHERE tenant_id = %s AND doc_id = ANY(%s)",
                (tenant_id, targets))

    queued = 0
    failed = 0
    for d in targets:
        if await enqueue_index(request.app, d, tenant_id):
            queued += 1
        else:
            failed += 1
            async with _pool_conn(request.app) as conn:
                async with conn.cursor() as cur:
                    await cur.execute(
                        "UPDATE kb_documents SET status = 2, error_msg = %s, updated_at = CURRENT_TIMESTAMP "
                        "WHERE doc_id = %s", ("Redis 索引队列不可用", d))
    # 知识库变更后失效该租户 retriever 缓存
    request.app.state.tenant_retrievers.pop(tenant_id, None)
    logger.info("KB index queued tenant=%s total=%d queued=%d failed=%d", tenant_id, len(targets), queued, failed)
    return {"code": 0, "msg": f"已提交 {queued} 个文档索引入队", "data": {"queued": queued, "failed": failed, "skipped": len(doc_ids) - len(targets)}}


@router.post("/agent/kb/batch")
async def kb_batch(request: Request):
    """知识库批量操作：delete / reindex / category
    body: {"action": "delete|reindex|category", "doc_ids": [...], "category": "..."}
    仅操作归属当前租户的文档（防越权）；reindex 复用单条重索引逻辑。
    """
    tenant_id = tenant_from_request(request)
    try:
        body = await request.json()
    except Exception:
        return {"code": 400, "msg": "请求体需为 JSON"}
    action = (body.get("action") or "").strip()
    doc_ids = [str(d) for d in (body.get("doc_ids") or [])]
    category = (body.get("category") or "").strip()
    if action not in ("delete", "reindex", "category"):
        return {"code": 400, "msg": "action 需为 delete / reindex / category"}
    if not doc_ids:
        return {"code": 400, "msg": "doc_ids 不能为空"}
    if action == "category" and not category:
        return {"code": 400, "msg": "批量分类需提供 category"}

    # 仅保留归属当前租户的文档
    async with _pool_conn(request.app) as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "SELECT doc_id FROM kb_documents WHERE tenant_id = %s AND doc_id = ANY(%s)",
                (tenant_id, doc_ids))
            owned = {r[0] for r in await cur.fetchall()}
    valid = [d for d in doc_ids if d in owned]
    if not valid:
        return {"code": 0, "msg": "没有可操作的文档", "data": {"done": 0, "failed": 0, "skipped": len(doc_ids)}}

    done, failed = 0, 0
    if action == "delete":
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "DELETE FROM kb_documents WHERE tenant_id = %s AND doc_id = ANY(%s)",
                    (tenant_id, valid))
        try:
            request.app.state.qdrant_client.delete(
                collection_name=collection_name(tenant_id),
                points_selector=qmodels.FilterSelector(filter=qmodels.Filter(must=[
                    qmodels.FieldCondition(key="doc_id", match=qmodels.MatchAny(any=valid))
                ])),
            )
        except Exception:
            pass
        request.app.state.tenant_retrievers.pop(tenant_id, None)
        done = len(valid)
    elif action == "category":
        async with _pool_conn(request.app) as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "UPDATE kb_documents SET category = %s, updated_at = CURRENT_TIMESTAMP "
                    "WHERE tenant_id = %s AND doc_id = ANY(%s)",
                    (category, tenant_id, valid))
        done = len(valid)
    else:  # reindex
        for d in valid:
            r = await kb_reindex(d, request)
            if r.get("code") == 0:
                done += 1
            else:
                failed += 1

    logger.info("KB batch %s tenant=%s total=%d done=%d failed=%d", action, tenant_id, len(valid), done, failed)
    return {"code": 0, "msg": f"批量{action}完成", "data": {"done": done, "failed": failed, "skipped": len(doc_ids) - len(valid)}}
