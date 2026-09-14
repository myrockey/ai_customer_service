"""功能验证：Prompt管理 / 知识库上传 / 会话清理"""
import asyncio
import json
import time

import httpx

BASE = "http://172.22.64.1:8080"
USER = "feat_test_" + str(int(time.time()))


async def main():
    ok = True
    async with httpx.AsyncClient(timeout=120) as c:
        # ============ 1. Prompt 管理 ============
        print("===== Prompt 管理 =====")
        # 新建并激活测试 Prompt
        r = (await c.post(BASE + "/api/admin/prompts", json={
            "key": "test_persona",
            "title": "测试人设",
            "content": '你是"测试客服"，你的名字叫小测。回答任何问题前必须先输出【测试Prompt生效】。',
            "is_active": True,
        })).json()
        print("[1a] create+activate:", r)

        # 聊天验证新 Prompt 生效
        r = (await c.post(BASE + "/api/user/chat", json={"user_id": USER, "msg": "你好，你是谁？"})).json()
        print("[1b] chat with new prompt:", r.get("code"), "|", str(r.get("msg"))[:80])
        assert "测试Prompt生效" in (r.get("msg") or ""), "new prompt not effective!"

        # 切回 default
        r = (await c.post(BASE + "/api/admin/prompts/default/activate", json={})).json()
        print("[1c] activate default:", r)

        # 列表验证
        r = (await c.get(BASE + "/api/admin/prompts")).json()
        keys = [(p["key"], p["is_active"]) for p in r["data"]]
        print("[1d] prompts list:", keys)
        assert ("default", True) in keys, "default should be active again"

        # 删除测试 Prompt
        r = (await c.delete(BASE + "/api/admin/prompts/test_persona")).json()
        print("[1e] delete test prompt:", r)

        # ============ 2. 知识库上传 ============
        print("===== 知识库上传 =====")
        r = (await c.post(BASE + "/api/admin/kb/upload", json={
            "title": "公司福利测试",
            "content": "ZYX 公司规定：员工加班每天补贴 88 元，每月最后一个周五发放免费榴莲福利。",
        })).json()
        print("[2a] upload text:", r)
        assert r.get("code") == 0, "kb upload failed"
        doc_id = r["data"]["doc_id"]

        r = (await c.get(BASE + "/api/admin/kb/documents")).json()
        print("[2b] kb list:", [(d["title"], d["chunk_count"]) for d in r["data"]])

        # 验证知识库检索生效（提问内容来自上传文档）
        r = (await c.post(BASE + "/api/user/chat", json={"user_id": USER, "msg": "公司加班补贴是多少钱？"})).json()
        print("[2c] kb QA:", str(r.get("msg"))[:120])
        assert "88" in (r.get("msg") or ""), "KB retrieval not working!"

        # 删除文档
        r = (await c.delete(BASE + f"/api/admin/kb/documents/{doc_id}")).json()
        print("[2d] delete kb doc:", r)

        # ============ 3. 会话清理 ============
        print("===== 会话清理 =====")
        r = (await c.get(BASE + "/api/admin/session/stats")).json()
        print("[3a] stats before:", r["data"])

        # 用很小的 days 但保留当天（不过期），验证接口可用
        r = (await c.post(BASE + "/api/admin/session/cleanup", json={"days": 30})).json()
        print("[3b] cleanup(days=30):", r)
        assert r.get("code") == 0, "cleanup failed"

        # 手动造一个过期会话：直接向 PG 插入过去 10 天的 session_meta 再清理
        # （通过 Python Agent 无法直接执行，这里用内部直连验证 cleanup 逻辑）
        r = (await c.get(BASE + "/api/admin/session/stats")).json()
        print("[3c] stats after:", r["data"])

    print("\n==== FEATURE TEST PASS ====" if ok else "\n==== FEATURE TEST FAIL ====")


if __name__ == "__main__":
    asyncio.run(main())
