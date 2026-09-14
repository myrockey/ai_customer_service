import os
# 强制utf‑8，解决httpx2 header ascii编码报错
os.environ["PYTHONIOENCODING"] = "utf‑8"
os.environ["LANG"] = "en_US.UTF‑8"
os.environ["LC_ALL"] = "en_US.UTF‑8"

from dotenv import load_dotenv
load_dotenv("../.env")

import logging
from typing import Optional
from contextlib import asynccontextmanager

import httpx
import psycopg
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field

from langchain.tools import tool
from langchain.agents import create_agent
from langchain.agents.middleware import before_model, after_model
from langchain.chat_models import init_chat_model
from langchain_core.messages import HumanMessage, AIMessage
from langchain_openai import OpenAIEmbeddings
from langchain_chroma import Chroma
from langchain_text_splitters import RecursiveCharacterTextSplitter
# ✅使用异步Checkpointer
from langgraph.checkpoint.postgres.aio import AsyncPostgresSaver
from langgraph.types import interrupt, Command

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# ====================== 常量配置 ======================
PERSIST_CHROMA_DIR = "./chroma_db"
GO_BUSINESS_API = os.getenv("GO_BUSINESS_API", "http://127.0.0.1:8080")
# 生产环境置为True，屏蔽接口原始异常详情
PROD_MODE = False

knowledge_base = [
    "菜鸟教程 RUNOOB 创立于 2013 年，是国内领先的免费编程学习平台。",
    "平台提供 300+ 套教程，涵盖 Python、Java、HTML、CSS、JavaScript 等。",
    "Python3 基础教程共 30 章，累计学习人次超 500 万。课程完全免费。",
    "VIP 会员费用为 ¥99/月，¥799/年，包含视频课程和一对一答疑服务。",
    "退款政策：购买 7 天内且在 3 节课以内可全额退款。",
    "平台支持在线编程环境，无需安装任何软件即可编写运行代码。",
    "客服工作时间：周一至周五 9:00-18:00，周末 10:00-16:00。",
]

# ====================== Tools 全部改为异步工具 @tool ======================
@tool
async def search_kb(query: str) -> str:
    """搜索菜鸟教程知识库，获取关于平台、课程、政策等官方信息。
    Args:
        query: 搜索问题或关键词
    """
    retriever = app.state.retriever
    docs = await retriever.ainvoke(query)
    if not docs:
        return "未找到相关信息，建议转接人工客服。"
    return "\n".join(f"- {doc.page_content}" for doc in docs)


@tool
async def query_order(order_id: str) -> str:
    """根据订单号查询订单状态和详情，调用Go业务系统获取真实订单。
    Args:
        order_id: 订单号，如 ORD-2024-001
    """
    http_client = app.state.http_client
    try:
        resp = await http_client.get(f"{GO_BUSINESS_API}/api/business/order?order_id={order_id}", timeout=5)
        resp.raise_for_status()
        data = resp.json()
        return str(data.get("data", "未获取订单数据"))
    except Exception as e:
        logger.warning(f"query_order error: {e}")
        return f"查询订单业务异常：{str(e)}"


@tool
async def transfer_to_human(reason: str) -> str:
    """将用户转接给人工客服，触发LangGraph HITL中断。
    Args:
        reason: 转接原因
    """
    approval = interrupt({
        "action": "transfer_to_human",
        "reason": reason,
        "message": f"用户请求转接人工客服，原因：{reason}。是否转接？"
    })
    if approval.get("confirmed"):
        return (f"已为您转接人工客服，预计等待 {approval.get('wait_time', 3)} 分钟。"
                f"工单号：TK-{approval.get('ticket_id', 'N/A')}")
    return "转接已取消，我继续为您服务。"


# ====================== Middleware ======================
@before_model
def content_guard(state, runtime):
    last_msg = state["messages"][-1] if state.get("messages") else None
    if not last_msg:
        return None
    content = str(getattr(last_msg, 'content', ''))
    blocked = ["黄X", "X博", "违法"]
    for word in blocked:
        if word in content:
            return {
                "jump_to": "end",
                "messages": [HumanMessage(content="抱歉，我不能处理这个请求。")]
            }
    return None


@after_model
def auto_signature(state, runtime):
    msgs = state.get("messages", [])
    if not msgs:
        return None
    last = msgs[-1]
    if last.type == "ai" and last.content and not (
        hasattr(last, 'tool_calls') and last.tool_calls
    ):
        return {"messages": [AIMessage(
            id=last.id,
            content=last.content
            + "\n\n---\n菜鸟教程 RUNOOB 客服中心 | 工作时间 9:00‑18:00"
        )]}
    return None


# ====================== FastAPI Lifespan 启动/销毁 ======================
@asynccontextmanager
async def lifespan(app: FastAPI):
    # 初始化state占位
    app.state.db_conn: Optional[psycopg.AsyncConnection] = None
    app.state.checkpointer: Optional[AsyncPostgresSaver] = None
    app.state.vector_store: Optional[Chroma] = None
    app.state.retriever = None
    app.state.agent = None
    app.state.http_client: Optional[httpx.AsyncClient] = None

    dsn = os.getenv("POSTGRES_DSN")
    logger.info("==== Agent Service starting ====")

    try:
        # 1. 初始化异步Postgres连接 & AsyncPostgresSaver
        app.state.db_conn = await psycopg.AsyncConnection.connect(dsn, autocommit=True)
        app.state.checkpointer = AsyncPostgresSaver(app.state.db_conn)
        await app.state.checkpointer.setup()
        logger.info("✅ AsyncPostgresSaver checkpointer setup done")

        # 2. 初始化向量库
        embeddings = OpenAIEmbeddings(
            model=os.getenv("DASHSCOPE_Embeddings_MODEL"),
            api_key=os.getenv("DASHSCOPE_API_KEY"),
            base_url=os.getenv("DASHSCOPE_BASE_URL"),
            check_embedding_ctx_length=False,
            chunk_size=10,
        )
        os.makedirs(PERSIST_CHROMA_DIR, exist_ok=True)
        text_splitter = RecursiveCharacterTextSplitter(
            chunk_size=200,
            chunk_overlap=30
        )
        if not knowledge_base:
            raise RuntimeError("knowledge_base 知识库数据为空，请检查业务配置！")

        has_exist_chroma_db = os.path.exists(PERSIST_CHROMA_DIR) and len(os.listdir(PERSIST_CHROMA_DIR)) > 0
        if has_exist_chroma_db:
            logger.info(f"尝试加载已存在Chroma向量库, path={PERSIST_CHROMA_DIR}")
            try:
                app.state.vector_store = Chroma(
                    persist_directory=PERSIST_CHROMA_DIR,
                    embedding_function=embeddings
                )
                logger.info("✅ Chroma向量库加载成功")
            except Exception as e:
                logger.critical(f"❌ Chroma向量库加载失败！数据库文件可能损坏，禁止自动重建。path={PERSIST_CHROMA_DIR}", exc_info=True)
                raise RuntimeError("Chroma persistent database load failed, please check db file manually") from e
        else:
            logger.info("🔨 Chroma向量库不存在，开始构建知识库")
            chunks = text_splitter.create_documents(knowledge_base)
            logger.info(f"知识库分片完成，分片总数: {len(chunks)}")
            if len(chunks) == 0:
                raise RuntimeError("知识库分片结果为空，请检查knowledge_base文本内容")
            app.state.vector_store = Chroma.from_documents(
                documents=chunks,
                embedding=embeddings,
                persist_directory=PERSIST_CHROMA_DIR
            )
            logger.info("✅ Chroma向量库首次构建完成")

        app.state.retriever = app.state.vector_store.as_retriever(
            search_type="similarity",
            search_kwargs={"k": 3}
        )

        # 3. 异步http客户端
        app.state.http_client = httpx.AsyncClient(timeout=10.0)

        # 4. 初始化大模型
        api_key = os.getenv("AGNES_API_KEY")
        base_url = os.getenv("AGNES_BASE_URL")
        base_model = os.getenv("AGNES_MODEL")
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

        # 5. 创建Agent实例
        app.state.agent = create_agent(
            model=model,
            tools=[search_kb, query_order, transfer_to_human],
            middleware=[content_guard, auto_signature],
            checkpointer=app.state.checkpointer,
            system_prompt="""你是菜鸟教程 RUNOOB 的智能客服"小菜"。
## 你的职责
1. 热情接待每一位用户，用"您"称呼
2. 关于平台信息、课程内容、政策等问题，使用 search_kb 查询
3. 关于订单查询，使用 query_order 工具
4. 遇到无法解决的问题，使用 transfer_to_human 转接人工
## 行为准则
- 回答简洁，每次 2‑3 句话
- 不知道的就查询知识库，查不到就诚实告知
- 保持友好亲切的语气""",
        )
        logger.info("✅ Agent init complete, service ready")

    except Exception as e:
        logger.exception("❌ 服务启动初始化失败")
        raise

    yield

    # ============ 服务关闭：释放全部资源 ============
    logger.info("==== Agent Service shutdown, release resources ====")
    if app.state.http_client is not None:
        await app.state.http_client.aclose()
    if app.state.db_conn is not None:
        await app.state.db_conn.close()


app = FastAPI(title="CustomerServiceAgent Internal Service", lifespan=lifespan)


# ====================== Pydantic Schema ======================
class ChatRequest(BaseModel):
    thread_id: str = Field(min_length=1, description="会话ID")
    user_message: str = Field(min_length=1, description="用户消息")


class ResumeRequest(BaseModel):
    thread_id: str = Field(min_length=1)
    confirmed: bool
    wait_time: int = Field(ge=0)
    ticket_id: str


class AgentResp(BaseModel):
    success: bool
    is_interrupt: bool
    interrupt_data: Optional[dict] = None
    content: Optional[str] = None


# ====================== API接口 ======================
@app.post("/agent/chat", response_model=AgentResp)
async def agent_chat(req: ChatRequest, request: Request):
    agent = request.app.state.agent
    if agent is None:
        raise HTTPException(status_code=500, detail="Agent尚未初始化")

    thread_id = req.thread_id.strip()
    user_message = req.user_message.strip()
    if not thread_id or not user_message:
        return AgentResp(
            success=False,
            is_interrupt=False,
            content="thread_id 和 user_message 不能为空"
        )
    config = {"configurable": {"thread_id": thread_id}}
    try:
        result = await agent.ainvoke(
            {"messages": [HumanMessage(content=user_message)]},
            config=config
        )
        messages = result.get("messages", [])
        if not messages:
            return AgentResp(success=False, is_interrupt=False, content="Agent执行完成，但未返回消息")

        state = await agent.aget_state(config)
        if state.tasks and state.tasks[0].interrupts:
            interrupt_info = state.tasks[0].interrupts[0].value
            return AgentResp(
                success=True,
                is_interrupt=True,
                interrupt_data=interrupt_info,
                content=None
            )
        last_msg = messages[-1]
        content = last_msg.content if hasattr(last_msg, "content") else ""
        return AgentResp(
            success=True,
            is_interrupt=False,
            content=content
        )
    except Exception as e:
        logger.exception("/agent/chat error")
        err_msg = "agent服务内部异常" if PROD_MODE else f"agent error:{str(e)}"
        return AgentResp(success=False, is_interrupt=False, content=err_msg)


@app.post("/agent/resume", response_model=AgentResp)
async def agent_resume(req: ResumeRequest, request: Request):
    agent = request.app.state.agent
    if agent is None:
        raise HTTPException(status_code=500, detail="Agent尚未初始化")

    config = {"configurable": {"thread_id": req.thread_id.strip()}}
    try:
        result = await agent.ainvoke(
            Command(resume={
                "confirmed": req.confirmed,
                "wait_time": req.wait_time,
                "ticket_id": req.ticket_id,
            }),
            config=config
        )
        messages = result.get("messages", [])
        if not messages:
            return AgentResp(success=False, is_interrupt=False, content="resume执行完成无返回消息")
        last_msg = messages[-1]
        content = last_msg.content if hasattr(last_msg, "content") else ""
        return AgentResp(
            success=True,
            is_interrupt=False,
            content=content
        )
    except Exception as e:
        logger.exception("/agent/resume error")
        err_msg = "resume服务内部异常" if PROD_MODE else f"resume error:{str(e)}"
        return AgentResp(success=False, is_interrupt=False, content=err_msg)


# 调试接口，查询checkpoint数据表（异步版本）
@app.get("/agent/checkpoint/{thread_id}")
async def get_checkpoint(thread_id: str, request: Request):
    conn = request.app.state.db_conn
    if conn is None:
        raise HTTPException(status_code=500, detail="数据库连接未初始化")

    async with conn.cursor() as cur:
        await cur.execute("""
            SELECT thread_id, checkpoint_ns, checkpoint_id, parent_checkpoint_id, metadata
            FROM checkpoints
            WHERE thread_id = %s AND checkpoint_ns = '';
        """, (thread_id,))
        rows = await cur.fetchall()
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
