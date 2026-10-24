/** gateway 内存里的实时节点状态，由 gateway 每 5 秒上报。 */
export type RuntimeNodeState = 'closed' | 'half_open' | 'open'

export interface RuntimeNode {
  service_id: number
  node_id: number
  address: string
  state: RuntimeNodeState
  healthy: boolean
  fails: number
  open_until?: string | null
  half_open_in_flight: number
}

export interface RuntimeMeta {
  instance: string
  published_at: string
  age_seconds: number
  stale: boolean
}

export interface RuntimeNodesResponse extends RuntimeMeta {
  nodes: RuntimeNode[]
}

export interface AIRuntimeStatus {
  enabled: boolean
  version: number
  loaded_at?: string | null
  provider_count: number
  model_count: number
  last_error?: string
  last_error_at?: string | null
}

export interface RuntimeAIResponse extends RuntimeMeta {
  ai: AIRuntimeStatus
}
