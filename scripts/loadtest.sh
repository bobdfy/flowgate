#!/usr/bin/env bash
# ============================================================================
# loadtest.sh —— 网关整体压测：吞吐 / 延迟分位 / 限流 / 过载 / 日志缓冲
#
# 跑法：
#   bash scripts/loadtest.sh                    # 完整流程，约 4 分钟
#   bash scripts/loadtest.sh --duration 10s     # 缩短每轮时长，快速冒烟
#   bash scripts/loadtest.sh --skip-overload    # 跳过会触发熔断的第 6 步
#   bash scripts/loadtest.sh --keep             # 保留临时租户与看板，便于事后排查
#
# 前置：docker 栈已起（deploy/docker-compose.yml）、mock 在 :8091、bombardier 已安装。
#   mock：go run ./cmd/mock
#   bombardier：go install github.com/codesenberg/bombardier@v1.2.6
#               装完在 $(go env GOPATH)/bin 下，可用 BOMBARDIER=/path/to/bombardier 覆盖
#
# 安全约定：中间产生的 API Key 明文只写进 mktemp 文件，不打印、不进仓库；
#           脚本退出时（含 Ctrl+C）会删掉临时文件、临时租户和压测灌进看板的条目。
# ============================================================================

# 刻意不开 -e：本脚本有好几处「探测可能为空」的场景（grep 无匹配、sed 抽不到字段），
# -e 会在这些地方直接退出。失败一律用 die() 显式处理，反而更好读。
set -uo pipefail

# 仓库根目录由脚本自身位置推出来，从哪个目录调用都行
ROOT="${FLOWGATE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
# go env GOPATH 在 Windows 上给的是 D:\Go\gopath 这种反斜杠形式，转成 / 再拼路径
BOMB="${BOMBARDIER:-$(go env GOPATH 2>/dev/null | tr '\\\\' '/')/bin/bombardier}"
GW="${GATEWAY_URL:-http://127.0.0.1:8090}"
ADMIN="${ADMIN_URL:-http://127.0.0.1:8092}"
MOCK="${MOCK_URL:-http://127.0.0.1:8091}"
METRICS="${METRICS_URL:-http://127.0.0.1:9090/metrics}"

DURATION=30s
SKIP_OVERLOAD=0
KEEP=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration) DURATION="$2"; shift 2 ;;
    --skip-overload) SKIP_OVERLOAD=1; shift ;;
    --keep) KEEP=1; shift ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "未知参数：$1（-h 看用法）"; exit 2 ;;
  esac
done

KEYFILE="$(mktemp)"
RLFILE="$(mktemp)"
TID=""
ADMIN_KEY=""
K=""
REDIS_PASS=""

die() { echo "✗ $*" >&2; exit 1; }
step() { echo; echo "==== $* ===="; }

cleanup() {
  local code=$?
  if [[ -n "$TID" && "$KEEP" -eq 0 ]]; then
    local c
    c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
        -X DELETE -H "X-Admin-Key: $ADMIN_KEY" "$ADMIN/api/v1/tenants/$TID")
    echo "清理租户 $TID：HTTP $c"
  fi
  rm -f "$KEYFILE" "$RLFILE"
  if [[ "$KEEP" -eq 0 ]]; then
    # 错误看板是「签名 → 次数 / 最近一次」三件套，要一起删干净，
    # 否则残留的 count/meta 会让下次看板显示压测期间的累计次数。
    docker exec flowgate-redis redis-cli -a "${REDIS_PASS:-flowgate}" --no-auth-warning \
      DEL flowgate:errors flowgate:errors:count flowgate:errors:meta >/dev/null 2>&1
    echo "已清空错误看板（flowgate:errors{,:count,:meta}）"
  fi
  exit "$code"
}
trap cleanup EXIT INT TERM

code_of() {
  curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$1"
}

step "0/7 前置检查"
[[ -d "$ROOT" ]] || die "找不到仓库目录 $ROOT（可用 FLOWGATE_ROOT=... 覆盖）"
[[ -f "$BOMB" ]] || die "找不到 bombardier：$BOMB（可用 BOMBARDIER=... 覆盖）"
docker ps --format '{{.Names}}' | grep -qx flowgate-gateway || die "gateway 容器没在跑"
ADMIN_KEY=$(grep '^ADMIN_KEY=' "$ROOT/.env" | cut -d= -f2- | tr -d '\r')
[[ -n "$ADMIN_KEY" ]] || die "从 .env 读不到 ADMIN_KEY"
REDIS_PASS=$(grep '^REDIS_PASSWORD=' "$ROOT/.env" | cut -d= -f2- | tr -d '\r')

gw_code=$(code_of "$GW/readyz")
mock_code=$(code_of "$MOCK/hello")
echo "gateway readyz: $gw_code    mock: $mock_code"
[[ "$gw_code" == "200" ]] || die "gateway 未就绪（$GW/readyz -> $gw_code）"
[[ "$mock_code" == "200" ]] || die "mock 未启动（$MOCK/hello -> $mock_code），先跑 go run ./cmd/mock"

step "1/7 创建临时租户与 key"
# qps_limit 必须给大值：默认 100 会把压测卡在 100 QPS，测出来的就不是网关吞吐了。
# （顺带解释：限流窗口是 1s，qps_limit 就是字面意义的每秒请求数。）
TID=$(curl -s --max-time 5 -X POST -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"name":"loadtest-tmp","status":"active","qps_limit":1000000,"tpm_limit":1000000000}' \
  "$ADMIN/api/v1/tenants" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
[[ "$TID" =~ ^[0-9]+$ ]] || die "创建租户失败"

BODY=$(curl -s --max-time 5 -X POST -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' -d '{"name":"loadtest-key","qps_limit":1000000}' \
  "$ADMIN/api/v1/tenants/$TID/keys")
printf '%s' "$BODY" | sed -n 's/.*"key":"\([^"]*\)".*/\1/p' > "$KEYFILE"
K=$(tr -d '\r\n' < "$KEYFILE")
[[ -n "$K" ]] || die "创建 API Key 失败"
echo "tenant=$TID"

# 上一次压测若刚触发过服务级熔断，breaker 会停在 open，要等第一条请求才惰性
# 转入半开：热身瞬间会有一批 503（半开期只放一条探测）。先探测一次，避免污染热身数据。
cs=$(curl -s --max-time 3 "$METRICS" \
     | sed -n 's/^flowgate_circuit_state{[^}]*} \([0-9]*\)$/\1/p' | sort -u | tr '\n' ' ')
if [[ "$cs" == *1* || "$cs" == *2* ]]; then
  echo "⚠ 有上游熔断尚未恢复（state=$cs），先发一条探测请求让它走完半开"
  curl -s -o /dev/null -H "X-API-Key: $K" "$GW/hello"
  sleep 2
fi

step "2/7 预热 5s（c=100）"
"$BOMB" -c 100 -d 5s -H "X-API-Key: $K" "$GW/hello" 2>&1 \
  | sed 's/\r/\n/g' | sed -n '/Statistics/,$p'

run() {
  echo
  echo "--- $1 ---"
  "$BOMB" -c "$2" -d "$DURATION" -l -H "X-API-Key: $K" "$GW/hello" 2>&1 \
    | sed 's/\r/\n/g' | sed -n '/Statistics/,$p'
}

step "3/7 主测四轮（经网关，每轮 $DURATION）"
run "c=100 第 1 轮" 100
run "c=100 第 2 轮" 100
run "c=500 第 1 轮" 500
run "c=500 第 2 轮" 500

if [[ "$SKIP_OVERLOAD" -eq 0 ]]; then
  # 直连对照组只跑 c=100：c=500 会打满 mock 的 accept 队列，
  # 测出来的是 mock 的上限而不是网关开销。
  step "4/7 对照组：直连 mock（c=100）"
  "$BOMB" -c 100 -d "$DURATION" -l "$MOCK/hello" 2>&1 \
    | sed 's/\r/\n/g' | sed -n '/Statistics/,$p'
else
  step "4/7 已跳过直连对照"
fi

step "5/7 限流验证（qps_limit=5，10s，期望 2xx≈50）"
RBODY=$(curl -s --max-time 5 -X POST -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' -d '{"name":"rl-5","qps_limit":5}' \
  "$ADMIN/api/v1/tenants/$TID/keys")
printf '%s' "$RBODY" | sed -n 's/.*"key":"\([^"]*\)".*/\1/p' > "$RLFILE"
RK=$(tr -d '\r\n' < "$RLFILE")
if [[ -z "$RK" ]]; then
  echo "⚠ 创建低配额 key 失败，跳过"
else
  "$BOMB" -c 20 -d 10s -H "X-API-Key: $RK" "$GW/hello" 2>&1 \
    | sed 's/\r/\n/g' | grep -A1 'HTTP codes' | tail -1
fi

if [[ "$SKIP_OVERLOAD" -eq 0 ]]; then
  step "6/7 过载标签验证（c=1200 打 /slow 5s，会短暂触发服务级熔断，10s 后自愈）"
  "$BOMB" -c 1200 -d 5s -H "X-API-Key: $K" "$GW/slow" 2>&1 \
    | sed 's/\r/\n/g' | grep -A1 'HTTP codes' | tail -1
  echo "期望三类分开："
  echo '  route="overload" 503 = 实例在途上限（1000）拒绝'
  echo '  route="/slow"    503 = 已匹配路由，上游/熔断拒绝'
  echo '  route="/slow"    504 = 请求超时'
  curl -s --max-time 3 "$METRICS" \
    | grep '^flowgate_request_duration_seconds_count' | grep -v '^#'
else
  step "6/7 已跳过过载验证"
fi

step "7/7 采集指标"
curl -s --max-time 3 "$METRICS" \
  | grep -E '^flowgate_(log_dropped_total|log_queue_depth|errorlog_dropped_total|rate_limited_total|overload_rejected_total)' \
  | grep -v '^#'
echo
echo "错误看板 kind 分布（最近 500 种错误，按种类计数）："
curl -s --max-time 5 -H "X-Admin-Key: $ADMIN_KEY" "$ADMIN/api/v1/logs/errors?limit=500" \
  | sed 's/"ts"/\n"ts"/g' | sed -n 's/.*"kind":"\([a-z_]*\)".*/\1/p' | sort | uniq -c
echo
echo "各行实际次数（验证聚合：429 风暴应聚成 1 行而不是占满窗口）："
curl -s --max-time 5 -H "X-Admin-Key: $ADMIN_KEY" "$ADMIN/api/v1/logs/errors?limit=500" \
  | sed 's/{"ts"/\n{"ts"/g' \
  | sed -n 's/.*"method":"\([A-Z]*\)".*"route":"\([^"]*\)".*"status":\([0-9]*\).*"kind":"\([a-z_]*\)".*"count":\([0-9]*\).*/\4 \1 \2 \3 x\5/p' \
  | sort | head -20

cat <<'REF'

==== 参考值（同一台机器，2026-10-28） ====
  经网关 c=100 : 3,774 / 3,931 QPS，P50 17.9-19.1ms，P95 61-68ms，P99 115-133ms
  经网关 c=500 : 4,473 / 4,407 QPS，P50 93-103ms，P95 238ms，P99 361-418ms
  直连 mock    : 13,348 QPS，P50 4.17ms
  限流 qps=5   : 10s 放行 50 个（1s 窗口），其余 429
  过载         : route="overload" 503 ≈ 4 万；route="/slow" 503 ≈ 3 万；504 ≈ 1 千
  日志缓冲     : flowgate_log_dropped_total = 0
  错误看板     : 同一种错误聚合成一行（count 为累计次数），429 风暴不再占满窗口
  已知问题     : 写入侧仍是每请求一次 pipeline，洪水期
                 flowgate_errorlog_dropped_total 依然会很大（属预期，聚合只解决
                 「窗口被同一种错误占满」，不解决写放大）
REF
