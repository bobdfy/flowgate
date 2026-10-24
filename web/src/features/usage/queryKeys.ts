export type UsageRange = '24h' | '7d' | '30d'

const RANGE_MS: Record<UsageRange, number> = {
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

/** 把时间范围换算成后端要求的 RFC3339 窗口。 */
export function usageWindow(range: UsageRange): { from: string; to: string } {
  const to = new Date()
  return {
    from: new Date(to.getTime() - RANGE_MS[range]).toISOString(),
    to: to.toISOString(),
  }
}

export const usageKeys = {
  all: ['usage'] as const,
  list: (range: UsageRange, tenantId: number | null) =>
    ['usage', 'list', range, tenantId ?? 'all'] as const,
  summary: (range: UsageRange, tenantId: number | null, groupBy: string) =>
    ['usage', 'summary', groupBy, range, tenantId ?? 'all'] as const,
  tpm: (tenantId: number) => ['usage', 'tpm', tenantId] as const,
}
