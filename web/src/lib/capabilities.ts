import { env } from './env'

export type CapabilityState = 'available' | 'unavailable' | 'pending'

export interface Capability {
  key: string
  label: string
  state: CapabilityState
  detail: string
}

export const isMockMode = env.dataMode === 'mock'

const liveCapabilities: Capability[] = [
  {
    key: 'admin',
    label: '管理面配置 API',
    state: 'available',
    detail:
      '43 个 /api/v1 路由，覆盖服务、节点、路由、发布、租户、Key、AI 配置、用量查询、错误窗口与运行状态。',
  },
  {
    key: 'metrics',
    label: '运行指标',
    state: 'available',
    detail:
      '以只读外链在新标签打开 gateway 的 Prometheus 端点（/metrics，文本格式）；控制台不解析指标，也不做聚合查询。',
  },
  {
    key: 'logs',
    label: '错误请求',
    state: 'available',
    detail:
      'gateway 把失败请求（5xx / 401 / 403 / 429）写入 Redis 窗口（最多 500 条），经 /api/v1/logs/errors 查询；全量 access log 仍只输出到 stdout。',
  },
  {
    key: 'node-health',
    label: '节点实时健康',
    state: 'available',
    detail:
      'gateway 每 5 秒上报节点健康与熔断快照（/api/v1/runtime/nodes），服务详情按 node_id 实时展示。',
  },
  {
    key: 'usage',
    label: 'Token 用量',
    state: 'available',
    detail:
      '每个 API Key 的 24h token / 请求数直接展示在租户详情的 Key 列表中（/api/v1/usage/summary），当窗 TPM 用量来自 /tenants/{id}/tpm-usage。',
  },
  {
    key: 'ai-runtime',
    label: 'AI 热更新状态',
    state: 'available',
    detail:
      'gateway 上报最近一次 AI 配置加载结果（版本、时间、模型数、错误），见 /api/v1/runtime/ai。',
  },
]

/**
 * 演示模式不会访问真实管理面，因此不能继续声称“管理面配置 API 已接入”。
 * 演示模式下不展示用量列、TPM 窗口设置与指标外链，而不是伪造数据。
 */
const mockCapabilities: Capability[] = liveCapabilities.map((capability) => {
  if (capability.key !== 'admin') {
    return capability
  }
  return {
    ...capability,
    label: '管理面配置 API（演示）',
    detail:
      '由浏览器内 Mock 适配层模拟 35 个 /api/v1 路由，数据不会写入真实 admin 或数据库。',
  }
})

export const capabilities: Capability[] = isMockMode
  ? mockCapabilities
  : liveCapabilities
