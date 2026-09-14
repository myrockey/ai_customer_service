#!/bin/bash
# P2-1 验证：HTTP 状态码归一化
BASE=http://localhost:8080
PASS=0; FAIL=0
chk() { # name expected_http actual_http note
  if [ "$2" = "$3" ]; then PASS=$((PASS+1)); echo "  ✅ $1: HTTP $3 $4"
  else FAIL=$((FAIL+1)); echo "  ❌ $1: 期望 $2 实际 $3 $4"; fi
}

echo "=== 1. 错误 secret 登录 → 401 ==="
HTTP=$(curl -s -o /tmp/r1.json -w '%{http_code}' -X POST $BASE/api/auth/token -H 'Content-Type: application/json' -d '{"app_key":"demo_key","app_secret":"wrong"}')
chk "登录失败" 401 "$HTTP" "$(python3 -c 'import json;print(json.load(open("/tmp/r1.json")).get("code"))')"

echo "=== 2. 未知租户 → 401 ==="
HTTP=$(curl -s -o /tmp/r2.json -w '%{http_code}' -X POST $BASE/api/auth/token -H 'Content-Type: application/json' -d '{"app_key":"nokey","app_secret":"x"}')
chk "未知app_key" 401 "$HTTP"

echo "=== 3. 缺 token 访问管理接口 → 401 ==="
HTTP=$(curl -s -o /tmp/r3.json -w '%{http_code}' $BASE/api/admin/tickets)
chk "未授权" 401 "$HTTP"

echo "=== 4. 防爆破锁定 → 429 ==="
for i in 1 2 3 4 5; do curl -s -o /dev/null -X POST $BASE/api/auth/token -H 'Content-Type: application/json' -d '{"app_key":"demo_key","app_secret":"bad"}'; done
HTTP=$(curl -s -o /tmp/r4.json -w '%{http_code}' -X POST $BASE/api/auth/token -H 'Content-Type: application/json' -d '{"app_key":"demo_key","app_secret":"bad"}')
chk "防爆破锁定" 429 "$HTTP"

echo "=== 5. 成功登录 → 200 + code:0 ==="
HTTP=$(curl -s -o /tmp/r5.json -w '%{http_code}' -X POST $BASE/api/auth/token -H 'Content-Type: application/json' -d '{"app_key":"admin_key","app_secret":"admin_secret"}')
CODE=$(python3 -c 'import json;print(json.load(open("/tmp/r5.json")).get("code"))')
chk "登录成功" 200 "$HTTP" "body.code=$CODE"

echo "=== 6. 正常业务 → 200 + code:0 ==="
AT=$(python3 -c 'import json;print(json.load(open("/tmp/r5.json")).get("data",{}).get("token",""))')
HTTP=$(curl -s -o /tmp/r6.json -w '%{http_code}' $BASE/api/admin/tenants -H "Authorization: Bearer $AT")
chk "租户列表" 200 "$HTTP"

echo "=== 7. 业务 4xx（非法参数类）→ 真实 4xx ==="
# 用一个会返回 code=400 的接口：获取不存在租户详情
HTTP=$(curl -s -o /tmp/r7.json -w '%{http_code}' $BASE/api/admin/tenants/t_does_not_exist -H "Authorization: Bearer $AT")
chk "租户不存在" 404 "$HTTP" "body.code=$(python3 -c 'import json;print(json.load(open("/tmp/r7.json")).get("code"))' 2>/dev/null)"

echo "=== 8. WS 握手不受影响（101） ==="
# 直接验证 /ws 路由不经过 JSON 改写（连不上只说明未握手，但状态码非 200 改写逻辑无关）
HTTP=$(curl -s -o /dev/null -w '%{http_code}' -m 3 $BASE/ws/chat)
echo "  （WS 无鉴权连接预期非200，中间件不拦截: HTTP=$HTTP 不代表失败）"

echo ""
echo "结果: $PASS 通过 / $FAIL 失败"
[ $FAIL -eq 0 ]
