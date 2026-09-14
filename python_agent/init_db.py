import os
from dotenv import load_dotenv
from langgraph.checkpoint.postgres import PostgresSaver

load_dotenv()

dsn = os.getenv("POSTGRES_DSN")
print("POSTGRES_DSN=", dsn)

# 重点：不要写 with！直接赋值
# with模式仅用于一次性初始化脚本，不适合全局变量
with PostgresSaver.from_conn_string(dsn) as cp:
    cp.setup()
print("表初始化完成")