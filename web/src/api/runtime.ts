import type {
  RuntimeAIResponse,
  RuntimeNodesResponse,
} from './contracts/runtime'
import { request } from './http'

export function getRuntimeNodes(
  signal?: AbortSignal,
): Promise<RuntimeNodesResponse> {
  return request<RuntimeNodesResponse>('/runtime/nodes', { signal })
}

export function getRuntimeAI(
  signal?: AbortSignal,
): Promise<RuntimeAIResponse> {
  return request<RuntimeAIResponse>('/runtime/ai', { signal })
}
