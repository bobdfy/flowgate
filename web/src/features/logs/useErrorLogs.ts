import { useQuery } from '@tanstack/react-query'
import type { ErrorLogQuery } from '../../api/contracts/errorLog'
import { getErrorLogs } from '../../api/errorLogs'

export const errorLogKeys = {
  list: (query: ErrorLogQuery) => ['logs', 'errors', query] as const,
}

/**
 * 最近失败请求。
 * 503 表示 admin 没配 Redis；重试没有意义，因此 retry: false。
 * 错误是低频事件，10 秒轮询足够，也不会给 Redis 造成压力。
 */
export function useErrorLogs(query: ErrorLogQuery, enabled = true) {
  return useQuery({
    queryKey: errorLogKeys.list(query),
    queryFn: ({ signal }) => getErrorLogs(query, signal),
    enabled,
    refetchInterval: 10_000,
    retry: false,
  })
}
