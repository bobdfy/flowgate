import type { ErrorLogQuery, ErrorLogResponse } from './contracts/errorLog'
import { request } from './http'

function toQuery(params: ErrorLogQuery): string {
  const search = new URLSearchParams()
  if (params.kind) search.set('kind', params.kind)
  if (params.status !== undefined) search.set('status', String(params.status))
  if (params.route) search.set('route', params.route)
  if (params.request_id) search.set('request_id', params.request_id)
  if (params.tenant_id !== undefined) {
    search.set('tenant_id', String(params.tenant_id))
  }
  if (params.since) search.set('since', params.since)
  if (params.limit !== undefined) search.set('limit', String(params.limit))
  const query = search.toString()
  return query ? `?${query}` : ''
}

/** 最近失败请求；数据来自 gateway 写入的 Redis 窗口，不是完整日志。 */
export function getErrorLogs(
  params: ErrorLogQuery,
  signal?: AbortSignal,
): Promise<ErrorLogResponse> {
  return request<ErrorLogResponse>(`/logs/errors${toQuery(params)}`, { signal })
}
