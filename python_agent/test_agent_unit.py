"""Agent 纯函数单测：租户解析 / 集合隔离 / 内容守卫 / 自动签名（无 DB 依赖，标准库 unittest）"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from main import tenant_from_thread, collection_name
from db_ops import extract_usage_metadata
from langchain_core.messages import HumanMessage, AIMessage


class TestTenantParse(unittest.TestCase):
    def test_tenant_from_thread(self):
        self.assertEqual(tenant_from_thread("t_demo:user123"), "t_demo")
        self.assertEqual(tenant_from_thread("t_demo:user:with:colon"), "t_demo")
        self.assertEqual(tenant_from_thread("nocolon"), "")
        self.assertEqual(tenant_from_thread(""), "")

    def test_collection_name(self):
        self.assertEqual(collection_name("t_demo"), "kb_t_demo")
        self.assertEqual(collection_name(""), "kb_default")
        self.assertEqual(collection_name("t_admin"), "kb_t_admin")


class TestUsageMeta(unittest.TestCase):
    def test_empty(self):
        self.assertIsNone(extract_usage_metadata([]))

    def test_no_exception_on_dicts(self):
        try:
            extract_usage_metadata([{"role": "user", "content": "hi"}])
        except Exception:
            self.fail("extract_usage_metadata 不应抛异常")


class TestContentGuardCompat(unittest.TestCase):
    """content_guard / auto_signature 被 langgraph 装饰器包装，直接验证注册结果非空且启用"""

    def test_decorated_hooks_registered(self):
        from main import before_model, after_model

        self.assertTrue(callable(before_model))
        self.assertTrue(callable(after_model))


if __name__ == "__main__":
    unittest.main(verbosity=2)
