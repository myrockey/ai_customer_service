#!/bin/bash
# 宿主机本地构建管理后台前端（需 WSL + Windows Node，产物输出到 web/admin/dist）
# 更推荐：容器内自动构建 docker compose up -d --build nginx（nginx.Dockerfile 多阶段构建，无需宿主机 Node）
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
set -e
cd "$ROOT/admin-app"
NODE="/mnt/c/Program Files/nodejs/node.exe"
NPMCLI="$(wslpath -w '/mnt/c/Program Files/nodejs/node_modules/npm/bin/npm-cli.js')"
echo "NPMCLI=$NPMCLI"
"$NODE" "$NPMCLI" install --registry=https://registry.npmmirror.com --no-audit --no-fund 2>&1 | tail -6
echo "=== install done ==="
"$NODE" "$NPMCLI" run build 2>&1 | tail -25
echo "=== build done ==="
ls -la "$ROOT/web/admin/dist" 2>&1 | head -20
