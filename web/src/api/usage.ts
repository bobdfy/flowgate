import type {
  AIUsageGroupBy,
  AIUsageListResponse,
  AIUsageSummaryResponse,
  TenantTPMUsage,
  UsageQueryParams,
} from './contracts/usage'
import { request } from './http'

function toQuery(params: UsageQueryParams, groupBy?: AIUsageGroupBy): string {
  const search = new URLSearchParams()
  search.set('from', params.from)
  search.set('to', params.to)
  if (params.tenantId !== undefined) {
    search.set('tenant_id', String(params.tenantId))
  }
  if (params.keyId !== undefined) {
    search.set('key_id', String(params.keyId))
  }
  if (params.model) {
    search.set('model', params.model)
  }
  if (params.limit !== undefined) {
    search.set('limit', String(params.limit))
  }
  if (params.offset !== undefined) {
    search.set('offset', String(params.offset))
  }
  if (groupBy) {
    search.set('group_by', groupBy)
  }
  return search.toString()
}

export function listUsage(
  params: UsageQueryParams,
  signal?: AbortSignal,
): Promise<AIUsageListResponse> {
  return request<AIUsageListResponse>(`/usage?${toQuery(params)}`, { signal })
}

export function getUsageSummary(
  params: UsageQueryParams,
  groupBy: AIUsageGroupBy,
  signal?: AbortSignal,
): Promise<AIUsageSummaryResponse> {
  return request<AIUsageSummaryResponse>(
    `/usage/summary?${toQuery(params, groupBy)}`,
    { signal },
  )
}

export function getTenantTPMUsage(
  tenantId: number,
  signal?: AbortSignal,
): Promise<TenantTPMUsage> {
  return request<TenantTPMUsage>(`/tenants/${tenantId}/tpm-usage`, { signal })
}
