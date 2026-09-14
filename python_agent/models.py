"""
P4-3：Pydantic 数据模型模块
从 main.py 拆分，集中管理所有 API 请求/响应模型。
"""
from typing import Optional
from pydantic import BaseModel, Field


class ChatRequest(BaseModel):
    """对话请求"""
    thread_id: str = Field(min_length=1, description="会话ID")
    user_message: str = Field(min_length=1, description="用户消息")
    has_pending_ticket: bool = Field(default=False, description="线程存在待处理工单（未接管），agent 不再重复触发转人工中断")


class ResumeRequest(BaseModel):
    """恢复人工接管请求"""
    thread_id: str = Field(min_length=1)
    confirmed: bool
    wait_time: int = Field(ge=0)
    ticket_id: str


class AgentResp(BaseModel):
    """Agent 响应"""
    success: bool
    is_interrupt: bool
    interrupt_data: Optional[dict] = None
    content: Optional[str] = None


class SessionCleanupReq(BaseModel):
    """会话清理请求"""
    days: int = Field(default=7, ge=1, le=365, description="回收多少天前未活跃的会话")


class TenantWarmReq(BaseModel):
    """租户预热请求"""
    tenant_id: str = Field(min_length=1, description="要预热的租户 ID")


class PromptReq(BaseModel):
    """Prompt 管理请求"""
    key: str = Field(min_length=1, description="Prompt标识")
    title: str = ""
    content: str = Field(min_length=1, description="Prompt内容")
    is_active: bool = False
