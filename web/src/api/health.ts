import type { HealthResponse } from './contracts/common'
import { request } from './http'

export function getHealth(signal?: AbortSignal): Promise<HealthResponse> {
  return request<HealthResponse>('/health', {
    signal,
    timeoutMs: 8_000,
  })
}
