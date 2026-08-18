# FlowGate 项目规划书（聚焦实施版）

## 1. 项目定位

**项目名称：FlowGate**

**GitHub 仓库名建议：**

```text
flowgate
```

FlowGate 是一个面向内部微服务与 AI 模型服务的高性能 API 网关。它接收客户端请求，根据动态路由规则将流量转发到后端服务，并通过服务发现、负载均衡、超时控制、限流、熔断、重试、灰度发布、流式代理和链路追踪，处理以下问题：

- 多个上游服务需要统一接入
- 后端实例动态上下线
- 单个服务延迟升高或频繁失败
- 突发流量压垮下游服务
- 多个租户争抢共享资源
- 重试放大流量并形成重试风暴
- 配置变更期间出现新旧规则混用
- 客户端断开后下游请求仍持续执行
- SSE 流式连接中途断开
- 某个异常实例拖慢整个服务
- 请求链路过长但无法定位瓶颈
- 非幂等请求被错误重试
- 网关自身过载后继续无限接收请求
- 多实例网关之间限流状态不一致

项目目标不是替代 Nginx、Envoy、Kong、APISIX 等成熟网关，而是围绕“请求转发的正确性、服务治理、故障隔离和性能可验证性”完成一个适合 Go 后端、平台中间件和 AI 基础设施岗位展示的系统。

FlowGate 的重点不是做一个复杂管理后台，也不是简单套用现成反向代理库，而是通过可控的 Mock 服务、故障注入和压测实验，证明网关在超时、重试、熔断、限流、配置热更新和流式代理场景下的行为符合预期。

### 实施范围原则

为了避免项目变成“大而全”的功能堆叠，开发时将能力分为三层：

```text
核心主线：
反向代理 → 动态路由 → 负载均衡 → 健康检查
→ 超时 / 重试 → 熔断 → 可观测性

平台强化：
Redis 分布式限流 → 多实例协调 → pprof 压测优化

可选扩展：
OpenAI 兼容接口 → SSE → Provider 适配
→ 控制面 / 数据面 → Kubernetes / WASM
```

其中“核心主线”必须做透并通过故障实验验证；“平台强化”只选择少量机制做深；“可选扩展”不作为项目完成的前置条件。

---

## 2. 推荐毕设或项目题目

### 具体业务型题目

> 基于 Go 的 AI 模型服务统一接入与流量治理网关设计与实现

### 通用技术型题目

> 面向微服务的高性能 API 网关与服务治理平台设计与实现

### 更偏中间件的题目

> 基于动态路由、限流熔断与链路追踪的分布式 API 网关设计与实现

### 更偏性能研究的题目

> 面向高并发请求的 API 网关流量治理与性能优化研究

建议优先使用第二个或第三个题目。它们既能覆盖普通 Go 后端岗位，也能覆盖平台中间件和 AI 平台后端岗位；项目内部可以使用 AI 模型服务作为真实演示场景，但核心仍保持通用网关设计。

---

## 3. 最推荐的项目场景

### 3.1 AI 模型服务统一接入

一家企业同时使用多个文本模型、Embedding 模型和内部推理服务，但不同服务的请求格式、认证方式、错误码、超时限制和流式协议不一致。

FlowGate 统一提供：

```text
客户端 / Bot 平台 / 内部业务
              ↓
           FlowGate
              ↓
模型服务 A / 模型服务 B / Mock 模型 / 内部工具服务
```

第一阶段只需要接入：

```text
普通 HTTP JSON 服务
SSE 流式服务
Mock 慢服务
Mock 故障服务
```

后期再接入：

```text
OpenAI 兼容模型
本地模型服务
Embedding 服务
内部业务 API
```

不建议第一版同时实现图片、语音、视频、多模态文件上传和完整 Agent 平台。

### 3.2 用户是谁

- AI 应用开发团队
- Bot 平台研发团队
- 内部微服务团队
- 平台基础架构团队
- 需要统一流量治理的 SaaS 团队
- 负责系统稳定性和成本控制的运维团队

### 3.3 当前通常如何解决

- 每个业务直接调用不同后端服务
- 在代码中写死服务地址
- 由 Nginx 配置静态路由
- 每个业务自行实现超时和重试
- 每个服务单独实现限流
- 出现故障后人工切换服务
- 通过日志逐层排查慢请求
- 流式请求由业务服务直接代理

### 3.4 为什么现有方式不够

简单直连或静态代理在以下情况下容易出问题：

- 后端实例扩容后需要手动改配置
- 某个实例异常但仍然继续接收流量
- 所有客户端同时重试造成故障放大
- 单个租户耗尽全部模型并发
- 客户端断开后下游仍然继续生成
- 多个服务重复实现相同的鉴权和限流逻辑
- 不同服务的错误码和协议不统一
- 配置修改时部分请求读取到不完整配置
- 网关只记录总耗时，无法知道具体慢在哪一步
- 已经开始流式输出后错误切换备用服务
- 非幂等 POST 请求被自动重试并重复执行
- 网关过载时没有反压，最终导致级联故障

### 3.5 为什么适合作为简历项目

- 有明确的平台用户和请求链路
- 可以使用本地 Mock 服务稳定复现故障
- 有路由、限流、熔断、负载均衡等经典中间件问题
- 能展示 Go 网络编程和并发能力
- 能做真实压测与性能分析
- 能接入 Prometheus、Grafana、OpenTelemetry
- 可同时匹配普通后端、平台后端和中间件岗位
- AI 模型服务场景与当前业务趋势相关
- 不依赖真实生产数据
- 容易通过 Docker Compose 公开演示

---

## 4. 系统的核心保证

FlowGate 不建议宣称“所有请求绝不丢失”或“所有请求 Exactly Once”。

更合理的描述是：

> 网关在单次请求范围内保证路由配置快照一致、超时与取消信号可传播、重试行为受幂等性约束；通过健康检查、熔断、限流和过载保护隔离异常上游，并通过监控与链路追踪使请求失败和性能瓶颈可以被检测、解释和复现。

系统需要维持以下不变量：

1. 单个请求在处理期间只能使用一个完整的路由配置版本。
2. 配置更新不能让请求读取到一半旧配置、一半新配置。
3. 客户端取消请求后，下游请求和相关 goroutine 必须能够及时退出。
4. 已经向客户端发送流式内容后，不能无条件切换到另一个上游重新生成。
5. 非幂等请求默认不能自动重试。
6. 同一个重试预算不能在多层服务中无限叠加。
7. 某个上游实例持续失败时，不能继续向其发送全部流量。
8. 某个租户或 API Key 不能无限占用平台总并发。
9. 网关过载时必须拒绝、排队或降级，不能无限创建连接和 goroutine。
10. 限流和配额状态异常时必须有明确的 Fail-Open 或 Fail-Closed 策略。
11. 负载均衡不能将流量长期集中到单个异常实例。
12. 请求必须拥有可追踪的 Request ID 或 Trace ID。
13. 网关实例重启后，持久配置和审计信息不能丢失。
14. 高频运行状态不应全部写入 PostgreSQL，避免数据库成为请求链路瓶颈。
15. 所有性能优化必须通过压测数据、pprof 或链路指标验证。

---

# 5. 项目版本路线

## V0：最小反向代理验证版

### 目标

证明最核心的一件事：

> 网关能够接收请求，并将请求正确转发到指定上游，同时保留状态码、响应头和响应体。

### 功能

实现一个最小 HTTP 反向代理：

```text
Client
  ↓
FlowGate
  ↓
Mock Upstream
```

提供静态配置：

```yaml
routes:
  - path: /api/users
    upstream: http://mock-service:8081
```

支持：

- 路径匹配
- 请求头转发
- Query 参数转发
- 请求体转发
- 响应状态码转发
- 响应头转发
- 基础访问日志
- Request ID
- 上游连接超时
- 总请求超时

### 技术栈

- Go
- net/http
- httputil.ReverseProxy 或自定义代理
- Mock HTTP 服务

### 暂不使用

- PostgreSQL
- Redis
- RabbitMQ
- Prometheus
- OpenTelemetry
- Kubernetes
- 服务发现
- 动态路由
- 管理前端

### 必须验证

- GET、POST、PUT、DELETE 是否能够正常转发
- 请求体是否被重复读取或丢失
- 上游状态码是否保持一致
- Hop-by-Hop Header 是否正确处理
- 上游连接失败时是否返回明确错误
- 请求超时后下游连接是否被取消
- 客户端主动断开后 goroutine 是否退出

---

## V1：可配置网关 MVP

### 目标

形成一个“配置可持久化、路由可发布、请求可验证”的基础 API 网关。

这一阶段的重点是配置与数据面的正确衔接，不把时间主要花在管理前端。

### 必做能力

- 创建和管理上游服务
- 添加或停用服务实例
- 创建路由并配置 Method、Path 和基础超时
- PostgreSQL 持久化服务、实例、路由和配置版本
- 配置草稿、校验、发布和回滚
- 基础健康检查
- 请求日志与 Request ID
- Mock Upstream 故障注入接口

### 管理界面原则

管理前端不是主线。

优先级如下：

```text
第一优先：管理 API + curl / Postman 可完整操作
第二优先：最小管理页，能完成配置与查看状态
第三优先：UI 美化、复杂权限、图表交互
```

如果实现页面，只保留 4～5 个核心页面：

1. 服务与实例
2. 路由配置
3. 配置发布与版本
4. 健康状态
5. 故障注入 / 请求日志（可合并）

不在 V1 处理复杂 RBAC、多租户、计费、运营后台。

### 模块

```text
FlowGate
├── 管理 API
├── 配置仓储（PostgreSQL）
├── 配置发布与版本
├── 路由匹配
├── 反向代理
├── 健康检查
├── 请求日志
└── Mock / 故障注入
```

### 技术栈

- Go
- net/http、Gin 或 Hertz（二选一，不为框架而框架）
- PostgreSQL
- Docker Compose
- 可选的极简 Vue / React 管理页

### Redis

不需要。

### RabbitMQ

不需要。

### 配置发布原则

配置不能直接边改边生效。

```text
编辑草稿
→ 校验引用关系
→ 生成完整版本
→ 原子发布
→ 数据面加载完整快照
→ 旧请求继续使用旧快照
→ 新请求使用新快照
```

V1 结束时，最重要的验收标准不是“页面齐不齐”，而是配置发布期间请求不会读到半旧半新的路由状态。

## V2：负载均衡、健康检查与动态路由版

### 目标

解决同一服务多个上游实例之间的流量分配，以及异常实例的摘除和恢复问题。

### 必做负载均衡策略

第一版只做：

- Round Robin
- Weighted Round Robin

它们足以验证负载分配、权重和实例上下线。

以下作为实验或后续选做，不要求同时实现：

- Least Connections
- Consistent Hash
- 基于延迟的动态选择

### 健康检查

主动检查：

```text
FlowGate
→ 定期请求 /health
→ 连续失败达到阈值
→ 实例标记 Unhealthy
→ 停止分配新请求
→ 连续成功后恢复
```

被动信号：

- 请求超时
- 连接失败
- 5xx
- 连接被重置

### 必做能力

- 服务多实例
- 实例权重
- 动态上下线
- 主动健康检查
- 基础被动健康信号
- 实例摘除与恢复
- 路由优先级
- Host / Method / Path 匹配
- 配置热更新

### 配置热更新

推荐使用不可变快照：

```text
PostgreSQL 配置
→ 构建完整 RouteTable
→ 校验
→ atomic.Value / 原子指针替换
→ 新请求读取新版本
→ 旧请求继续持有旧版本
```

不要在请求路径里直接读取一组正在修改的 map。

### 验收重点

- 配置发布不能中断正在执行的请求
- 单次请求只能看到一个完整配置版本
- 异常实例能够被摘除，恢复后能够重新加入
- 权重分配结果在足够样本下符合预期

### 技术栈

- Go
- PostgreSQL
- sync / atomic.Value
- Docker Compose

### Redis

仍然不需要。

## V3：超时、重试、熔断与故障隔离版

### 目标

从“能够转发请求”升级到“能够在上游异常时保护系统”。

### 新增超时控制

分层设置：

- 建连超时
- TLS 握手超时
- 等待响应头超时
- 空闲连接超时
- 单次尝试超时
- 整体请求超时

### 新增重试策略

只对以下情况考虑重试：

- 连接建立失败
- 上游连接被重置
- 可重试的 502、503、504
- 明确允许重试的 429
- 幂等 GET、HEAD、PUT、DELETE
- 带有 Idempotency-Key 且后端支持幂等的请求

默认不重试：

- 已经开始向客户端发送响应
- 普通非幂等 POST
- 业务参数错误
- 认证失败
- 请求体不可重放
- 达到总重试预算

### 新增熔断器

状态：

```text
Closed
→ Open
→ Half-Open
→ Closed / Open
```

指标：

- 连续失败次数
- 时间窗口错误率
- 慢请求比例
- 最小请求数量
- Open 持续时间
- Half-Open 探测数量

### 新增故障隔离

- 服务级熔断
- 实例级熔断
- 路由级并发限制
- 上游 Bulkhead
- 最大排队长度
- 请求丢弃或快速失败
- 重试预算
- 指数退避
- 随机抖动

### 需要保证

1. 重试次数必须包含在整体超时内。
2. 多个网关层不能无限重复重试。
3. 已经流式输出后不能重新请求备用服务。
4. 熔断状态切换必须并发安全。
5. 半开状态只能放行有限探测请求。
6. 故障服务不能持续占用全部连接池。

### 技术栈

- Go
- PostgreSQL
- Prometheus
- Docker Compose
- 管理前端

---

## V4：分布式限流与多实例强化版

### 目标

当 FlowGate 运行多个实例时，引入 Redis 解决“每个网关各自计数导致全局限流失真”的问题。

V4 不追求把所有限流算法都实现一遍，而是选择两种代表性方案做深、做实验、做故障回退。

### 只要求实现两类限流

#### 1. 本地 Token Bucket

用途：

- 单实例快速限流
- Redis 故障时 Local Fallback
- 对比本地与分布式方案的附加延迟

#### 2. Redis + Lua 滑动窗口计数

用途：

- 多 FlowGate 实例共享 API Key / 租户额度
- 保证“检查 + 计数 + 过期”原子执行
- 验证 Redis 故障时的降级策略

以下算法只作为阅读或性能实验备选，不作为完成条件：

- Fixed Window
- Sliding Window Log
- Leaky Bucket
- 复杂分布式 Token Bucket

### 限流层级

主线只实现：

```text
API Key QPS
+ 租户 QPS / 配额
```

有余力再补：

```text
上游服务全局并发
或
平台总并发
```

不要求一开始同时实现平台、租户、API Key、路由、上游五层限流。

### Redis 的职责

Redis 只保存高频、短期、可重建的协调状态，例如：

- 多实例共享请求计数
- API Key / 租户短期配额
- 可选的全局并发计数
- 配置版本通知

最终可靠配置、租户信息、审计记录仍然保存在 PostgreSQL。

### Redis 不可用策略

必须至少实现并测试两种模式：

```text
Fail-Closed
Local Fallback
```

Fail-Open 可以作为第三种可选策略。

示例：

- 付费或高风险接口：Fail-Closed
- 一般内部接口：Local Fallback
- 低风险只读接口：可选 Fail-Open + 告警

### 必做实验

比较：

```text
本地 Token Bucket
VS
Redis + Lua 滑动窗口
```

观察：

- 限流准确性
- 突发流量行为
- P95 / P99 附加延迟
- Redis 操作次数
- 多实例下是否保持全局额度
- Redis 不可用时请求成功率与限流准确性

### 技术栈

- Go
- PostgreSQL
- Redis
- Lua
- Prometheus
- Grafana
- Docker Compose
- pprof

## V5：可选 AI 模型网关扩展版

### 定位

V5 不属于 FlowGate 核心完成条件。

只有当 V3 / V4 已经稳定，且希望向 AI 平台后端方向扩展时，再增加 AI 模型服务能力。

### 最小 AI 扩展范围

只推荐实现四项：

1. OpenAI 兼容 Chat 接口
2. Provider 适配层
3. SSE 流式代理
4. Token Usage 统计

```text
Client
→ FlowGate
→ Provider Adapter
→ Model Provider
→ SSE / JSON Response
```

### Provider 接口建议

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
    HealthCheck(ctx context.Context) error
}
```

第一版实现：

```text
MockProvider
+ OpenAICompatibleProvider
```

LocalModelProvider 可选。

### SSE 必须处理

- 首 Token 延迟
- Flush
- 客户端断开
- 下游 Context 取消
- 流式中途错误
- goroutine / 连接回收
- 最终 Usage 统计
- 已经输出后禁止透明重试
- 慢客户端的基本写超时或反压策略

### 暂不作为主线

- 完整多租户成员与角色系统
- 复杂模型成本路由
- 最低延迟 / 最低成本动态路由
- 完整计费后台
- Bot / Agent 平台
- 多模态上传

### 完成标准

V5 的价值在于证明“在线流式 AI 请求经过网关时仍然具有正确的取消、错误与重试语义”，而不是把模型功能做得很多。

## V6：长期可选的平台化扩展

### 定位

V6 只作为长期路线，不计入主线项目完成度。

只有出现真实需求时才考虑：

- 控制面和数据面需要独立扩容
- 配置发布与请求代理需要独立发布
- 多数据面实例需要可靠配置分发
- Kubernetes 环境确实用于运行和测试

### 可选方向

#### 控制面 / 数据面

```text
Control Plane
- 服务 / 路由 / 策略
- 配置版本
- 发布 / 回滚

Data Plane
- 路由匹配
- 反向代理
- 负载均衡
- 超时 / 重试 / 熔断 / 限流
- 可选 SSE
```

#### 插件

优先：

- 编译期注册
- 声明式插件配置

WASM 只作为长期探索，不作为项目目标。

#### Kubernetes

可选验证：

- Readiness / Liveness
- 优雅终止
- 滚动更新
- HPA

不要为了“简历里有 K8s”而引入 Kubernetes。

### 原则

当 V3 / V4 的正确性、故障实验和压测结果还不完整时，不进入 V6。

## 6. 版本与技术对应表

| 版本 | 核心内容 | PostgreSQL | Redis | Prometheus | OpenTelemetry | 是否主线 |
|---|---|---:|---:|---:|---:|---:|
| V0 | 静态反向代理、取消与超时 | 否 | 否 | 否 | 否 | 是 |
| V1 | 配置持久化、版本发布、基础健康检查 | 是 | 否 | 可选 | 否 | 是 |
| V2 | 多实例、负载均衡、动态路由、配置快照 | 是 | 否 | 可选 | 可选 | 是 |
| V3 | 分层超时、幂等重试、熔断、过载保护 | 是 | 否 | 是 | 可选 | 是 |
| V4 | 多实例分布式限流、Redis 故障回退、pprof | 是 | 是 | 是 | 可选 | 强化主线 |
| V5 | OpenAI 兼容接口、Provider、SSE、Usage | 是 | 可复用 | 是 | 推荐 | AI 可选 |
| V6 | 控制面/数据面、K8s、插件 | 是 | 是 | 是 | 是 | 长期可选 |

# 7. 完成范围建议

## 最低可展示版本

完成 V1：

- 反向代理
- PostgreSQL 配置持久化
- 服务 / 实例 / 路由管理 API
- 配置版本与原子发布
- 基础健康检查
- Mock 故障服务

此时已经是完整 MVP，但技术深度有限。

## 核心完成版本

完成到 V3：

- 动态路由与不可变快照
- Round Robin / Weighted Round Robin
- 主动健康检查与实例摘除
- 分层超时
- 幂等约束下的有限重试
- 指数退避与抖动
- 熔断与 Half-Open 探测
- 基础过载保护
- Prometheus / Grafana
- 一组完整故障实验

**V3 是最重要的完成节点。**

如果 V3 做得扎实，不需要为了“版本数字更高”立刻继续加功能。

## 推荐平台强化版本

在 V3 稳定后完成 V4，但只做深：

```text
本地 Token Bucket
+ Redis + Lua 滑动窗口
+ API Key / 租户两级限流
+ Redis 故障回退
+ 多网关实例验证
+ pprof / 压测
```

V4 的目标是把 Redis 的价值和代价说明白，而不是收集所有限流算法。

## AI 平台可选版本

仅补充 V5 的最小能力：

- OpenAI 兼容接口
- Provider 适配
- SSE 流式代理
- Token Usage

不要求同时完成复杂多租户、计费和模型成本路由。

## 不作为项目完成前置条件

- 控制面 / 数据面拆分
- Kubernetes
- WASM 插件
- 完整多租户后台
- 复杂计费
- 完整灰度发布平台

这些内容只有在主线已经完成并且确实有时间时再做。

# 8. 完整请求流程

```text
1. 客户端发送请求

2. FlowGate 生成或读取 Request ID / Trace ID

3. 执行基础请求校验
   - 请求体大小
   - Header 大小
   - 路径合法性

4. 执行鉴权
   - API Key
   - JWT
   - 内部服务身份

5. 读取当前完整路由快照

6. 匹配路由
   - Host
   - Path
   - Method
   - Header
   - 优先级

7. 检查租户与 API Key 配额

8. 检查路由级和服务级限流

9. 检查熔断器状态

10. 选择上游实例
    - 健康状态
    - 权重
    - 当前连接数
    - 路由策略

11. 创建带整体 Deadline 的下游请求

12. 转发请求

13. 上游失败时
    - 判断错误是否可重试
    - 判断请求是否幂等
    - 判断请求体是否可重放
    - 检查剩余重试预算
    - 指数退避
    - 随机抖动
    - 选择新的上游实例

14. 上游开始返回时
    - 普通响应：转发状态码、Header 和 Body
    - SSE 响应：逐块读取并 Flush

15. 客户端断开时
    - Context 取消
    - 关闭下游请求
    - 释放连接和 goroutine

16. 请求完成后
    - 更新熔断指标
    - 更新请求指标
    - 记录审计信息
    - 记录 Token 用量（AI 请求）
    - 完成 Trace
```

---

# 9. 核心数据结构

## users

```text
id
email
password_hash
status
created_at
```

## tenants

```text
id
name
status
created_at
```

## tenant_members

```text
tenant_id
user_id
role
created_at
```

## gateway_services

```text
id
tenant_id
name
protocol
connect_timeout_ms
request_timeout_ms
retry_policy_id
circuit_policy_id
enabled
created_at
updated_at
```

## upstream_nodes

```text
id
service_id
address
weight
enabled
health_status
last_health_check_at
created_at
updated_at
```

## gateway_routes

```text
id
tenant_id
service_id
name
host
path_pattern
path_match_type
methods
priority
strip_prefix
enabled
created_at
updated_at
```

## route_versions

```text
id
tenant_id
version
status
config_hash
created_by
created_at
published_at
```

## route_version_items

```text
version_id
resource_type
resource_id
resource_snapshot
```

## api_keys

```text
id
tenant_id
name
key_hash
status
expires_at
created_at
last_used_at
```

## rate_limit_policies

```text
id
tenant_id
name
algorithm
scope
limit_value
window_seconds
burst
fail_mode
created_at
updated_at
```

## circuit_breaker_policies

```text
id
name
failure_threshold
error_rate_threshold
minimum_requests
open_duration_ms
half_open_max_requests
slow_request_threshold_ms
created_at
updated_at
```

## retry_policies

```text
id
name
max_attempts
per_try_timeout_ms
base_backoff_ms
max_backoff_ms
jitter
retry_status_codes
idempotent_only
created_at
updated_at
```

## request_audit_logs

```text
id
request_id
trace_id
tenant_id
api_key_id
route_id
service_id
upstream_node_id
method
path
status_code
error_code
retry_count
duration_ms
first_byte_ms
created_at
```

注意：

- 这里只记录审计级或采样请求，不建议所有高频访问日志都同步写入 PostgreSQL。
- 高频访问日志可以输出到 stdout，再由日志系统收集。
- Prometheus 保存聚合指标。
- OpenTelemetry 保存链路信息。
- PostgreSQL 保存配置、审计和结算级记录。

### AI 扩展数据结构说明

以下 `model_providers`、`model_routes`、`usage_records` 只在实现 V5 时加入；V0～V4 不需要提前建表。

## model_providers

```text
id
tenant_id
name
provider_type
base_url
credential_encrypted
enabled
created_at
updated_at
```

## model_routes

```text
id
tenant_id
name
model_alias
routing_strategy
primary_provider_id
fallback_provider_id
enabled
created_at
updated_at
```

## usage_records

```text
id
request_id
tenant_id
api_key_id
provider_id
model
input_tokens
output_tokens
total_tokens
estimated_cost
status
created_at
```

唯一键建议：

```text
request_id
```

用于防止重试、回调或异步结算造成重复计费。

---

# 10. 为什么这样选择技术

## 10.1 为什么核心代理使用 Go

FlowGate 的核心请求链路需要：

- 大量并发连接
- 低额外延迟
- Context 取消传播
- SSE 长连接
- 可控的 goroutine 生命周期
- 内置 HTTP 客户端和服务端
- pprof 性能分析
- 单二进制部署

Go 的 net/http、context、sync、atomic 和 pprof 很适合实现此类项目。

项目不需要为了“高性能”绕开标准库。

第一版应优先基于标准库实现正确行为，再通过压测确认真正瓶颈。

## 10.2 为什么配置放 PostgreSQL

PostgreSQL 负责保存：

- 用户和租户
- 服务和路由
- 配置版本
- 限流策略
- 重试策略
- 熔断策略
- API Key 元数据
- 审计记录
- 用量结算记录

这些数据要求：

- 事务
- 版本
- 审计
- 查询
- 持久化
- 回滚

不适合只保存在 Redis 或内存中。

## 10.3 为什么 Redis 从 V4 再加入

单实例时期，本地限流和 PostgreSQL 配置已经足够。

只有当多个 FlowGate 实例需要共享以下状态时，Redis 才有明确价值：

- 全局 QPS
- 全局并发
- 租户配额
- API Key 配额
- 短期熔断状态
- 配置变更通知

不建议为了技术栈在 V0 或 V1 强行加入 Redis。

## 10.4 为什么核心请求链路不使用 RabbitMQ

普通 API 请求和 SSE 请求是在线同步链路：

```text
客户端等待
→ 网关转发
→ 上游返回
```

如果核心链路强行经过 RabbitMQ：

- 请求延迟增加
- 流式响应困难
- 请求取消难传播
- 错误语义更复杂
- 在线请求与异步任务混淆

RabbitMQ 只适合用于：

- 异步审计日志
- 用量聚合
- 报表生成
- 离线告警
- 异步通知
- 长任务

因此，FlowGate 核心代理链路不需要消息队列。

## 10.5 为什么配置使用不可变快照

如果请求处理过程中直接读取正在修改的 map，可能出现：

- 路由存在但对应服务不存在
- 服务已更新但实例列表仍是旧版本
- 限流策略与路由版本不一致
- 并发读写导致数据竞争

推荐流程：

```text
数据库配置
→ 构建完整配置对象
→ 校验引用关系
→ 计算 Hash
→ 原子替换快照
```

请求只读取当前快照，不修改快照。

---

# 11. 故障实验

故障实验分成“核心必做”和“扩展选做”。项目是否有技术说服力，主要看核心必做实验是否能复现、解释并记录指标。

## 核心必做一：上游连接 / 响应超时

Mock 服务故意延迟或不返回。

检查：

- 单次尝试超时是否生效
- 整体 Deadline 是否生效
- Context 是否传递到下游
- goroutine 是否退出
- 连接是否释放
- 是否出现无限重试

## 核心必做二：连续 5xx 与熔断

Mock 服务持续返回 500 / 503。

检查：

- 是否达到错误阈值
- Closed → Open 是否正确
- Open 是否快速失败
- Half-Open 是否只放有限探测
- 服务恢复后能否回到 Closed
- 其他上游是否受到影响

## 核心必做三：非幂等 POST 与错误重试

构造“每调用一次都会创建记录”的 Mock POST。

先演示错误方案：自动重试导致重复执行。

再改为：

```text
普通非幂等 POST 默认不重试
或
后端支持 Idempotency-Key 时才允许重试
```

检查重复执行风险和请求语义。

## 核心必做四：配置发布期间持续压测

```text
旧版本：/api → Service A
新版本：/api → Service B
```

持续请求同时发布新配置。

检查：

- 单个请求是否完整使用 A 或 B
- 是否出现中间状态
- 是否出现路由存在但服务不存在
- 是否发生数据竞争
- Request / Config Version 是否可追踪

## 核心必做五：单个异常实例

一个服务配置三个实例，其中一个随机超时。

检查：

- 健康检查能否识别异常
- 流量是否转移
- 异常实例是否摘除
- 恢复后是否重新加入
- P95 / P99 是否明显改善

## 核心必做六：重试风暴

模拟大量请求同时遇到 503。

比较：

```text
固定间隔重试
VS
指数退避 + 随机抖动
VS
加入总重试预算
```

观察：

- 上游瞬时请求量
- 成功率
- P99
- 恢复时间
- 网关 CPU

## V4 必做：Redis 不可用

在 Redis 限流启用时人为停止 Redis。

比较：

- Fail-Closed
- Local Fallback
- 可选 Fail-Open

观察：

- 请求成功率
- 限流准确性
- 附加延迟
- Redis 恢复后的行为
- 是否产生明确告警

## V4 必做：突发流量

比较：

- 不限流
- 本地 Token Bucket
- Redis + Lua 滑动窗口

观察：

- QPS
- P95 / P99
- 下游错误率
- 拒绝请求数
- goroutine / 内存
- 系统恢复时间

## V5 选做：SSE 客户端中途断开

只有实现 V5 时测试：

- Context 是否取消
- Provider 是否停止生成
- goroutine 是否回收
- Usage 是否正确处理

## 其他选做

- 网关优雅退出
- 慢客户端
- 超大 Request Body
- 长连接规模
- Kubernetes Readiness / 滚动更新

这些实验有价值，但不应挤占 V3 / V4 核心实验时间。

# 12. 实验与性能分析方向

性能实验不追求“测很多”，而追求每个实验都有明确问题、对照组和结论。

## 实验一：负载均衡策略

主比较：

- Round Robin
- Weighted Round Robin

可选加入 Least Connections。

场景：

- 实例性能相同
- 实例性能不同
- 一个实例间歇性变慢

观察：

- 请求分布
- P95 / P99
- 活跃连接
- 错误率
- 异常实例恢复速度

## 实验二：重试策略

比较：

- 不重试
- 固定间隔
- 指数退避 + Jitter
- 加总重试预算

观察：

- 成功率
- 平均延迟 / P99
- 上游总请求量
- 故障恢复时间
- 重复执行风险

## 实验三：配置读取方式

比较：

- 每请求查询 PostgreSQL
- 本地可变 map + 锁
- 不可变快照 + atomic.Value

观察：

- QPS
- P99
- 锁竞争
- 数据竞争
- 配置发布时间
- 内存占用

## 实验四：V4 限流

只重点比较：

- 本地 Token Bucket
- Redis + Lua 滑动窗口

观察：

- 限流准确性
- Burst 行为
- Redis 操作次数
- 附加延迟
- 多实例全局一致性
- Redis 故障时行为

## 实验五：代理与连接池（有真实瓶颈再做）

可比较：

- httputil.ReverseProxy
- 自定义 Transport 参数
- 不同连接池配置

只有压测或 pprof 已经显示代理 / 连接池是瓶颈时，才进行这项优化实验。

## V5 可选：SSE 长连接规模

只有实现 AI 流式代理后再测试 100 / 500 / 1000+ 长连接，并观察内存、goroutine、首 Token 延迟和断开后的资源回收。

# 13. 可选使用场景（用于定位，不进入主线开发）

本节只用于帮助确定业务叙事和未来扩展方向，不应被理解为需要同时实现的功能列表。主线仍以 V0～V4 为准。

## 场景 A：内部微服务 API 网关

### 用户

拥有多个后端服务的研发团队。

### 功能

- 路由
- 鉴权
- 负载均衡
- 健康检查
- 限流
- 熔断
- 灰度发布

### 优点

- 后端岗位容易理解
- 技术边界清楚
- 不依赖第三方模型

### 风险

容易被认为是简化版 Nginx，需要突出配置快照、故障实验和性能分析。

### 推荐程度

**★★★★★**

---

## 场景 B：AI 模型统一接入网关

### 用户

Bot 平台、AI 应用团队和内部模型团队。

### 功能

- OpenAI 兼容协议
- Provider 适配
- SSE
- Token 用量
- 模型路由
- 主备降级
- 配额

### 优点

- 与 AI 平台岗位匹配
- 流式链路有差异化
- 可展示高价值稳定性问题

### 风险

容易被误解为模型 API 套壳，必须突出网关中间件能力。

### 推荐程度

**★★★★★**

---

## 场景 C：Webhook 出站投递网关

### 用户

需要向外部客户推送事件的 SaaS 团队。

### 功能

- 目标地址管理
- 签名
- 超时
- 重试
- 熔断
- 限流
- 投递审计

### 优点

可靠性问题突出。

### 风险

Webhook 长任务更适合异步系统，会与 SyncGuard/RabbitMQ 项目产生重合。

### 推荐程度

**★★★☆☆**

---

## 场景 D：多租户开放 API 平台

### 用户

对外提供 API 的 SaaS 企业。

### 功能

- API Key
- 租户配额
- QPS 限制
- 用量统计
- 套餐管理
- 审计

### 优点

产品形态完整，多租户价值清楚。

### 风险

容易把大量时间花在管理后台和计费页面。

### 推荐程度

**★★★★☆**

---

## 场景 E：灰度发布与服务治理网关

### 用户

频繁发布微服务的研发团队。

### 功能

- Header 灰度
- Cookie 灰度
- 权重灰度
- 版本路由
- 故障回滚
- 指标对比

### 优点

服务治理特色明显。

### 风险

需要设计清晰的多版本 Mock 服务。

### 推荐程度

**★★★★☆**

---

## 场景 F：统一出站访问网关

### 用户

需要统一访问第三方 API 的内部服务。

### 功能

- 访问控制
- 凭证托管
- 出站限流
- 超时重试
- 供应商切换
- 审计

### 优点

与 AI 模型供应商接入非常接近，平台价值明确。

### 风险

网络与安全边界需要解释清楚。

### 推荐程度

**★★★★★**

---

# 14. 场景横向比较

| 场景 | 业务易懂 | 技术深度 | 数据可得性 | 页面展示 | 差异化 | 推荐 |
|---|---:|---:|---:|---:|---:|---:|
| 内部微服务网关 | 5 | 5 | 5 | 4 | 4 | 5 |
| AI 模型网关 | 5 | 5 | 4 | 5 | 5 | 5 |
| Webhook 投递网关 | 4 | 5 | 5 | 3 | 4 | 3 |
| 多租户开放 API | 5 | 4 | 5 | 5 | 4 | 4 |
| 灰度发布网关 | 4 | 5 | 5 | 4 | 5 | 4 |
| 统一出站访问网关 | 4 | 5 | 4 | 4 | 5 | 5 |

---

# 15. 场景选择建议

## 最稳妥的后端简历场景

**内部微服务 API 网关**

理由：

- 业务容易理解
- Mock 服务容易实现
- 路由、限流、熔断都合理
- 与 Go 后端岗位直接相关
- 不会过度依赖 AI 概念

## 最匹配 AI 平台岗位的场景

**AI 模型统一接入网关**

理由：

- 有 Provider 适配
- 有 SSE 流式代理
- 有模型路由和降级
- 有 Token 配额
- AI 只是业务背景，核心仍是平台中间件

## 最适合研究服务治理的场景

**灰度发布与服务治理网关**

理由：

- 路由策略丰富
- 可以对比多版本指标
- 故障回滚和配置快照问题自然存在

## 最适合突出 Redis 的场景

**多租户开放 API 平台**

理由：

- 分布式限流
- API Key 配额
- 全局并发
- 实时用量

## 最适合与 SyncGuard 形成互补的场景

**AI 模型统一接入网关**

理由：

- SyncGuard 偏异步数据链路和一致性
- FlowGate 偏在线请求链路和低延迟
- SyncGuard 重点是 RabbitMQ 与 PostgreSQL
- FlowGate 重点是 HTTP、SSE、Redis、熔断和负载均衡
- 两个项目重复度低

---

# 16. 推荐最终方案

推荐把项目拆成“必须完成的通用网关内核”和“按岗位选择的一个扩展”。

### 通用核心（主线）

```text
动态路由
+ 反向代理
+ Round Robin / Weighted Round Robin
+ 健康检查
+ 不可变配置快照
+ 分层超时
+ 幂等约束重试
+ 指数退避 / Jitter
+ 熔断
+ 基础过载保护
+ Prometheus / Grafana
```

### 平台强化（二选一重点做深）

推荐优先：

```text
Redis 分布式限流
+ Local Fallback
+ 多实例一致性验证
+ pprof / 压测
```

### AI 岗位扩展（可选）

```text
OpenAI 兼容接口
+ Provider Adapter
+ SSE
+ Token Usage
```

不要把以下能力作为完成前置条件：

- Kubernetes
- WASM
- 控制面 / 数据面拆分
- 完整多租户与计费
- 大而全的管理后台

最终推荐描述：

> FlowGate 是一个基于 Go 的 API 网关与流量治理系统。系统围绕动态路由、负载均衡、健康检查、超时重试、熔断和配置热更新构建在线请求链路，并通过故障注入和压测验证上游异常、配置变更和重试风暴场景下的正确性与稳定性；在平台强化版本中使用 Redis + Lua 实现多实例共享限流并验证 Redis 故障回退策略。AI 场景下可进一步扩展 OpenAI 兼容协议与 SSE 流式代理。

# 17. 推荐开发顺序

## 第 1 阶段：把代理做正确

```text
Client → FlowGate → Mock Upstream
```

完成：

- 静态路由
- Request / Response 转发
- Header / Body 处理
- Request ID
- 基础超时
- 客户端取消传播

不要接数据库、Redis、K8s。

## 第 2 阶段：把配置做正确

加入：

- PostgreSQL
- 服务 / 实例 / 路由管理 API
- 配置版本
- 原子发布
- 不可变 RouteTable
- 基础健康检查

管理页面只做最小可用。

## 第 3 阶段：把服务治理做正确

加入：

- 多实例
- Round Robin / Weighted Round Robin
- 主动健康检查
- 实例摘除 / 恢复
- 分层超时
- 有限重试
- 幂等判断
- 指数退避 + Jitter
- 熔断 / Half-Open
- 基础过载保护
- Prometheus / Grafana

完成 V3 核心故障实验后再进入下一阶段。

## 第 4 阶段：Redis 与性能强化

只加入：

- 本地 Token Bucket
- Redis + Lua 滑动窗口
- API Key / 租户限流
- Redis 故障回退
- 多 FlowGate 实例验证
- pprof
- 压测报告

先完成对照实验，再决定是否继续优化。

## 第 5 阶段：按方向选做

AI 平台方向：

- OpenAI 兼容接口
- Provider
- SSE
- Token Usage
- OpenTelemetry（推荐）

平台基础设施方向：

- 更深入的过载保护
- 全局并发控制
- 配置分发

只有主线已经完整后，才考虑控制面 / 数据面、Kubernetes、WASM 等长期扩展。

# 18. README 应如何组织

README 首页建议顺序：

1. 现实问题
2. 一个错误的重试或配置更新实现如何导致故障
3. FlowGate 的解决方式
4. 系统架构图
5. 一次请求完整链路
6. 配置发布流程
7. 故障实验
8. 性能测试结果
9. 功能截图
10. 技术栈
11. 一键运行
12. 已知限制
13. 后续计划
14. 参考项目与设计来源

不要一开始只罗列：

```text
Go + PostgreSQL + Redis + Prometheus + Kubernetes
```

面试官更关心这些组件分别解决了什么问题。

README 应重点展示：

- 为什么不能无条件重试
- 为什么流式输出开始后不能透明切换模型
- 为什么配置需要不可变快照
- 为什么 Redis 故障需要 Fail-Open/Fail-Closed 策略
- 为什么网关过载时需要反压
- 如何证明没有 goroutine 泄漏
- 如何通过 pprof 和 Trace 定位瓶颈

---

# 19. 简历表达模板

不要把所有版本能力一次写进简历。只写自己真正完成并做过实验的部分。

### 完成 V3 后可写

> 使用 Go 实现 API 网关核心请求链路，支持动态路由、反向代理、多实例负载均衡与健康检查，并通过不可变 RouteTable + 原子替换保证配置热更新期间单次请求的配置一致性。

> 设计分层超时、幂等约束重试、指数退避与熔断机制，通过 Mock 上游复现连接超时、连续 5xx、非幂等 POST 和重试风暴，验证故障隔离与请求取消行为。

> 接入 Prometheus / Grafana 监控请求延迟、错误率、上游健康状态、重试次数和熔断状态，并结合压测与 pprof 分析请求链路瓶颈。

### 完成 V4 后再增加

> 基于 Redis + Lua 实现多网关实例共享的 API Key / 租户限流，并设计 Local Fallback 与 Fail-Closed 策略；通过 Redis 故障和突发流量实验对比本地 Token Bucket 与分布式滑动窗口的准确性和附加延迟。

### 完成 V5 后再增加

> 实现 OpenAI 兼容接口与 SSE 流式代理，通过 Context 传播客户端取消信号并回收下游连接与 goroutine，同时统计模型 Token Usage。

具体 QPS、P95/P99、内存和性能提升比例必须等真实测试后再填写。

# 20. 最终范围建议

推荐把项目完成度理解成：

```text
V0：地基
V1：完整 MVP
V2：服务治理基础
V3：核心完成线
V4：平台强化线
V5：AI 可选扩展
V6：长期路线，不作为当前目标
```

### 推荐实际目标

```text
必须做透：
V0 + V1 + V2 + V3

重点强化：
V4 中的
本地 Token Bucket
+ Redis + Lua 滑动窗口
+ Redis 故障回退
+ 多实例验证
+ pprof / 压测

AI 岗位可选：
OpenAI 兼容接口
+ Provider
+ SSE
+ Token Usage

当前不要追：
控制面 / 数据面拆分
Kubernetes
WASM
完整多租户计费
复杂管理后台
```

项目应始终遵守以下原则：

1. 先证明正确性，再追求高性能。
2. 先实现单实例，再引入分布式协调。
3. 先通过压测和 pprof 发现瓶颈，再优化。
4. 每个中间件必须对应一个明确问题。
5. 不为了技术栈强行加入消息队列、Redis、Kubernetes 或微服务拆分。
6. 一个机制做深并通过故障实验验证，优先于同时实现五个相似机制。
7. 不声称完全替代 Nginx、Envoy、Kong、APISIX。
8. 不编造 QPS、P99、并发规模和性能提升比例。
9. 网关项目的核心不是页面，而是请求链路的正确性、稳定性、故障隔离和可观测性。
10. 如果 V3 / V4 已经能稳定复现、解释并验证核心故障场景，项目已经达到“完整且有深度”的状态，不需要靠 V6 才证明价值。
