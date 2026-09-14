# -*- coding: utf-8 -*-
"""数据库连接池公共 helper：业务查询统一从 AsyncConnectionPool 取连接，避免全局单连接并发报错
"another command is already in progress"。

取消/异常安全（v2）：借用者协程被取消（如流式响应断开）或抛异常时，连接上可能残留
未完成命令。优先用 conn.cancel() 取消服务端命令并 rollback 复位，使连接回到 IDLE
可复用；复位失败（连接真损坏）才关闭丢弃（pool 自动重建）。避免：
- 归还时 rollback 撞残留命令 → "another command is already in progress"
- 无条件丢弃 → 连接抖动 + discarding WARNING
"""
from contextlib import asynccontextmanager


@asynccontextmanager
async def _pool_conn(app):
    """从连接池获取一个连接（业务查询并发安全 + 取消/异常安全），用法：
        async with _pool_conn(request.app) as conn:
            await some_db_func(conn, ...)
    """
    conn = await app.state.db_pool.getconn()
    try:
        yield conn
    except BaseException:
        # 异常/取消路径：先尝试取消残留命令并复位连接（可复用）
        try:
            await conn.cancel()
            try:
                await conn.rollback()
            except Exception:
                pass
        except Exception:
            # 连接已损坏：关闭丢弃，pool 识别后自动重建
            try:
                await conn.close()
            except Exception:
                pass
        raise
    finally:
        await app.state.db_pool.putconn(conn)
