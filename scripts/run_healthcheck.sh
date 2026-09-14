#!/bin/bash
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
tr -d '\r' < deploy/production/healthcheck.sh > /tmp/hc.sh
chmod +x /tmp/hc.sh
bash /tmp/hc.sh http://localhost:8080
echo "exit=$?"
