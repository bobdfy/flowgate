import { useQuery } from '@tanstack/react-query'
import {
  getTenantTPMUsage,
  getUsageSummary,
  listUsage,
} from '../../api/usage'
import type { AIUsageGroupBy } from '../../api/contracts/usage'
import { usageKeys, usageWindow, type UsageRange } from './queryKeys'

const LIST_LIMIT = 50

export function useUsageSummary(
  range: UsageRange,
  tenantId: number | null,
  groupBy: AIUsageGroupBy,
  enabled = true,
) {
  return useQuery({
    queryKey: usageKeys.summary(range, tenantId, groupBy),
    queryFn: ({ signal }) => {
      const window = usageWindow(range)
      return getUsageSummary(
        {
          ...window,
          tenantId: tenantId ?? undefined,
        },
        groupBy,
        signal,
      )
    },
    enabled,
    refetchInterval: 30_000,
  })
}

export function useUsageList(
  range: UsageRange,
  tenantId: number | null,
  enabled = true,
) {
  return useQuery({
    queryKey: usageKeys.list(range, tenantId),
    queryFn: ({ signal }) => {
      const window = usageWindow(range)
      return listUsage(
        {
          ...window,
          tenantId: tenantId ?? undefined,
          limit: LIST_LIMIT,
        },
        signal,
      )
    },
    enabled,
    refetchInterval: 30_000,
  })
}

/** 租户当窗 TPM 用量；窗口结束前 20 秒自动刷新。 */
export function useTenantTPMUsage(tenantId: number | null) {
  return useQuery({
    queryKey: usageKeys.tpm(tenantId ?? 0),
    queryFn: ({ signal }) => {
      if (tenantId === null) {
        throw new Error('缺少租户 ID')
      }
      return getTenantTPMUsage(tenantId, signal)
    },
    enabled: tenantId !== null,
    refetchInterval: 20_000,
  })
}
