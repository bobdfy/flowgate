import { useQuery } from '@tanstack/react-query'
import { getRuntimeAI, getRuntimeNodes } from '../../api/runtime'

export const runtimeKeys = {
  nodes: ['runtime', 'nodes'] as const,
  ai: ['runtime', 'ai'] as const,
}

/**
 * gateway 上报的节点实时状态。
 * 503 表示 gateway 尚未上报（或 Redis 未配置），重试没有意义，因此 retry: false。
 */
export function useRuntimeNodes(enabled = true) {
  return useQuery({
    queryKey: runtimeKeys.nodes,
    queryFn: ({ signal }) => getRuntimeNodes(signal),
    enabled,
    refetchInterval: 5_000,
    retry: false,
  })
}

/** gateway 最近一次 AI 配置加载结果。 */
export function useRuntimeAI(enabled = true) {
  return useQuery({
    queryKey: runtimeKeys.ai,
    queryFn: ({ signal }) => getRuntimeAI(signal),
    enabled,
    refetchInterval: 5_000,
    retry: false,
  })
}
