"""端到端验证脚本：WebSocket 聊天 + 人工客服接管全流程"""
import asyncio
import json
import time

import httpx
import websockets

BASE = "http://172.22.64.1:8080"
WS = "ws://172.22.64.1:8080"
USER = "ws_test_" + str(int(time.time()))


async def recv_until(ws, want_types, timeout=60):
    """接收消息直到命中指定 type 之一，返回该消息"""
    end = time.time() + timeout
    got = []
    while time.time() < end:
        remain = max(0.5, end - time.time())
        try:
            raw = await asyncio.wait_for(ws.recv(), remain)
        except asyncio.TimeoutError:
            break
        try:
            m = json.loads(raw)
        except Exception:
            continue
        got.append(m)
        if m.get("type") in want_types:
            return m, got
    return None, got


async def main():
    ok = True
    async with websockets.connect(WS + "/ws/admin") as aws:
        async with websockets.connect(WS + f"/ws/chat?user_id={USER}") as uws:
            # 1. 知识库问答
            await uws.send(json.dumps({"type": "chat", "msg": "你们有哪些课程？"}))
            m, _ = await recv_until(uws, {"chat_reply", "error"})
            print("[1 chat_reply]", m.get("type"), "|", str(m.get("msg"))[:80])
            assert m and m.get("type") == "chat_reply", "chat_reply failed"

            # 2. 触发转人工
            await uws.send(json.dumps({"type": "chat", "msg": "我要转人工客服，请帮我处理退款"}))
            m, _ = await recv_until(uws, {"ticket", "chat_reply", "error"})
            print("[2 ticket]", m.get("type"), "| ticket_no:", m.get("ticket_no"), "|", str(m.get("msg"))[:60])
            assert m and m.get("type") == "ticket", "ticket not created"
            ticket_no = m.get("ticket_no")

            # 3. 管理端查工单
            async with httpx.AsyncClient(timeout=60) as client:
                r = (await client.get(BASE + f"/api/admin/tickets?status=0&page=1&size=20")).json()
                tk = next((t for t in r["data"]["list"] if t["ticket_no"] == ticket_no), None)
                print("[3 ticket found]", None if not tk else {"id": tk["id"], "status": tk["status"]})
                assert tk, "ticket not in admin list"
                tid = tk["id"]

                # 4. 会话查看
                msgs = (await client.get(BASE + f"/api/admin/tickets/{tid}/messages")).json()
                roles = [(x["role"], x["content"][:30]) for x in msgs["data"]]
                print("[4 session messages]", roles)
                assert any(x[0] == "user" for x in roles), "no user msg stored"

                # 5. 人工接管
                rr = (await client.post(BASE + f"/api/admin/tickets/{tid}/takeover", json={"wait_time": 3})).json()
                print("[5 takeover]", rr.get("msg", "")[:80])
                assert rr.get("code") == 0, "takeover failed"

            # 用户端应收到接管通知
            m, _ = await recv_until(uws, {"human_notice", "error"}, timeout=30)
            print("[5b user notice]", m.get("type"), "|", str(m.get("msg"))[:60])

            # 6. 人工接管中，用户发消息 → 应转给管理端
            # 先排空管理端 WS 缓冲中的旧消息
            await asyncio.sleep(0.5)
            while True:
                try:
                    await asyncio.wait_for(aws.recv(), 0.3)
                except asyncio.TimeoutError:
                    break
                except Exception:
                    break
            await uws.send(json.dumps({"type": "chat", "msg": "人工客服你好，我想全额退款"}))
            m, _ = await recv_until(aws, {"user_msg", "ticket_update", "new_ticket"}, timeout=30)
            print("[6 admin ws]", m.get("type"), "|", str(m.get("msg"))[:60])
            assert m and m.get("type") == "user_msg", "user msg not routed to admin"

            # 7. 人工回复 → 用户端收到 human_reply
            async with httpx.AsyncClient(timeout=60) as client:
                rr = (await client.post(BASE + f"/api/admin/tickets/{tid}/reply",
                                        json={"content": "您好，您的退款申请已受理，3个工作日内原路退回。"})).json()
                print("[7 reply]", rr)
                assert rr.get("code") == 0, "reply failed"
            m, _ = await recv_until(uws, {"human_reply", "error"}, timeout=30)
            print("[7b user human_reply]", m.get("type"), "|", str(m.get("msg"))[:60])
            assert m and m.get("type") == "human_reply", "human_reply not delivered to user"

            # 8. 关闭工单 → 用户收到通知
            async with httpx.AsyncClient(timeout=60) as client:
                rr = (await client.post(BASE + f"/api/admin/tickets/{tid}/close", json={})).json()
                print("[8 close]", rr)
            m, _ = await recv_until(uws, {"human_notice", "error"}, timeout=30)
            print("[8b user close notice]", m.get("type"), "|", str(m.get("msg"))[:60])

            # 9. 会话消息落库验证
            async with httpx.AsyncClient(timeout=60) as client:
                msgs = (await client.get(BASE + f"/api/admin/tickets/{tid}/messages")).json()
                roles = [x["role"] for x in msgs["data"]]
                print("[9 final roles]", roles)
                assert "human" in roles and "system" in roles, "human/system msgs missing"

    print("\n==== E2E PASS ====" if ok else "\n==== E2E FAIL ====")


if __name__ == "__main__":
    asyncio.run(main())
