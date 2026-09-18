# FlowGate 开发进度清单

> 本文件记录整个项目从零到完成的每一步。每完成一步，就在 `[ ]` 里打勾变成 `[x]`。
> 版本路线遵循《FlowGate_项目规划书_聚焦实施版.md》第 5 节。
>
> **最后更新：V5 错误路径验证 + 鉴权失败 body 分流之后。V0~V4 的状态表之前长期过时（写着"未开始"但实际已完成），本次一并订正。**

---

## 版本路线总览

```text
V0 静态反向代理（地基）
  → V1 可配置网关 MVP（PostgreSQL 持久化 + 配置发布）
  → V2 负载均衡 / 健康检查 / 动态路由（不可变快照）
  → V3 超时 / 重试 / 熔断 / 过载保护（核心完成线）
  → V4 分布式限流 / Redis 故障回退 / pprof（平台强化线）
  → V5 OpenAI 兼容 / Provider / SSE（AI 扩展）
```

| 版本 | 定位 | 状态 |
|---|---|---|
| V0 | 地基 | ✅ 完成 |
| V1 | 完整 MVP | ✅ 完成（基础健康检查除外，见下） |
| V2 | 服务治理基础 | ✅ 完成 |
| V3 | **核心完成线** | ⏳ 主体完成，**3 项遗留未做**（见下） |
| V4 | 平台强化线 | ⏳ 主体完成，缺压测报告 |
| V5 | AI 扩展 | ⏳ 主体完成 + 错误路径已验，**回归仅冒烟** |

---

## V0：最小反向代理验证版 ✅

> 目标：网关能接收请求、正确转发到上游，并保留状态码、响应头、响应体。

- [x] 0. 确认 Go 环境
- [x] 1. 初始化模块 `go mod init github.com/bobdfy/flowgate`
- [x] 2. 创建目录 `cmd/gateway`、`cmd/mock`
- [x] 3. 写静态路由配置
- [x] 4. 写 Mock 上游服务（回显/慢/故障/健康）
- [x] 5. 写网关主程序（RoundTrip + 中间件）
- [x] 6. 装依赖
- [x] 7. 跑起来验证
- [x] 8. V0 验收：GET/POST 转发、body 不丢、状态码一致、超时取消、客户端断开退出

---

## V1：可配置网关 MVP ✅

> 目标：配置可持久化、路由可发布、请求可验证。引入 PostgreSQL。

- [x] 服务 / 实例 / 路由管理 API
- [x] PostgreSQL 持久化（服务、实例、路由、配置版本）
- [x] 配置草稿 → 校验 → 原子发布 → 回滚
- [x] 数据面加载完整配置快照（旧请求旧快照、新请求新快照）
- [ ] **基础健康检查** —— 实际已在 V2 用主动健康检查实现（`internal/loadbalance/checker.go`），此项可视为已完成
- [x] 请求日志 + Request ID
- [x] Mock 上游故障注入接口
- [x] 验收：配置发布期间请求不读到半旧半新状态

---

## V2：负载均衡、健康检查与动态路由 ✅

- [x] 服务多实例 + 实例权重
- [x] Round Robin / Weighted Round Robin（按权重展开成循环队列）
- [x] 主动健康检查（`checker.go`，5s 间隔 / 2s 超时 / `/health`）
- [x] 被动信号（超时 / 连接失败 / 5xx）
- [x] 实例摘除与恢复
- [x] 路由优先级 + Host/Method/Path 匹配
- [x] 不可变 RouteTable + `atomic.Value` 原子替换
- [x] 验收：配置发布不中断请求、单请求只见一个版本、异常实例可摘除恢复

---

## V3：超时、重试、熔断与故障隔离 ⏳ 主体完成，有遗留

- [x] 分层超时（建连 / 响应头 / 整体 Deadline）
- [x] 幂等约束下的有限重试 + 指数退避 + Jitter + 总重试预算
- [x] 熔断器（Closed / Open / Half-Open）
- [x] 故障隔离（服务级熔断、被动 + 主动探测）
- [x] Prometheus 指标（请求计数 / 耗时、限流、熔断状态）
- [x] 核心故障实验：超时 → 504、连续 5xx 熔断 → 503、幂等重试、异常实例
- [ ] **场景④ 连接拒绝 → 502 未验证**
- [ ] **过载保护（最大排队 / 丢弃 / 快速失败）未实现**
- [ ] **Bulkhead / 路由级并发限制未实现**

> 这三项的实施步骤写在 `V3_收尾实施说明.md`（含迁移文件、`internal/overload` 护栏设计、验收清单）。
> **不是技术阻塞，是优先级——V5 插队了。**

---

## V4：分布式限流与多实例强化 ⏳

- [x] 本地 Token Bucket（单实例快速限流 + Redis 故障时 Local Fallback）
- [x] Redis + Lua 滑动窗口计数（API Key / 租户两级限流）
- [x] Redis 故障回退策略（Fail-Closed / Local Fallback / Fail-Open）
- [x] 多 FlowGate 实例全局额度验证（15 请求 → 10 放行 + 5 拒绝）
- [x] pprof（`:6060`）
- [x] Prometheus 指标（`:9090/metrics`）
- [ ] **压测报告未做**
- [ ] **Redis 不可用 / 突发流量对比实验（本地桶 vs Redis 滑窗）未做**

---

## V5：AI 网关扩展 ⏳ 主体完成

> 目标：让 FlowGate 支持 AI 请求，并证明**在线流式 AI 请求经过网关时
> 仍然具有正确的取消、错误与重试语义**。
>
> 详细实施步骤见 `AI_Gateway_实施手册.md`。

### 一、OpenAI 兼容接入 ✅

- [x] `internal/ai/api_name.go` —— ApiName 规范化（`{vendor}/{version}/{apitype}`）+ path 映射表
- [x] `internal/ai/request.go` / `response.go` —— OpenAI 请求/响应结构
- [x] `internal/ai/handler.go` —— `POST /v1/chat/completions` + `GET /v1/models`
- [x] 模型白名单（未配置的模型 → 404，不透传上游）
- [x] 模型别名 / 灰度（对外 `flowgate-chat` → 上游 `glm-5.3`）
- [x] 错误响应统一成 OpenAI 格式（客户端 SDK 能解析）

### 二、Provider 适配层 ✅

- [x] 锚接口 `Provider`（只有 `GetProviderType()`）
- [x] 能力接口 `RequestHeadersHandler` / `RequestBodyHandler`（按需实现 + 类型断言）
- [x] `Capabilities` 能力位图（构造期探一次，热路径只读布尔值）
- [x] `internal/provider/openai.go` —— OpenAI 兼容适配器（**智谱、DeepSeek、Qwen、Ollama 都用它**）
- [x] 上游路径可按厂商配置（`UpstreamPath`）—— 智谱是 `/chat/completions`，DeepSeek 是 `/v1/chat/completions`
- [x] 多 provider 同时挂载（`map[对外模型名]*ProviderEntry`）

### 三、SSE 流式代理 ✅

- [x] `internal/sse/framer.go` —— **纯字节 framing 状态机**（tail 缓冲 + pending CR + RESYNC）
- [x] **16 个单测**，含"每个字节偏移切两半"黄金测试
- [x] `internal/ai/stream.go` —— `streamProxy`：逐事件 Flush + 末块 flush + 取消
- [x] 每请求独立 Framer 实例
- [x] 流式响应删 `Content-Length`、加 `X-Accel-Buffering: no`
- [x] 发上游请求时不带 `Accept-Encoding`（避免被压缩导致没法逐事件解析）
- [x] **端到端验证：4 种恶劣切分下输出与基准完全一致**
  - `sse_split`（事件切两半）✅
  - `sse_delim`（`\r\n\r\n` 切在 CR/LF 之间）✅
  - `sse_no_tail`（上游不补末尾空行，usage 不丢）✅
  - `sse_oversize`（1MiB+ 畸形数据，RESYNC 不连坐）✅

### 四、网关装配与治理复用 ✅

- [x] `cmd/gateway/ai.go` —— `buildAIRouter` / `buildAIHandler` / `aiRouter`
- [x] 分流器插在「鉴权 + 限流内层、路由外层」
- [x] **AI 请求共享已有的租户 / API Key / 限流**（这三个模块一行未改）
- [x] 配置错时降级（不 `log.Fatal`，普通代理照常跑）
- [x] `cmd/mockai` —— OpenAI 兼容假上游 + 10 种故障注入模式
- [x] **鉴权失败的 body 按链路分流**（V5 唯一一处为 AI 动过的治理代码）：
      `RequireAuth` 新增 `UnauthorizedResponder` 函数口，`nil` 时退回原纯文本行为；
      AI 路径回 OpenAI 错误结构（SDK 才能解析出真正原因），普通路径回纯文本。
      判定复用 `ai.ApiNameFromPath`，与 `aiRouter` 共用同一份事实来源。

### 五、验证状态

| 项 | 状态 |
|---|---|
| 编译 / vet / gofmt | ✅ 全绿 |
| `internal/sse` 单测（16 个） | ✅ 全绿 |
| 真上游（智谱 glm-5.3）非流式 | ✅ 通过 |
| 真上游流式 | ✅ 通过 |
| 4 种恶劣切分（经网关） | ✅ 全部与基准一致 |
| 客户端断开 → 上游停止生成 | ✅ 通过（网关 `stream_client_gone` + mockai 侧写失败） |
| 错误路径（404 / 400 / 空 body / 401 / 上游 5xx / 限流） | ✅ **6/6 通过** |
| 鉴权失败 body 分流（8 种组合） | ✅ 通过（AI→JSON / 普通→纯文本，状态码一致） |
| 回归（关掉 AI 跑 V1~V4 验收） | ⬜ **只做了冒烟**（有效 key 能穿过鉴权进真实路由），全量未做 |

### 六、V5 明确没做的事（有意跳过）

- 多厂商原生协议适配（Claude / Ollama 原生格式）
- Token 用量落库、成本计算、租户额度（计费后台）
- token 级多 key failover 池
- 模型权重路由 / 灰度发布

> 理由见 `AI_Gateway_实施手册.md` 第 9 节。这些与 V5 的完成标准（证明流式语义正确）无关，
> 属于"有真实需求时再考虑"。

---

## 长期不做（不作为完成前置条件）

- [ ] 控制面 / 数据面拆分（V6）
- [ ] Kubernetes（V6）
- [ ] WASM 插件（V6）
- [ ] 完整多租户计费
- [ ] 复杂管理后台

---

## 当前最该做的三件事

```
1. 回归：关掉 AI 跑一遍 V1~V4 验收 —— 证明「没打坏老链路」（目前只有冒烟）
2. V3 那三条遗留（按 V3_收尾实施说明.md）—— 核心完成线还缺一块
3. V4 压测报告 —— 唯一能拿出真实性能数字的地方
```
