# FlowGate 开发进度清单

> 本文件记录整个项目从零到完成的每一步。每完成一步，就在 `[ ]` 里打勾变成 `[x]`。
> 版本路线遵循《FlowGate_项目规划书_聚焦实施版.md》第 5 节。

## 版本路线总览

```text
V0 静态反向代理（地基）
  → V1 可配置网关 MVP（PostgreSQL 持久化 + 配置发布）
  → V2 负载均衡 / 健康检查 / 动态路由（不可变快照）
  → V3 超时 / 重试 / 熔断 / 过载保护（核心完成线）
  → V4 分布式限流 / Redis 故障回退 / pprof（平台强化线）
  → V5 OpenAI 兼容 / Provider / SSE（AI 可选扩展）
```

| 版本 | 定位 | 状态 |
|---|---|---|
| V0 | 地基 | ✅ 完成 |
| V1 | 完整 MVP | ⚪ 未开始 |
| V2 | 服务治理基础 | ⚪ 未开始 |
| V3 | **核心完成线** | ⚪ 未开始 |
| V4 | 平台强化线 | ⚪ 未开始 |
| V5 | AI 可选扩展 | ⚪ 未开始 |

---

## V0：最小反向代理验证版（当前阶段）

> 目标：网关能接收请求、正确转发到上游，并保留状态码、响应头、响应体。

- [x] 0. 确认 Go 环境（go1.26.5）
- [x] 1. 初始化模块 `go mod init github.com/bobdfy/flowgate`
- [x] 2. 创建目录 `cmd/gateway`、`cmd/mock`
- [x] 3. 写静态路由配置 `config.yaml`
- [x] 4. 写 Mock 上游服务 `cmd/mock/main.go`（回显/慢/故障/健康）
- [x] 5. 写网关主程序 `cmd/gateway/main.go`（ReverseProxy + 中间件）
- [x] 6. 装依赖 `go get gopkg.in/yaml.v3` + `go mod tidy`
- [x] 7. 跑起来验证（起 Mock → 起网关 → curl 转发）
- [x] 8. V0 验收：GET/POST 转发、body 不丢、状态码一致、超时取消、客户端断开退出

---

## V1：可配置网关 MVP

> 目标：配置可持久化、路由可发布、请求可验证。引入 PostgreSQL。

- [x] 服务 / 实例 / 路由管理 API
- [x] PostgreSQL 持久化（服务、实例、路由、配置版本）
- [x] 配置草稿 → 校验 → 原子发布 → 回滚
- [x] 数据面加载完整配置快照（旧请求旧快照、新请求新快照）
- [ ] 基础健康检查
- [x] 请求日志 + Request ID
- [x] Mock 上游故障注入接口
- [x] 验收：配置发布期间请求不读到半旧半新状态

---

## V2：负载均衡、健康检查与动态路由

> 目标：多实例流量分配、异常实例摘除与恢复、动态路由热更新。

- [ ] 服务多实例 + 实例权重
- [ ] Round Robin / Weighted Round Robin
- [ ] 主动健康检查 + 被动信号（超时/连接失败/5xx）
- [ ] 实例摘除与恢复
- [ ] 路由优先级 + Host/Method/Path 匹配
- [ ] 不可变 RouteTable + atomic.Value 原子替换
- [ ] 验收：配置发布不中断请求、单请求只见一个版本、异常实例可摘除恢复

---

## V3：超时、重试、熔断与故障隔离（核心完成线）

> 目标：从"能转发"升级到"上游异常时保护系统"。

- [ ] 分层超时（建连/TLS/响应头/单次尝试/整体 Deadline）
- [ ] 幂等约束下的有限重试 + 指数退避 + Jitter + 总重试预算
- [ ] 熔断器（Closed/Open/Half-Open）
- [ ] 故障隔离（服务级/实例级熔断、路由并发限制、Bulkhead、快速失败）
- [ ] 过载保护（最大排队、丢弃/快速失败）
- [ ] Prometheus / Grafana 指标
- [ ] 核心故障实验（超时、连续 5xx 熔断、非幂等 POST、配置发布压测、异常实例、重试风暴）
- [ ] 验收：重试计入整体超时、流式输出后禁止换上游、半开只放有限探测

---

## V4：分布式限流与多实例强化（平台强化线）

> 目标：多实例共享限流，验证 Redis 价值与故障回退。做深不做全。

- [ ] 本地 Token Bucket（单实例快速限流 + Redis 故障时 Local Fallback）
- [ ] Redis + Lua 滑动窗口计数（API Key / 租户两级限流）
- [ ] Redis 故障回退策略（Fail-Closed / Local Fallback，可选 Fail-Open）
- [ ] 多 FlowGate 实例全局额度验证
- [ ] pprof 性能分析 + 压测报告
- [ ] 必做实验：Redis 不可用、突发流量（本地 vs Redis+Lua 对比）

---

## V5：AI 模型网关扩展（可选，非完成前置）

> 目标：证明在线流式 AI 请求经过网关时仍有正确的取消/错误/重试语义。

- [ ] OpenAI 兼容 Chat 接口
- [ ] Provider 适配层（MockProvider + OpenAICompatibleProvider）
- [ ] SSE 流式代理（首 Token 延迟、Flush、客户端断开、Usage 统计）
- [ ] Token Usage 统计
- [ ] 验收：SSE 客户端中途断开后 Context 取消、goroutine 回收、Usage 正确

---

## 长期不做（不作为完成前置条件）

- [ ] 控制面 / 数据面拆分（V6）
- [ ] Kubernetes（V6）
- [ ] WASM 插件（V6）
- [ ] 完整多租户计费
- [ ] 复杂管理后台

> 以上仅在主线（V0~V3）做透、且确有余力时再考虑。
