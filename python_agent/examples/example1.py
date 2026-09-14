import os
from dotenv import load_dotenv
import psycopg_pool
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from langchain.tools import tool
from langchain.agents import create_agent
from langchain.chat_models import init_chat_model
from langgraph.checkpoint.postgres import PostgresSaver

load_dotenv()
dsn = os.getenv("POSTGRES_DSN")

# --------------------------
# 全局单例：连接池、checkpointer、agent（只初始化一次）
# --------------------------
pool: psycopg_pool.ConnectionPool | None = None
checkpointer: PostgresSaver | None = None
agent = None

# 大模型配置
api_key = os.getenv("AGNES_API_KEY")
base_url = os.getenv("AGNES_BASE_URL")
base_model = os.getenv("AGNES_MODEL")

# app = FastAPI(title="Agent Postgres Checkpoint Demo")


# 示例工具，用于Agent工具调用测试
@tool
def get_current_time() -> str:
    """获取当前系统时间"""
    from datetime import datetime
    return datetime.now().strftime("%Y-%m-%d %H:%M:%S")


tools = [get_current_time]

from contextlib import asynccontextmanager

@asynccontextmanager
async def lifespan(app: FastAPI):
    global pool, checkpointer, agent
    # 启动：初始化连接池
    pool = psycopg_pool.ConnectionPool(
        conninfo=dsn,
        min_size=2,
        max_size=10,
    )
    checkpointer = PostgresSaver(pool)

    model = init_chat_model(
        model=base_model,
        model_provider="openai",
        temperature=0,
        max_tokens=1024,
        timeout=30,
        max_retries=2,
        api_key=api_key,
        base_url=base_url,
    )

    tools = [get_current_time]
    agent = create_agent(
        model=model,
        tools=tools,
        checkpointer=checkpointer,   # 在这里传入！！
        system_prompt="""你是菜鸟教程 RUNOOB 的智能客服"小菜"。
可以回答用户问题，可以调用工具获取当前时间。记住用户告诉你的信息。"""
    )

    yield

    # 关闭服务，释放连接池
    if pool is not None:
        pool.close()



app = FastAPI(title="Agent Postgres Checkpoint Demo", lifespan=lifespan)

# @app.on_event("startup")
# async def startup_event():
#     """服务启动：初始化连接池、checkpointer、agent"""
#     global pool, checkpointer, agent

#     # 创建pg连接池，全局单例
#     pool = psycopg_pool.ConnectionPool(
#         conninfo=dsn,
#         min_size=2,
#         max_size=10,
#     )

#     checkpointer = PostgresSaver(pool)

#     # 初始化大模型（中转兼容openai接口）
#     model = init_chat_model(
#         model=base_model,
#         model_provider="openai",
#         temperature=0,
#         max_tokens=1024,
#         timeout=30,
#         max_retries=2,
#         api_key=api_key,
#         base_url=base_url,
#     )

#     # create_agent：langgraph高层封装agent，直接传入checkpointer实现会话持久化
#     agent = create_agent(
#         model=model,
#         tools=tools,
#         checkpointer=checkpointer,
#         system_prompt="""你是菜鸟教程 RUNOOB 的智能客服"小菜"。
# 可以回答用户问题，可以调用工具获取当前时间。记住用户告诉你的信息。"""
#     )


# @app.on_event("shutdown")
# async def shutdown_event():
#     """服务优雅关闭，释放pg连接"""
#     global pool
#     if pool is not None:
#         pool.close()


# --------------------------
# 请求结构体
# --------------------------
class ChatRequest(BaseModel):
    thread_id: str  # 用户会话ID，前端传入，每个会话唯一
    user_input: str


# 对话接口
@app.post("/chat")
async def chat(req: ChatRequest):
    if agent is None:
        raise HTTPException(status_code=500, detail="Agent尚未初始化")

    # ✅ 每个请求使用传入的thread_id，实现多用户会话隔离
    config = {
        "configurable": {
            "thread_id": req.thread_id
        }
    }

    try:
        resp = agent.invoke(
            {"messages": [("human", req.user_input)]},
            config=config
        )
        messages = resp["messages"]
        ai_msg = messages[-1]

        return {
            "thread_id": req.thread_id,
            "reply": ai_msg.content,
            "full_messages": [msg.model_dump() for msg in messages]
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


# 调试接口：直接查询Postgres checkpoints表验证落库
@app.get("/checkpoint/{thread_id}")
async def get_checkpoint(thread_id: str):
    if pool is None:
        raise HTTPException(status_code=500, detail="数据库连接池未初始化")

    with pool.connection() as conn:
        cur = conn.cursor()
        cur.execute("""
            SELECT thread_id, checkpoint_ns, checkpoint_id, parent_checkpoint_id, metadata
            FROM checkpoints
            WHERE thread_id = %s AND checkpoint_ns = '';
        """, (thread_id,))
        rows = cur.fetchall()
        result = []
        for row in rows:
            result.append({
                "thread_id": row[0],
                "checkpoint_ns": row[1],
                "checkpoint_id": row[2],
                "parent_checkpoint_id": row[3],
                "metadata": row[4]
            })
        return {"data": result}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)


"""
请求示例：
curl -X POST http://127.0.0.1:8000/chat -H "Content-Type:application/json" -d "{\"thread_id\":\"user_0001\",\"user_input\":\"我叫张三，请记住我的名字\"}"
curl -X POST http://127.0.0.1:8000/chat -H "Content-Type:application/json" -d "{\"thread_id\":\"user_0001\",\"user_input\":\"我叫什么名字\"}"

查询：浏览器访问
http://127.0.0.1:8000/checkpoint/user_0001

"""