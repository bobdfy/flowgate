#!/usr/bin/env bash
# ============================================================================
# up.sh —— 一键拉起本地全栈（postgres + redis + gateway + admin）
#
# 跑法：
#   bash scripts/up.sh              # 构建镜像 → 启动 → 等到 /readyz 通过 → 打印入口
#   bash scripts/up.sh --no-build   # 不重建镜像（只重启时用；改了 Go 代码必须重建）
#   bash scripts/up.sh --down       # 停栈，保留数据卷
#   bash scripts/up.sh --clean      # 停栈并删除数据卷（租户 / 路由 / 版本历史全清）
#
# 这是 README「一键运行 / 方式一」的可执行版本：README 只留结论，步骤和判读在这里。
#
# 为什么要等 /readyz 而不是等容器 Up：
#   Up 只代表进程起来了。gateway 还要连 PG、加载路由表，这一段没完成前它
#   会拒绝服务（/readyz 返回 503）—— 直接发请求会看到一片 503 然后误判成部署失败。
# ============================================================================

set -euo pipefail

# 仓库根目录由脚本自身位置推出来，从哪个目录调用都行
ROOT="${FLOWGATE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
COMPOSE_FILE="$ROOT/deploy/docker-compose.yml"
GW="${GATEWAY_URL:-http://127.0.0.1:8090}"
ADMIN="${ADMIN_URL:-http://127.0.0.1:8092}"
READY_TIMEOUT="${READY_TIMEOUT:-90}"

BUILD=1
MODE=up

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-build) BUILD=0; shift ;;
    --down)     MODE=down; shift ;;
    --clean)    MODE=clean; shift ;;
    -h|--help)  sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "未知参数：$1（-h 看用法）" >&2; exit 2 ;;
  esac
done

step() { printf '\n\033[1;36m▸ %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m  注意：%s\033[0m\n' "$*"; }
die()  { printf '\033[1;31m错误：%s\033[0m\n' "$*" >&2; exit 1; }

# ---- 前置检查 -------------------------------------------------------------

command -v docker >/dev/null 2>&1 || die "找不到 docker，请先安装 Docker Desktop 并启动"

# compose v2 是 docker 的子命令；老版本是独立的 docker-compose。两种都认。
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  die "找不到 docker compose（v2 或独立的 docker-compose 都可以）"
fi

compose() { "${COMPOSE[@]}" -f "$COMPOSE_FILE" "$@"; }

# compose 里 gateway/admin 都写了 env_file: ../.env。文件不存在时 compose
# 报的是「env file not found」，和真正的故障原因（没配上游 key）差得很远。
[[ -f "$ROOT/.env" ]] || die "缺少 $ROOT/.env（至少需要 REDIS_PASSWORD=flowgate 和上游 key）"

if [[ ! -f "$ROOT/web/dist/index.html" ]]; then
  warn "web/dist 不存在，admin 会跳过控制台静态托管（需要时：cd web && pnpm install && pnpm build）"
fi

# ---- 停栈模式 -------------------------------------------------------------

case "$MODE" in
  down)
    step "停止 FlowGate（保留数据卷）"
    compose down
    echo "已停止。下次 bash scripts/up.sh --no-build 即可回来。"
    exit 0
    ;;
  clean)
    step "停止 FlowGate 并删除数据卷"
    compose down -v
    echo "已清空：数据库、Redis 数据、以及本项目的镜像层之外的一切运行时状态。"
    exit 0
    ;;
esac

# ---- 启动 -----------------------------------------------------------------

if [[ $BUILD -eq 1 ]]; then
  step "构建镜像并启动（首次约 1~2 分钟）"
  compose up -d --build
else
  step "启动（不重建镜像）"
  compose up -d
fi

# ---- 等待真正可服务 -------------------------------------------------------

wait_for() {
  # $1=名字  $2=URL；最多等 READY_TIMEOUT 秒
  local name="$1" url="$2" deadline=$((SECONDS + READY_TIMEOUT))
  printf '  等待 %-8s %s ' "$name" "$url"
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      printf 'OK（%ss）\n' "$((READY_TIMEOUT - (deadline - SECONDS)))"
      return 0
    fi
    printf '.'
    sleep 2
  done
  printf ' 超时\n'
  return 1
}

step "等待服务就绪（最多 ${READY_TIMEOUT}s）"
if ! wait_for gateway "$GW/readyz"; then
  compose ps
  echo
  echo "gateway 没在 ${READY_TIMEOUT}s 内进入 ready。看日志定位："
  echo "  ${COMPOSE[*]} -f deploy/docker-compose.yml logs --tail=50 gateway"
  exit 1
fi

# admin 只在控制台场景下是必须的，起得慢一点不算部署失败 —— 但还是要告诉使用者。
if ! wait_for admin "$ADMIN/api/v1/health"; then
  warn "admin 未就绪：数据面不受影响，控制台暂时打不开"
fi

# ---- 打印入口 -------------------------------------------------------------

cat <<EOF

$(printf '\033[1;32mFlowGate 已启动\033[0m')

  控制台      http://127.0.0.1:8092/        （需要 pnpm build 过 web/）
  网关        http://127.0.0.1:8090/        （请求需带 X-API-Key）
  指标        http://127.0.0.1:9090/metrics
  pprof       http://127.0.0.1:6060/debug/pprof/  （仅本机回环；top 用 go tool pprof）
  PostgreSQL  127.0.0.1:5433                （flowgate / flowgate / flowgate）
  Redis       127.0.0.1:6380                （密码 flowgate）

下一步：
  1. 打开控制台建租户 + API Key（明文只显示一次，别贴到聊天里）
  2. 冒烟：FLOWGATE_KEY=<key> bash scripts/smoke.sh
  3. 跟进日志：${COMPOSE[*]} -f deploy/docker-compose.yml logs -f gateway

假上游 mock 不在 compose 里，需要时另开一个终端：go run ./cmd/mock -addr :8091
（网关在容器里，节点地址要填 host.docker.internal:8091）
EOF
