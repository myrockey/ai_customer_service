# -*- coding: utf-8 -*-
"""并发安全 + 断线自动重连的 AsyncPostgresSaver 包装。

两类问题的根因都是「AsyncPostgresSaver 单连接设计」：
1. 并发写冲突：多租户/多会话并发聊天时，多个 LangGraph 图实例同时 aput/aput_writes
   写同一连接，触发 psycopg "another command is already in progress"。
   → 修复：全局 asyncio.Lock 将 checkpoint 读写串行化（轻量 upsert，开销可忽略）。
2. 连接断线后不可用：checkpointer 持有的是常驻单连接（main.py 启动时 connect 一次），
   PostgreSQL 重启 / 网络中断 / 云 RDS 空闲回收后，该连接进入 closed/broken 状态，
   后续所有聊天在恢复会话历史（aget_tuple）时抛
   psycopg.OperationalError: the connection is closed，服务不可用直到 agent 重启。
   → 修复（两层）：
     a) 活性探测：仅检查 closed/broken 不够——PG 重启瞬间"服务端已 kill、客户端未感知"
        的连接这两个标志都是 False，execute 时才抛 AdminShutdown。因此每次操作前
        执行 SELECT 1 活性探测，探测失败即重建。
     b) 执行级重试：即使探测通过，操作执行期间 PG 仍可能被重启，捕获瞬时段连错误
        （AdminShutdown / OperationalError / InterfaceError）重建连接后重试一次。
"""
import asyncio
import logging

import psycopg
from langgraph.checkpoint.postgres.aio import AsyncPostgresSaver

logger = logging.getLogger("agent.checkpoint_safe")

# 可安全重试的瞬时段连错误（幂等 checkpoint 操作，重试一次无副作用）
_TRANSIENT_DB_EXC = (
    psycopg.errors.AdminShutdown,       # SQLSTATE 57P01：管理员终止连接（PG 重启/terminate）
    psycopg.errors.ConnectionException,  # 08 类：连接异常（无法连接/已关闭）
    psycopg.OperationalError,           # 运行时连接错误（"the connection is closed" 等）
    psycopg.InterfaceError,             # 客户端侧连接状态错误
)


def _is_transient(exc: BaseException) -> bool:
    return isinstance(exc, _TRANSIENT_DB_EXC)


class SafeAsyncPostgresSaver(AsyncPostgresSaver):
    """并发安全 + 断线自动重连包装：aget/aget_tuple/aput/aput_writes 串行化并自愈连接"""

    def __init__(self, conn, dsn: str | None = None, *args, **kwargs):
        super().__init__(conn, *args, **kwargs)
        self._checkpoint_lock = asyncio.Lock()
        self._dsn = dsn or ""          # 断线重连用的 DSN（main.py 传入 POSTGRES_DSN）
        self._reconnect_count = 0      # 重连次数（日志排障用）

    def _conn_ok(self) -> bool:
        """客户端侧连接状态检查（未关闭、未断开）"""
        conn = getattr(self, "conn", None)   # langgraph-checkpoint-postgres 3.x: self.conn
        if conn is None:
            return False
        if getattr(conn, "closed", False):
            return False
        if getattr(conn, "broken", False):
            return False
        return True

    async def _ping(self) -> bool:
        """活性探测：服务端已 kill 但客户端未感知的连接，closed/broken 均为 False，
        execute 才抛 AdminShutdown —— SELECT 1 能提前发现"""
        conn = getattr(self, "conn", None)
        if conn is None:
            return False
        try:
            async with conn.cursor() as cur:
                await cur.execute("SELECT 1")
                await cur.fetchone()
            return True
        except Exception:
            return False

    async def _rebuild(self):
        """用 DSN 重建连接（setup 幂等，可安全重复执行）"""
        if not self._dsn:
            raise RuntimeError(
                "checkpointer 连接已关闭且未配置 dsn（请向 SafeAsyncPostgresSaver 传入 dsn=POSTGRES_DSN），无法自动重连")
        old = getattr(self, "conn", None)
        if old is not None:
            try:
                await old.close()
            except Exception:
                pass
        new_conn = await psycopg.AsyncConnection.connect(self._dsn, autocommit=True)
        self.conn = new_conn                    # 替换连接，_cursor 经 get_connection(self.conn) 全部走新连接
        try:
            await self.setup()
        except Exception:
            # setup 失败（如表已存在/权限问题）不影响使用：连接本身已重建
            logger.warning("checkpointer 重连后 setup 执行失败（可忽略，表已存在）", exc_info=True)
        self._reconnect_count += 1
        logger.warning("⚠️ checkpointer PG 连接已关闭，已自动重建（第 %d 次）", self._reconnect_count)

    async def _ensure_connection(self):
        """操作前确保连接可用：客户端状态检查 + SELECT 1 活性探测，不可用即重建"""
        if self._conn_ok() and await self._ping():
            return
        await self._rebuild()

    async def _with_retry(self, fn, *args, **kwargs):
        """连接自愈 + 瞬时段连错误重试一次：
        探测通过但执行期间 PG 又被重启（AdminShutdown）时，重建连接重试"""
        async with self._checkpoint_lock:
            await self._ensure_connection()
            try:
                return await fn(*args, **kwargs)
            except BaseException as exc:
                if not _is_transient(exc):
                    raise
                logger.warning("checkpointer 操作遇瞬时连接错误（%s），重建连接重试一次",
                               exc.__class__.__name__)
                await self._rebuild()
                return await fn(*args, **kwargs)

    async def aget(self, *args, **kwargs):
        return await self._with_retry(super().aget, *args, **kwargs)

    async def aget_tuple(self, *args, **kwargs):
        return await self._with_retry(super().aget_tuple, *args, **kwargs)

    async def aput(self, *args, **kwargs):
        return await self._with_retry(super().aput, *args, **kwargs)

    async def aput_writes(self, *args, **kwargs):
        return await self._with_retry(super().aput_writes, *args, **kwargs)

    async def adelete_thread(self, *args, **kwargs):
        return await self._with_retry(super().adelete_thread, *args, **kwargs)
