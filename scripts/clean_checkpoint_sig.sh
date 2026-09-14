#!/bin/bash
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# 备份 checkpoint 表 + 执行 Python 清洗脚本
cd "$ROOT"
mkdir -p backups
echo "=== 备份 checkpoints 表 ==="
docker exec cs_postgres pg_dump -U cs_user -d cs_agent_db -t checkpoints -t checkpoint_writes -t checkpoint_blobs > backups/checkpoints_backup_$(date +%Y%m%d_%H%M%S).sql 2>/dev/null
ls -la backups/ | tail -2

echo ""
echo "=== 执行 checkpoint 签名清洗 ==="
docker exec -i cs_agent python - < scripts/clean_dup_signature_cp.py 2>&1 | tail -10
