# FlowGate

> 一个基于 Go 的高性能 API 网关与流量治理系统。
> 面向内部微服务与 AI 模型服务，围绕请求转发的正确性、服务治理、故障隔离与性能可验证性构建。

---

## 目录

- [现实问题](#现实问题)
- [FlowGate 的解决方式](#flowgate-的解决方式)
- [技术栈](#技术栈)
- [系统架构图](#系统架构图)
- [目录结构](#目录结构)
- [一次请求完整链路](#一次请求完整链路)
- [配置发布流程](#配置发布流程)
- [一键运行](#一键运行)
- [故障实验](#故障实验)
- [性能测试结果](#性能测试结果)
- [已知限制](#已知限制)
- [后续计划](#后续计划)
- [参考项目与设计来源](#参考项目与设计来源)

---

## 现实问题

1. **上游偶发抖动，调用方各自处理重试**
   上游每月总有几次抖动。每个调用方都得自己决定要不要重试、重试几次、等多久。
   大多数人不会做退避 —— 上游刚恢复，一堆客户端同时重试又把它打挂。

2. **第一次请求成功、下一次失败**
   客户按文档调接口，有时成功有时失败，但错误码看不出差别。
   根因是网关把一个已经出问题的实例还留在可用列表里 —— 失败是**随机的**，
   调用方无法复现，会误判成"自己的代码有问题"。

3. **一条慢接口把整个服务拖死**
   一个数据服务上有两条接口：查用户 50ms、全表导出 10 秒。
   运营点了一次导出，30 个并发把上游的并发槽位占满，**正常的用户查询全部超时**。
   排查半小时才发现是导出打满了上游 —— 导出本身没坏，但它没有隔离。

4. **厂商密钥散落在各业务系统**
   20 多个服务都要调大模型，每家厂商的 key 各自配置。
   换一次 key 要发 20 次上线，漏一个那个服务就静默失败；
   而且无法回答"现在有几个服务在用哪个 key"。

---

## FlowGate 的解决方式

> 下表按上面的问题顺序排列 —— 「对应问题」那一列说明这个能力是为谁准备的。

| # | 能力 | 对应问题 | 说明 |
|---|---|---|---|
| 1 | **配置热更新** | — | 不可变配置快照；每 5 秒轮询版本号，变化则用 `atomic.Value` **原子替换**整张路由表，在途请求不受影响 |
| 2 | **分层超时** | ① | 建连 / 整体 / 响应头三层分开 —— 使"TCP 连不上"和"上游处理慢"不再混成同一个超时错误 |
| 3 | **幂等重试 + 退避** | ① | 只对幂等方法重试（POST 不重试，防重复扣款）；指数退避 + **保底 25% 抖动**，避免多个客户端同时重试把刚恢复的上游再次打挂 |
| 4 | **三态熔断器** | ② | 节点级 Closed / Open / HalfOpen；冷却期**惰性**转半开（省掉每节点一个定时器） |
| 5 | **主动 + 被动健康检查** | ② | `/health` 定时探测 + 请求结果反馈，把故障节点摘出可用列表 |
| 6 | **两级并发护栏** | ③ | 服务级保护上游、路由级实现隔离（一条慢接口占不满整个服务的并发）；三档策略 fail-fast / wait / drop |
| 7 | **分布式限流** | — | Redis + Lua 滑动窗口保证多实例配额一致；租户 + Key 双层配额**合并为一次 Lua 原子判定**；Redis 故障时降级到本地令牌桶（三档策略） |
| 8 | **Provider 适配层** | ④ | 厂商密钥只配在网关侧，业务系统不接触；以 OpenAI 兼容协议接入多家大模型，厂商差异由适配器抹平 |
| 9 | **SSE 流式分帧** | ④ | 网络分块不按事件边界切，需跨块重建事件边界；末尾遗漏事件要补发（否则 token 用量统计整批丢失） |
| 10 | **鉴权与配额** | ③④ | API Key 认证（SHA256 存储 + 进程内 TTL 缓存，含正/负缓存）+ 租户级 / Key 级双层 QPS，把配额落到具体客户 |

---

## 技术栈

| 分类 | 内容 |
|---|---|
| **语言** | Go 1.26；goroutine + channel + `sync/atomic` + `context` |
| **核心链路** | ★ **零 Web 框架** —— 转发、路由、负载均衡、熔断、重试、并发控制全部基于标准库：`net/http` + `Transport.RoundTrip`（**不用** `httputil.ReverseProxy`）、`atomic.Value` 原子替换路由表、`select` + channel 信号量、`context.WithTimeout`；SSE 分帧为自研字节级状态机 |
| **直接依赖** | 仅 5 个：`pgx/v5`（PostgreSQL）、`go-redis/v9`、`client_golang`（Prometheus）、`yaml/v2`（解析 `ai.yaml`）、`godotenv` |
| **基础设施** | PostgreSQL 16（:5433，配置 / 版本快照 / 租户 / API Key）、Redis（:6380，Lua 滑动窗口限流）、Docker Compose |
| **可观测性** | 11 个 Prometheus 指标 + `/metrics`（:9090）、`slog` JSON 日志带 `request_id`、pprof（:6060）、`/health` 白名单、SIGTERM 优雅退出 |

> ★ 依赖只有 5 个，是因为绝大部分能力没有引入第三方库 —— 这也是本项目的主要学习目标。

---

## 系统架构图

![FlowGate 系统架构图](architecture.png)

> 图片源文件在 `architecture.drawio`，可用 draw.io 打开修改后重新导出。

```
                         ┌───────────────────────────────────────────────┐
                         │                gateway  :8090                 │
   客户端 / 业务方        │               （数据面，无状态）                │
  (携带 X-API-Key)       │                                               │
        │                │   RequestID → Logging                         │
        │  ① 请求         │     → RequireAuth          ──► PG（TTL 缓存）   │
        └───────────────►│     → RateLimitPair  ──► Redis（一次 Lua）       │
                         │     → 分流                                    │
                         │        ├─► AI Handler ─────────► 大模型 API    │
                         │        └─► Router → 匹配路由（atomic.Value 快照）│
                         │              → 路由级护栏（Bulkhead）           │
                         │              → 选实例（加权轮询 + 健康过滤）      │
                         │              → 服务级护栏（并发保护）            │
                         │              → 分层超时 + 幂等重试              │
                         │              → 转发 ────────────► 上游服务      │
                         └───────────────────────────────────────────────┘
                                                  │
                ② 每请求查 Redis（PG 走缓存）        │              ③ 转发
              ┌───────────────────┬───────────────┴──────────────┐
              ▼                   ▼                              ▼
      ┌───────────────┐   ┌───────────────┐            ┌──────────────────┐
      │  PostgreSQL   │   │     Redis     │            │    上游服务        │
      │  服务 / 路由   │   │  Lua 滑动窗口  │            │  普通 HTTP 后端    │
      │  版本快照      │   │  （多实例共享） │            │  或 大模型 API     │
      │  租户 / API Key│   └───────────────┘            └──────────────────┘
      └───────▲───────┘
              │
              │  ④ 发布配置（写入新版本快照）
              │     网关每 5 秒轮询版本号 → 变化则原子替换路由表
              │
      ┌───────┴───────┐
      │     admin     │
      │  控制面 :8092  │
      └───────────────┘


  ══════════════════════════════════════════════════════════════════════════
   观测（横切所有组件，没有反向依赖）
     Prometheus 指标 :9090   ｜   slog JSON 日志（带 request_id）  ｜   pprof :6060
  ══════════════════════════════════════════════════════════════════════════
```

**图例**

| 编号 | 流向 | 说明 |
|---|---|---|
| ① | 客户端 → 网关 | 请求入口，携带 `X-API-Key` |
| ② | 网关 → PG / Redis | Redis **每请求一次**：租户 + Key 双层配额合并在一个 Lua 里原子判定；PG 做鉴权但走进程内 TTL 缓存，**命中时不查库**（最终一致，TTL 可配） |
| ③ | 网关 → 上游 | 普通路径走 HTTP 后端；AI 路径走 AI Handler → 大模型 API |
| ④ | admin → PG | 配置发布写入新版本快照；网关轮询版本号决定是否刷新路由表 |

>  **两条链路在网关内部分叉，但共享同一套鉴权与限流** —— 这正是"统一网关"的含义：
>
> ```
> 普通请求：分流 → Router → 路由级护栏 → 服务级护栏 → 转发
> AI 请求：分流 → AI Handler → HTTP 转发（含 SSE 流式分帧）
> ```
>
> 两者都走完整的 `RequestID → Logging → RequireAuth（TTL 缓存）→ RateLimitPair（一次 Lua）`。
>
> 缓存与 Lua 都属于**最终一致**的取舍：鉴权缓存让"停用 key 最多 TTL 秒后才全量生效"，
> 与路由表"5 秒轮询版本号"的模型一致；换来的稳态收益见 [性能测试结果](#性能测试结果)。

---

## 目录结构

```
cmd/
  gateway/        数据面入口（装配 + 中间件链 + 路由刷新 + 优雅退出）
                  ai.go = AI 路由构建
  admin/          控制面入口（配置管理 API :8092）
  mock/           通用假上游（回显 + 故障注入：延迟 / 状态码 / 概率）
  mockai/         OpenAI 兼容假上游（SSE 流式 + 恶劣分片模式）
internal/
  gateway/        反向代理核心（路由匹配 + 转发编排 + 重试退避）
  loadbalance/    加权轮询 + 健康检查 + 三态熔断状态机
  middleware/     中间件链（RequestID / 日志 / 鉴权+TTL缓存 / 合并限流 / 错误响应分流）
  overload/       并发护栏（三档策略 + 热更新，零依赖）
  sse/            SSE 字节级分帧器（跨块拼接 / 悬挂 CR / RESYNC / 末尾补发）
  ai/             AI 网关（模型路由 + Provider 抽象 + 流式转发）
  provider/       厂商适配器（OpenAI 兼容实现）
  ratelimit/      Redis 滑动窗口 + 本地令牌桶 + 故障降级三档
  identity/       身份模型（叶子包，零依赖）
  model/          数据模型（Service / Route / Node / Tenant / APIKey / Version）
  store/postgres/ 数据访问层（连接池 + 各表 Store + 版本快照发布）
  observability/  Prometheus 指标（11 个）
  redisclient/    Redis 客户端封装
  api/            控制面 HTTP 处理器（服务 / 节点 / 路由 / 租户 / Key / 发布）
migrations/       SQL 建表脚本（001_init.sql 含完整表结构）
tools/
  loadgen/        并发压测器（精确控制并发数与每请求超时）
  disconnect-test/ 验证「客户端断开 → 网关停止转发 → 上游停止生成」
deploy/           docker-compose（postgres + redis + gateway + admin）
Dockerfile        多阶段构建（gateway/admin/mock/mockai 共用一个，ARG BIN 区分）
.dockerignore     构建上下文排除清单（密钥 / 缓存）
ai.yaml           首次启动的 AI 配置种子（之后以 DB 为准）
```

---

## 一次请求完整链路

1. RequestID  生成唯一 ID（写进响应头 + 日志）
2. Logging    开始计时（日志经 64KB 缓冲后台批量落盘）
3. RequireAuth  校验 X-API-Key → 注入身份到 context
               命中进程内 TTL 缓存时**不查库**；含负缓存，防无效 key 打穿 DB
4. RateLimitPair 租户 + Key 双层配额，**一次 Redis Lua 原子判定**（2 次往返 → 1 次）
               Redis 故障时降级到本地令牌桶
5. 分流       AI 路径 → AI Handler；其余 → Router
6. 匹配路由    从当前快照匹配 host + path + method
7. 路由级 Guard  并发护栏（入口），满了直接 503
8. 选实例      加权轮询 + 健康检查过滤 + 熔断状态过滤
9. 服务级 Guard  并发护栏（出口）
10. 分层超时 + 转发（含幂等重试 + 退避）
11. 错误分类处理  连接拒绝→502 / 超时→504 / 无健康节点→503

---

## 配置发布流程

```
① 改配置      通过 admin API 创建 / 修改服务、实例、路由
                  ↓
② 校验        发布时检查：服务是否有可用实例、路由匹配规则是否重复
                  ↓  校验失败则拒绝发布，不会污染线上配置
③ 生成快照    把当前所有配置打包成一个【不可变的版本】，
              写入 route_versions + route_version_items
                  ↓
④ 生效        网关每 5 秒轮询版本号，发现变化就用 atomic.Value
              整表原子替换路由表 —— 在途请求不受影响
                  ↓
⑤ 回滚        把历史版本重新发布为一个新版本（copy-forward），
              不删除历史记录
```

**为什么这么设计**：配置是"发一次、用很久"的东西，需要可追溯、可回滚。
用版本快照而不是"直接改表"，才能回答"线上跑的是哪一版"以及"出问题回到哪一版"。

---

## AI 模型管理（DB 配置 + 热更新）

AI 上游与模型不再只靠 `ai.yaml`，而是可以通过管理 API 在线管理：

- **API**：
  - `GET/POST /api/v1/ai/providers`、`PUT/DELETE /api/v1/ai/providers/{id}`
  - `GET/POST /api/v1/ai/models`、`PUT/DELETE /api/v1/ai/models/{name}`
- **密钥只存环境变量名**：`api_key_env=ZHIPU_API_KEY`，明文密钥不落库；
  gateway 启动时从环境变量读取。本地假上游要写死 key 时才会用到 `api_key` 列。
- **配置来源**：首次启动若 `ai_providers` 为空，自动把 `ai.yaml` 导入 DB 做种子；
  之后 **DB 是唯一事实来源**，`ai.yaml` 只在 DB 不可用时兜底。
- **生效方式**：任何写操作都会在同一个事务里把 `ai_config_version` +1；
  gateway 每 3 秒轮询该版本号，变化时重建 AI handler 并原子替换，
  在途请求不受影响。DB 故障时保留旧 handler（fail-static）。
- **部署**：新库会自动执行 `migrations/003_ai_config.sql`；已有库需手动执行一次
  （如 `docker exec -i flowgate-postgres psql -U flowgate -d flowgate < migrations/003_ai_config.sql`）。
  admin 建议用 `-admin-key` 启动，管理接口请求需带 `X-Admin-Key`。

### Key 级模型白名单

创建/更新 API Key 时可以带 `allowed_models`（模型别名的数组），把 Key 绑定到指定模型：

```json
{"name": "agent-a", "allowed_models": ["flowgate-deepseek"]}
```

- 空数组 / 不传 = 不限制（兼容旧 Key）；非空 = 只能用列出的模型。
- 建 Key 时会校验别名是否存在，不存在返回 400。
- 越权调用返回 `403 permission_error`；`GET /v1/models` 也只返回该 Key 可见的模型。
- `PUT /api/v1/keys/{id}` 不带 `allowed_models` 时保留原值，显式传 `[]` 才清空限制。
- 新库自动执行 `migrations/004_api_key_models.sql`；已有库需手动执行一次。

### TPM 用量查询与鉴权缓存

- `GET /api/v1/tenants/{id}/tpm-usage` 返回当前窗口的已用 token、限额、剩余量与重置时间。
  读的是 gateway 正在用的 Redis 固定窗口计数器；窗口长度由 gateway 写入 `tpm:window`，
  所以 admin 不必再配一遍。
- **TPM 窗口可视化配置**：控制台「访问控制 → 租户与 API Key」页顶部的「TPM 配额窗口」卡片，
  或 `PUT /api/v1/settings/tpm-window`（body `{"window_seconds": 300}`，范围 1–604800）。
  窗口保存在 `gateway_settings` 表，gateway 每 3 秒轮询并热切换；所有实例共享同一配置。
  `-tpm-window` 只在首次启动、数据库还没有配置时作为种子。
  改窗口不会迁移旧计数：旧窗口的 Redis key 按 TTL 自然回收，新请求立即按新窗口计数。
- **TPM 放行规则**：转发前按「请求体大小 / 4 + `max_tokens`（缺省 512、封顶 4096）」预扣估算 token，
  拿到真实 usage 后多退少补。预扣最多占满剩余额度，只有窗口已用满才返回 429，
  避免出现「还剩 9 万额度却报配额已用尽」；429 message 会带上已用 / 上限 / 重置秒数。
- admin 未设置 `REDIS_ADDR` 时该接口返回 503，其余管理接口不受影响；
  docker compose 已为 admin 注入 Redis 地址。
- 管理面写 API Key / 租户时，会在同一事务里递增 `auth_version`；gateway 每 3 秒轮询，
  发现变化后清空进程内鉴权缓存。改 `allowed_models`、限额、停用状态不再需要等缓存 TTL。
- 新库自动执行 `migrations/005_auth_version.sql`；已有库需手动执行一次
  （如 `docker exec -i flowgate-postgres psql -U flowgate -d flowgate < migrations/005_auth_version.sql`）。
- TPM 窗口配置表见 `migrations/007_gateway_settings.sql`；已有库同样需手动执行。

### Token 用量流水

- gateway 每完成一次 `/v1/chat/completions` 请求写一条 `ai_usage` 记录：
  租户 / Key、对外模型名、prompt / completion / total token、是否估算、
  状态码、是否流式、耗时、Request ID、时间。**只记量，不记费用。**
- 写入是异步的：请求路径只做一次非阻塞入队，后台每 1 秒或每 256 条
  批量 `CopyFrom`；队列满时丢记录并打日志，绝不阻塞 AI 响应。
- 上游没返回 usage 时，流式按事件字节估算，并标记 `estimated=true`。
- 控制台把每个 Key 最近 24 小时的 token / 请求数直接显示在「租户详情 → API Keys」表格里。
- 查询接口：
  - `GET /api/v1/usage?tenant_id=&key_id=&model=&from=&to=&limit=&offset=`
  - `GET /api/v1/usage/summary?group_by=model|key|tenant|day&...`
  - `from` / `to` 支持 RFC3339 或 `YYYY-MM-DD`，缺省为最近 24 小时。
- 新库自动执行 `migrations/006_ai_usage.sql`；已有库需手动执行一次
  （如 `docker exec -i flowgate-postgres psql -U flowgate -d flowgate < migrations/006_ai_usage.sql`）。

### gateway 运行状态（节点健康 / AI 热加载）

- 健康检查结果和 AI 配置加载结果只存在于 gateway 内存。gateway 每 5 秒把快照写入 Redis
  `flowgate:gateway:status`（TTL 15 秒），admin 只读转发，不自己探测。
- 接口：
  - `GET /api/v1/runtime/nodes`：实时节点状态（node_id、地址、closed/half_open/open、
    连续失败数、熔断到期时间）
  - `GET /api/v1/runtime/ai`：最近一次 AI 配置加载结果（版本、时间、提供商/模型数、最近错误）
  - 响应带 `instance`、`published_at`、`age_seconds`、`stale`；Redis 里没有快照时返回 503。
- 控制台：服务详情多一列「实时健康」；「系统 → 接入状态」新增 gateway 运行状态卡片。
- 多实例部署时以最后上报的实例为准（与 `tpm:window` 相同）。Redis 不可用或 admin 未配
  `REDIS_ADDR` 时这些接口返回 503，其余管理接口不受影响。

客户端接入（OpenAI SDK / Codex / LangChain 配置、限流与错误码、排障清单）
见 [docs/AI接入指南.md](docs/AI接入指南.md)。

---

## 一键运行

前置：Docker（方式二还需 Go 1.26+；方式三还需 `kubectl` 与本地 K8s 集群，如 Docker Desktop 自带 Kubernetes）。

### 方式一：Docker 全容器（推荐）

```powershell
cd F:\FlowGate\deploy
docker compose up -d --build
```

一条命令拉起 4 个服务：

| 服务 | 端口 | 说明 |
|---|---|---|
| postgres | 5433 | 首次启动自动执行 `migrations/*.sql`（含租户、路由、TPM、AI 配置表） |
| redis | 6380 | 密码 `flowgate` |
| gateway | 8090 / 9090 | 数据面 / metrics |
| admin | 8092 | 管理面 |

管理控制台由 admin 同端口托管：先在 `web/` 执行 `pnpm install && pnpm build`，
产物 `web/dist` 挂载进 admin，然后打开 <http://127.0.0.1:8092/>。
开发模式仍用 `cd web && pnpm dev`（5173，`/api` 代理到 8092，支持热更新）。

验证：`docker compose ps`（四个都 Up）；查表 `docker exec flowgate-postgres psql -U flowgate -d flowgate -c "\dt"`（7 张表）。

> 假上游 `mock` 是测试替身、不属于部署产物，不在 compose 里；联调/压测时本地另起 `go run ./cmd/mock -addr :8091`（AI 流式用 `go run ./cmd/mockai`）。

> **mock 节点地址怎么填（跟跑法有关）**
> - **方式一（Docker 全容器）**：网关在容器里，配置节点地址要填 `host.docker.internal:8091`
>   （`127.0.0.1` 是容器自己，够不到宿主机上的 mock）；admin 需开 `ALLOW_PRIVATE_UPSTREAM=1`（compose 里已配）。
> - **方式二（本地 `go run`）**：网关直接跑在宿主机，节点地址照常填 `127.0.0.1:8091`，不用改。

### 方式二：本地开发（依赖 Docker 化，程序用 go run）

```powershell
# 1. 只起依赖（表自动建）
cd F:\FlowGate\deploy
docker compose up -d postgres redis

# 2. 三个进程，admin 先于 gateway
# 终端 A：go run ./cmd/mock -addr :8091
# 终端 B：go run ./cmd/admin        # :8092
# 终端 C：go run ./cmd/gateway      # :8090
```

> 数据库 `flowgate/flowgate@localhost:5433/flowgate`；redis `localhost:6380`（密码 `flowgate`，本地 `.env` 需配 `REDIS_PASSWORD=flowgate`）。

### 方式三：Kubernetes

三种方式**三选一**，不要混用。

```bash
# 1. 构建镜像
docker build --build-arg BIN=gateway -t flowgate-gateway:latest .
docker build --build-arg BIN=admin   -t flowgate-admin:latest .

# 2. 导入集群
#    本地 kind 集群没有镜像仓库，必须手动导入，否则 Pod 会 ImagePullBackOff
#    Git Bash 下需加 MSYS_NO_PATHCONV=1，否则容器内路径会被改写成 Windows 路径
docker save flowgate-gateway:latest | MSYS_NO_PATHCONV=1 docker exec -i desktop-control-plane ctr -n k8s.io images import -
docker save flowgate-admin:latest   | MSYS_NO_PATHCONV=1 docker exec -i desktop-control-plane ctr -n k8s.io images import -

# 3. 建 Secret（密码不写进 YAML）
kubectl create secret generic flowgate-secret \
  --from-literal=REDIS_PASSWORD=flowgate \
  --from-literal=DATABASE_URL='postgres://flowgate:flowgate@postgres:5432/flowgate'

# 4. 部署
kubectl apply -f k8s/
kubectl get pods        # 期望 4 个 1/1 Running
```

| 文件 | 内容 |
|---|---|
| `k8s/10-postgres.yaml` | PVC（数据盘）+ Deployment + Service |
| `k8s/11-redis.yaml` | Deployment + Service |
| `k8s/20-gateway.yaml` | Deployment + Service（8090 / 9090） |
| `k8s/21-admin.yaml` | Deployment + Service（8092） |

验证：

```bash
kubectl port-forward svc/admin 18092:8092
curl http://127.0.0.1:18092/api/v1/health          # 200

kubectl port-forward svc/gateway 18090:8090
curl http://127.0.0.1:18090/                       # 401「缺少 X-API-Key」= 鉴权生效
```

> 集群里的节点地址填 Service 名，例如 `http://mock:8091`，不要填 `127.0.0.1`
>（那是 Pod 自己）；admin 已配 `ALLOW_PRIVATE_UPSTREAM=1` 放行集群内地址。

> 完整的功能验证步骤（25 项，可直接照做）见 [验证清单.md](<验证清单.md>)。

---

## 故障实验

> 以下全部是**实测结果**，不是设计预期。重点是"这条在证明什么"。

| 实验 | 条件 | 结果 | 在证明什么 |
|---|---|---|---|
| **fail-fast** | `mc=1` + 上游延迟 3s + 20 并发 | `200×1` + `503×7`，**总耗时 3.0s** | ★ 不是 8×3=24s → **立即拒绝**而非排队 |
| **wait** | 同上 + `queue_timeout=500ms` | `503 queue_timeout` + `Retry-After: 1` | 被拒前**真的等过**，并告知何时重试 |
| **drop** | 同上 + 等候区容量限制 | `queue_full` 与 `queue_timeout_drop` **都出现** | 两种失败场景可区分，能指导调参 |
| **护栏覆盖重试全程** | `mc=2` + `max_retries=3` + 上游全 500，6 并发 | **在途峰值 = 2** | 写错会是 **8**（上限被放大 `maxRetries+1` 倍） |
| **Bulkhead 隔离** | 同服务两条路由，慢的 `mc=1`、服务级不限 | slow `200×1+503×7`；**fast `200×5`、耗时 8ms** | ★ 隔离的**定义性验证**（若 fast 也被拒，就只是多了一层限流） |
| **两层同时生效** | 服务和路由级都配 | 30 秒交替压测 10 轮无 hang | 固定获取顺序排除 AB-BA 死锁 |
| **客户端断开不计过载** | 10 个主动断开 vs 10 个真排队超时 | 计数 **0 vs 9** | ★ 区分"我们扛不住"和"你自己走了"（对照组排除"指标没接上"） |
| **超时 → 504** | 上游延迟 10s，`request_timeout=2000ms` | `504`，耗时 **≈2000ms** | 网关主动放弃，不是等上游返回 |
| **幂等重试 + 退避** | 上游持续 500，`max_retries=3` | 日志 `backoff_ms` **递增**，无接近 0 的值 | 保底 25% 抖动生效，防重试风暴 |
| **连接被拒 → 502** | 节点先健康、再杀掉进程 | `502` + 日志含 `connection refused` | ★ 与 503 区分（必须先健康再杀，否则验到的是熔断不是连接拒绝） |
| **熔断三态** | `cb_failure_threshold=3`，上游持续 500 | `500×3` → `503`，`circuit_state=1` | 上游明确挂掉时不再白等 |
| **配置校验** | 建同名匹配规则的路由 | `409 路由冲突`（Update 也挡） | 把**静默失效**变成显式失败 |

> 详细的实验步骤、前置配置与判读方法见 [验证清单.md](<验证清单.md>)（25 项，均可照做）。

---

## 性能测试结果

> 2026-10-18 重测。工具、参数、机器信息全部记录，可复现。

### 测试环境（12 项参数）

| 项 | 值 |
|---|---|
| CPU | Intel i5-12500H（12 核 16 线程） |
| 内存 | 16 GB（压测端 / 网关 / 后端同机共享） |
| 网络 | 压测端→网关 loopback（Docker 端口映射）；网关→Redis/mock 走 Docker 桥接 |
| 压测工具 | bombardier v1.2.6（`go install`，复用连接，`-l` 输出百分位） |
| 请求大小 | GET 无 body；响应约 300 字节 JSON |
| TLS | 否（明文 HTTP） |
| 数据库 | 参与（鉴权，走 TTL 缓存，预热后命中不查库） |
| Redis | 参与（每请求一次 Lua 限流） |
| 单机/多实例 | 单机、单实例 |
| 并发设置 | bombardier `-c`（本测用 100 / 500） |
| P50/P99 统计 | bombardier `-l` 的 Latency Distribution 50% / 99% 分位 |
| 持续时长 | 每组固定 30 秒（`-d 30s`）× 2 轮，另加 5 秒预热 |

### 结果（每组 2 轮，30 秒/轮）

| 组 | 并发 | QPS | P50 | P99 | 总请求量（单轮 30s） |
|---|---|---|---|---|---|
| A 直连后端（绕过网关） | 100 | 59308 / 51324 | 1.12 / 1.03 ms | 9.43 / 10.79 ms | ≈177.9万 / ≈154.0万 |
| B 经网关 | 100 | 4679 / 4938 | 17.82 / 17.82 ms | 75.28 / 54.53 ms | ≈14.0万 / ≈14.8万 |
| C 经网关 | 500 | 6400 / 5825 | 70.52 / 73.82 ms | 215.99 / 269.79 ms | ≈19.2万 / ≈17.5万 |

（全部 2xx，零错误。）

### 结论

后端是零成本的假服务，直连约 5.5 万 QPS 经网关后掉到约 4.8 千（**约 11 倍开销**），
P50 从约 1ms 涨到约 18ms —— 这 17ms 几乎全是网关自身的治理开销（鉴权 + Redis 限流 + 转发），
不是后端慢。500 并发时 QPS 升到约 6 千、但 P50 涨到约 70ms：并发把吞吐顶上去，排队也把单请求延迟顶上去。

### 复现命令

```powershell
# 一次性装好工具
go install github.com/codesenberg/bombardier@latest

# A 直连后端
bombardier -c 100 -d 30s -l http://127.0.0.1:8091/

# B 经网关 @100 并发
bombardier -c 100 -d 30s -l http://127.0.0.1:8090/ -H "X-API-Key: <key>"

# C 经网关 @500 并发
bombardier -c 500 -d 30s -l http://127.0.0.1:8090/ -H "X-API-Key: <key>"
```

> 前置：网关跑在 Docker 里时，mock 的节点地址要指向 `host.docker.internal:8091`
>（`127.0.0.1` 是容器自己，够不到宿主机的 mock），admin 需开 `ALLOW_PRIVATE_UPSTREAM=1`。


---

## 已知限制

### 边界（这个项目**不适合**的用法）

| 限制 | 说明 |
|---|---|
| **定位是学习与验证，不是生产选型** | 生产环境请用 Nginx / Envoy / APISIX —— 它们更成熟、生态更完整 |
| **无管理界面** | 所有配置通过 admin API 操作，可用 Yaak / Postman |
| **未做控制面 / 数据面分离** | `admin` 和 `gateway` 是两个进程，但共用同一份代码仓库与配置读取逻辑 |
| **未做多租户计费** | 只有配额限制，没有用量计费后台 |
| **单实例验证** | 分布式限流支持多实例共享配额，但未做多实例集群压测 |


### K8s 部署的不足

> 属于**最小可用部署**，优先保证跑通，未做生产级加固。

| # | 不足 | 影响 | 怎么补 |
|---|---|---|---|
| 1 | 单副本，无 HPA | 一台挂了服务就断，流量涨了扛不住 | gateway 无状态，可设 `replicas: 3` + HPA；但需先验证分布式限流配额一致 |
| 2 | 无 Ingress | 外部访问不了，只能 `port-forward` | 加 Ingress + TLS 终止，或前置 NLB |
| 3 | 资源限额是估算值 | 高了浪费、低了 OOMKilled | 压测后按 P99 定 requests，limits 留 1.5 倍余量 |
| 4 | 无 PDB | 节点维护驱逐时可能全挂 | `PodDisruptionBudget: minAvailable: 1` |
| 5 | postgres 用 Deployment 而非 StatefulSet | 无稳定网络标识，无法扩成集群 | 单实例够用；要做主从再换 StatefulSet |
| 6 | 镜像用 `latest` + 手动导入 | 无法追溯版本，集群重建要重来 | 建私有仓库（Harbor），用 commit hash 打 tag |
| 7 | **gateway 没有 `/health` 端点** | 只能用 `tcpSocket` 探活，**测不出路由表是否加载成功** | 加 `/health`，返回当前配置版本号 |
| 8 | 无 NetworkPolicy | Pod 之间全通，横向移动风险大 | 只放行 gateway → postgres / redis |
| 9 | 无监控采集 | 9090 上的 metrics 没人抓 | 部署 Prometheus + Grafana，或接入现有监控 |

> 第 7 条是代码层面的真实缺口：auth 中间件里 `/health` 在白名单中，但没有对应的 handler。

### AI 部分的范围

```
内置适配：智谱、DeepSeek（OpenAI 兼容协议）
另有 mockai 假上游，用于验证 SSE 流式与恶劣分片（不需要真实 API Key）

未做：Claude / Gemini 等非 OpenAI 协议的原生适配
```

---

## 后续计划

1. 连续压测劣化 —— **已完成**：定位为两类原因——压测端不复用连接耗尽自身端口 + 网关连接池上限小于并发数；后者已改为动态跟随 `max_concurrency`
2. 性能优化 —— **已落地**：鉴权 TTL 缓存、限流合并（两次 Redis 往返 → 一次 Lua）、日志缓冲、连接池参数、指标基数收敛
3. 压 AI 流式接口（`/v1/chat/completions`）—— **待做**：补上未覆盖的部分，拿 TTFT 与流式吞吐数据
4. 长时 / 真实负载 / 多实例压测 —— 验证连接池参数在恶劣场景的收益，以及分布式限流的多实例配额一致性
5. 学习 LLMLite 与 New API 的框架设计，完善 AI 网关的多厂商接入

---

## 参考项目与设计来源

1. Nginx
2. Envoy
3. Kong
