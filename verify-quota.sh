#!/usr/bin/env bash
# FlowGate V4 —— 多实例共享额度验证（一条脚本跑到底，不用 Postman）
# 用法：在 F:\FlowGate 目录下 `bash verify-quota.sh`
# 前提：先把之前起的 go run / gateway 进程停掉（避免端口冲突）

ADMIN=http://localhost:8092
GW_A=http://localhost:8090
GW_B=http://localhost:8093
QPS=10      # key 的每秒限额
N=10        # 每个网关打的次数（总共 2N 个请求，期望 ~QPS 个放行）

# 从 JSON 里抠 id（没有 jq，用 grep）
jid() { grep -oE '"id":[0-9]+' | head -1 | grep -oE '[0-9]+'; }

echo "== 0. 依赖：起 postgres + redis =="
docker-compose -f deploy/docker-compose.yml up -d
sleep 1

echo "== 0.1 修正 .env 的 REDIS_ADDR（宿主机端口是 6380）=="
if ! grep -q '^REDIS_ADDR=localhost:6380' .env; then
  sed -i 's/^REDIS_ADDR=.*/REDIS_ADDR=localhost:6380/' .env
  echo "    .env 已改成 localhost:6380"
fi
docker exec flowgate-redis redis-cli PING || { echo "!! redis 容器没起来"; exit 1; }

echo "== 1. 编译 =="
mkdir -p bin
go build -o bin/fg-mock   ./cmd/mock
go build -o bin/fg-admin  ./cmd/admin
go build -o bin/fg-gw     ./cmd/gateway

echo "== 2. 起 4 个进程（后台，日志在 bin/*.log）=="
./bin/fg-mock             > bin/mock.log   2>&1 & PID_MOCK=$!
./bin/fg-admin            > bin/admin.log  2>&1 & PID_ADMIN=$!
./bin/fg-gw               > bin/gw-a.log   2>&1 & PID_GWA=$!
./bin/fg-gw -addr :8093   > bin/gw-b.log   2>&1 & PID_GWB=$!

cleanup() { kill $PID_MOCK $PID_ADMIN $PID_GWA $PID_GWB 2>/dev/null; }
trap cleanup EXIT

echo "== 3. 等进程起来 =="
sleep 3
if ! curl -s -o /dev/null -w "%{http_code}" $ADMIN/api/v1/health | grep -q 200; then
  echo "!! admin 没起来，看 bin/admin.log"; tail -n 20 bin/admin.log; exit 1
fi

echo "== 4. 造数据（每条都打印完整返回）=="

echo "  -- service --"
SVC_JSON=$(curl -s -X POST $ADMIN/api/v1/services -d '{"name":"mock","protocol":"http"}')
echo "$SVC_JSON"
SVC_ID=$(echo "$SVC_JSON" | jid)

echo "  -- node --"
curl -s -X POST $ADMIN/api/v1/services/$SVC_ID/nodes -d '{"address":"http://localhost:8091"}'

echo "  -- route --"
ROUTE_BODY=$(printf '{"service_id":%s,"name":"echo","path_pattern":"/echo","path_match_type":"prefix","methods":""}' "$SVC_ID")
curl -s -X POST $ADMIN/api/v1/routes -d "$ROUTE_BODY"

echo "  -- publish --"
curl -s -X POST $ADMIN/api/v1/publish

echo "  -- tenant --"
TEN_JSON=$(curl -s -X POST $ADMIN/api/v1/tenants -d '{"name":"t1","qps_limit":1000}')
echo "$TEN_JSON"
TEN_ID=$(echo "$TEN_JSON" | jid)

echo "  -- key（明文只在这一次返回里出现，注意保存）--"
KEY_JSON=$(curl -s -X POST $ADMIN/api/v1/tenants/$TEN_ID/keys -d "{\"name\":\"k1\",\"qps_limit\":$QPS}")
echo "$KEY_JSON"
KEY_ID=$(echo "$KEY_JSON" | jid)
KEY=$(echo "$KEY_JSON" | grep -oE 'sk_[a-f0-9]+')

echo "== 5. 等 6 秒让两个网关拉到发布的路由 =="
sleep 6

echo "== 6. 并发打：$N 个打 A + $N 个打 B（限额 $QPS），每个状态码逐个打印 =="
(
  for i in $(seq 1 $N); do curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: $KEY" $GW_A/echo/anything & done
  for i in $(seq 1 $N); do curl -s -o /dev/null -w "%{http_code}\n" -H "X-API-Key: $KEY" $GW_B/echo/anything & done
  wait
) | tee bin/codes.txt
echo "  -- 计数汇总 --"
sort bin/codes.txt | uniq -c

echo "== 7. 判读 =="
echo "    约 $QPS 个 200 + 约 $QPS 个 429 → 共享生效（Redis 在工作）"
echo "    约 $((2*N)) 个 200、没有 429   → 没共享（Redis 没生效，查 .env / 日志）"

echo "== 8. 看 Redis 里的共享滑窗 =="
docker exec flowgate-redis redis-cli ZRANGE rl:key:$KEY_ID 0 -1 WITHSCORES

echo "== 9. 网关 A 日志里的关键事件 =="
grep -E "rate_limited|auth_" bin/gw-a.log | tail -n 5 || echo "    (无)"

echo "== 完成。日志在 bin/mock.log admin.log gw-a.log gw-b.log =="
