"""
结构化日志模块
输出 JSON 格式日志，包含 trace_id、tenant_id 等字段，便于 Loki 收集
"""
import json
import logging
import sys
import time
from typing import Any, Dict, Optional


class JSONFormatter(logging.Formatter):
    """JSON 格式日志格式化器"""

    def format(self, record: logging.LogRecord) -> str:
        log_entry = {
            "timestamp": self.formatTime(record, self.datefmt),
            "level": record.levelname.lower(),
            "message": record.getMessage(),
            "logger": record.name,
        }

        # 添加额外字段
        if hasattr(record, "trace_id"):
            log_entry["trace_id"] = record.trace_id
        if hasattr(record, "tenant_id"):
            log_entry["tenant_id"] = record.tenant_id
        if hasattr(record, "user_id"):
            log_entry["user_id"] = record.user_id
        if hasattr(record, "extra_fields"):
            log_entry.update(record.extra_fields)

        # 异常信息
        if record.exc_info:
            log_entry["exception"] = self.formatException(record.exc_info)

        return json.dumps(log_entry, ensure_ascii=False)


class ContextLogger:
    """带上下文的日志包装器，支持 trace_id、tenant_id 等字段"""

    def __init__(self, logger: logging.Logger, **context):
        self._logger = logger
        self._context = context

    def _with_context(self, **kwargs) -> Dict[str, Any]:
        extra = dict(self._context)
        extra.update(kwargs)
        return {"extra": extra}

    def info(self, msg: str, **kwargs):
        self._logger.info(msg, **self._with_context(**kwargs))

    def warn(self, msg: str, **kwargs):
        self._logger.warning(msg, **self._with_context(**kwargs))

    def warning(self, msg: str, **kwargs):
        self._logger.warning(msg, **self._with_context(**kwargs))

    def error(self, msg: str, **kwargs):
        self._logger.error(msg, **self._with_context(**kwargs))

    def debug(self, msg: str, **kwargs):
        self._logger.debug(msg, **self._with_context(**kwargs))

    def critical(self, msg: str, **kwargs):
        self._logger.critical(msg, **self._with_context(**kwargs))

    def exception(self, msg: str, **kwargs):
        self._logger.exception(msg, **self._with_context(**kwargs))

    def with_context(self, **kwargs) -> "ContextLogger":
        """创建新的 ContextLogger，合并上下文"""
        new_context = dict(self._context)
        new_context.update(kwargs)
        return ContextLogger(self._logger, **new_context)


# 全局日志实例
_logger: Optional[logging.Logger] = None


def init_logger(level: str = "INFO", output: str = "json") -> logging.Logger:
    """
    初始化全局日志

    Args:
        level: 日志级别 DEBUG/INFO/WARNING/ERROR
        output: 输出格式 json 或 text

    Returns:
        配置好的 logger 实例
    """
    global _logger

    if _logger is not None:
        return _logger

    logger = logging.getLogger("customer_service_agent")
    logger.setLevel(getattr(logging, level.upper(), logging.INFO))

    # 避免重复添加 handler
    if logger.handlers:
        logger.handlers.clear()

    handler = logging.StreamHandler(sys.stdout)

    if output == "json":
        formatter = JSONFormatter(datefmt="%Y-%m-%dT%H:%M:%S.%fZ")
    else:
        formatter = logging.Formatter(
            "%(asctime)s [%(levelname)s] %(message)s",
            datefmt="%Y-%m-%d %H:%M:%S",
        )

    handler.setFormatter(formatter)
    logger.addHandler(handler)
    logger.propagate = False

    _logger = logger
    return logger


def get_logger(**context) -> ContextLogger:
    """
    获取带上下文的日志实例

    Args:
        **context: 上下文字段，如 trace_id、tenant_id、user_id

    Returns:
        ContextLogger 实例
    """
    if _logger is None:
        init_logger()
    return ContextLogger(_logger, **context)


def get_logger_from_headers(headers: Dict[str, str]) -> ContextLogger:
    """
    从请求头中提取 trace_id 和 tenant_id，创建带上下文的日志实例

    Args:
        headers: 请求头字典

    Returns:
        ContextLogger 实例
    """
    context = {}
    trace_id = headers.get("X-Trace-Id") or headers.get("x-trace-id")
    tenant_id = headers.get("X-Tenant-Id") or headers.get("x-tenant-id")

    if trace_id:
        context["trace_id"] = trace_id
    if tenant_id:
        context["tenant_id"] = tenant_id

    return get_logger(**context)
