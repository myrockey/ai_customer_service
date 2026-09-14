#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""清洗 LangGraph checkpoints 中重复累积的签名块（多个连续签名合并为 1 个）
仅在 agent 容器内执行：python - < clean_dup_signature_cp.py
"""
import json
import os
import re
import sys

SIG_RE = re.compile(r'(\n\n---\n智能客服 客服中心 \| 工作时间 9:00-18:00)+')
SIG_ONE = '\n\n---\n智能客服 客服中心 | 工作时间 9:00-18:00'

# 与 agent 相同的 PG DSN（容器网络内 hostname=postgres）
dsn = os.getenv("POSTGRES_DSN") or "postgresql://cs_user:cs_pass123@postgres:5432/cs_agent_db"

try:
    import psycopg
except ImportError:
    try:
        import psycopg2 as psycopg
    except ImportError:
        print("no psycopg available")
        sys.exit(1)


def fix_value(o, state):
    """递归清洗 content 字段（支持 LangChain 序列化消息 dict/list/str）"""
    if isinstance(o, dict):
        if isinstance(o.get("content"), str):
            new = SIG_RE.sub(SIG_ONE, o["content"])
            if new != o["content"]:
                o["content"] = new
                state["changed"] = True
        for v in o.values():
            fix_value(v, state)
    elif isinstance(o, list):
        for i in o:
            fix_value(i, state)


def main():
    conn = psycopg.connect(dsn)
    conn.autocommit = True
    cur = conn.cursor()
    cur.execute("SELECT thread_id, checkpoint_id, checkpoint::text FROM checkpoints")
    rows = cur.fetchall()
    total = 0
    for tid, cid, cp_text in rows:
        try:
            cp = json.loads(cp_text)
        except Exception:
            continue
        state = {"changed": False}
        fix_value(cp, state)
        if state["changed"]:
            cur.execute(
                "UPDATE checkpoints SET checkpoint = %s::jsonb WHERE thread_id = %s AND checkpoint_id = %s",
                (json.dumps(cp, ensure_ascii=False), tid, cid),
            )
            total += 1
    print(f"checkpoint 清洗完成：扫描 {len(rows)} 条，修改 {total} 条")
    cur.close()
    conn.close()


if __name__ == "__main__":
    main()
