export interface AIUsageRecord {
  id: number
  tenant_id: number
  key_id: number
  model: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated: boolean
  status_code: number
  streaming: boolean
  duration_ms: number
  request_id: string
  created_at: string
}

export interface AIUsageListResponse {
  items: AIUsageRecord[]
  from: string
  to: string
  limit: number
  offset: number
}

export interface AIUsageSummaryItem {
  group: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated_count: number
}

export type AIUsageGroupBy = 'model' | 'key' | 'tenant' | 'day'

export interface AIUsageSummaryResponse {
  group_by: AIUsageGroupBy
  from: string
  to: string
  items: AIUsageSummaryItem[]
}

export interface UsageQueryParams {
  from: string
  to: string
  tenantId?: number
  keyId?: number
  model?: string
  limit?: number
  offset?: number
}

export interface TenantTPMUsage {
  tenant_id: number
  window_seconds: number
  window_start: string
  window_end: string
  resets_in_seconds: number
  used: number
  limit: number
  remaining: number | null
  unlimited: boolean
}
