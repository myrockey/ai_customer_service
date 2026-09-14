#!/bin/bash
# P0-1 统一测试入口：Go 单测 + Agent 单测 + 系统健康冒烟
# 用法: bash scripts/run_p0_tests.sh [--skip-smoke]
set -e
# 支持 CS_ROOT 环境变量覆盖（开发环境 CRLF 拷贝到 /tmp 执行时使用）
if [ -n "$CS_ROOT" ]; then
  ROOT="$CS_ROOT"
else
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
fi
FAIL=0

echo "==================== 1/3 Go service 单测 ===================="
if docker run --rm -v "$ROOT/go_service":/app -w /app -e GOPROXY=https://goproxy.cn,direct golang:1.25-alpine \
    sh -c 'go mod download && go test ./internal/pkg/...' > /tmp/go_test_out.txt 2>&1; then
  tail -3 /tmp/go_test_out.txt
  echo "[PASS] Go 单测"
else
  tail -8 /tmp/go_test_out.txt
  echo "[FAIL] Go 单测"; FAIL=1
fi

echo "==================== 2/3 Agent 单测 ===================="
docker cp python_agent/test_agent_unit.py cs_agent:/app/test_agent_unit.py >/dev/null 2>&1
if docker exec cs_agent sh -c 'cd /app && python3 test_agent_unit.py' >/dev/null 2>&1; then
  echo "[PASS] Agent 单测"
else
  echo "[FAIL] Agent 单测"; FAIL=1
fi

echo "==================== 3/3 系统健康冒烟 ===================="
if [ "$1" = "--skip-smoke" ]; then
  echo "[SKIP] 冒烟跳过"
else
  if bash scripts/run_healthcheck.sh >/dev/null 2>&1; then
    echo "[PASS] 健康冒烟 14 项断言"
  else
    echo "[FAIL] 健康冒烟"; FAIL=1
  fi
fi

echo "==================== 结果: $([ $FAIL -eq 0 ] && echo ALL_PASS || echo HAS_FAIL) ===================="
exit $FAIL
