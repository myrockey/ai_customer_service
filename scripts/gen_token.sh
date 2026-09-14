#!/bin/bash
# 生成有效token并测试流式输出
echo "=== 生成Token ==="
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"app_key":"demo_key","app_secret":"demo_secret"}' | python3 -c "import sys,json; print(json.load(sys.stdin).get('token',''))")

echo "Token: ${TOKEN:0:30}..."
echo ""
echo "=== 用有效token打开pc.html ==="
echo "http://localhost:8080/chat/pc.html?token=${TOKEN}&user_id=t_demo:stream_ui_test2"
