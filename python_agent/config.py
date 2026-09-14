"""
P4-3：配置管理模块
从 main.py 拆分，集中管理所有环境变量和常量配置。
"""
import os


def get_env_or_raise(key: str) -> str:
    """获取环境变量，未配置则抛出异常"""
    value = os.getenv(key)
    if not value:
        raise RuntimeError(f"环境变量 {key} 未配置，请检查 .env 文件")
    return value


# ====================== 数据库 & 外部服务 ======================
POSTGRES_DSN = get_env_or_raise("POSTGRES_DSN")
QDRANT_URL = get_env_or_raise("QDRANT_URL")
QDRANT_COLLECTION_NAME = "customer_service_knowledge"
GO_BUSINESS_API = os.getenv("GO_BUSINESS_API", "http://127.0.0.1:8080")

# 内部接口鉴权 Token：调用 Go 后端 /api/agent/* 接口时必须携带
INTERNAL_API_TOKEN = os.getenv("INTERNAL_API_TOKEN", "")

# ====================== 模型配置 ======================
# 【模型配置单一数据源】模型参数统一由后台「模型提供商/租户模型配置」维护（DB），
# 不从 .env 读取模型 Key/Base/Model（旧 AGNES/DASHSCOPE 环境变量迁移桥已删除）。

# 生产环境置为True，屏蔽接口原始异常详情
PROD_MODE = os.getenv("PROD_MODE", "false").lower() == "true"

# ====================== 会话回收配置 ======================
# 过期天数 / 定时清理间隔(秒, 0=关闭)
SESSION_RETENTION_DAYS = int(os.getenv("SESSION_RETENTION_DAYS", "7"))
SESSION_CLEANUP_INTERVAL = int(os.getenv("SESSION_CLEANUP_INTERVAL", str(6 * 3600)))

TEXT_SPLITTER_CHUNK_SIZE = 200
TEXT_SPLITTER_CHUNK_OVERLAP = 30
RETRIEVER_TOP_K = 3

# ====================== 默认 Prompt ======================
DEFAULT_SYSTEM_PROMPT = """你是智能客服系统 的智能客服"小Q"。
## 你的职责
1. 热情接待每一位用户，用"您"称呼
2. 关于平台信息、课程内容、政策等问题，使用 search_kb 查询
3. 关于订单查询，使用 query_order 工具
4. 遇到无法解决的问题，使用 transfer_to_human 转接人工
## 行为准则
- 回答简洁，每次 2-3 句话
- 不知道的就查询知识库，查不到就诚实告知
- 保持友好亲切的语气
## 服务信息
- 每次回复结尾必须附上一行：智能客服 客服中心 | 工作时间 9:00-18:00"""
