# -*- coding: utf-8 -*-
"""并发安全的 AsyncPostgresSaver 包装。

根因：AsyncPostgresSaver 是单连接设计（内部共用 self._connection），
多租户/多会话并发聊天时，多个 LangGraph 图实例同时 aput/aput_writes 写同一连接，
触发 psycopg "another command is already in progress"（WARNING/偶发接口报错）。

修复：全局 asyncio.Lock 将 checkpoint 写入串行化。
checkpoint 写入是轻量操作（单行 upsert），串行开销可忽略。
"""
import asyncio
import logging

from langgraph.checkpoint.postgres.aio import AsyncPostgresSaver

logger = logging.getLogger("agent.checkpoint_safe")


class SafeAsyncPostgresSaver(AsyncPostgresSaver):
    """并发安全包装：aput / aput_writes 串行化"""

    def __init__(self, conn, *args, **kwargs):
        super().__init__(conn, *args, **kwargs)
        self._checkpoint_lock = asyncio.Lock()

    async def aput(self, *args, **kwargs):
        async with self._checkpoint_lock:
            return await super().aput(*args, **kwargs)

    async def aput_writes(self, *args, **kwargs):
        async with self._checkpoint_lock:
            return await super().aput_writes(*args, **kwargs)
