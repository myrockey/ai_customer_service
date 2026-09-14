#!/bin/bash
PROJ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# 重启 Python Agent（tmux 分离会话，保证后台常驻）
# 用法：wsl -e bash scripts/start_agent.sh
PROJ="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJ/python_agent" || exit 1

# 停掉旧的 tmux 会话与残留进程
tmux kill-session -t cs_agent 2>/dev/null
for pid in $(pgrep -f 'venv/bin/python .*main\.py'); do
  kill -9 "$pid" 2>/dev/null
done
sleep 2

# 在 tmux 中启动（tmux server 常驻，随 WSL 发行版存活）
tmux new-session -d -s cs_agent "cd $PROJ/python_agent && exec ./venv/bin/python -u main.py"
sleep 1
echo "agent started in tmux session: cs_agent"
echo "查看日志：wsl -e bash -lc \"tmux capture-pane -t cs_agent -p -S -50\""
