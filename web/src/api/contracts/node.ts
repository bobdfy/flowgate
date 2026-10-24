/**
 * 上游节点契约。
 *
 * `health_status` 是持久化字段，不是运行时健康检查结果；
 * 管理 API 不提供节点实时健康查询。
 */
export interface Node {
  id: number
  service_id: number
  address: string
  weight: number
  enabled: boolean
  health_status: string
  created_at: string
  updated_at: string
}

export interface NodeWriteInput {
  address: string
  weight: number
  enabled: boolean
}
