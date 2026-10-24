import type { BadgeTone } from '../../components/ui/Badge'
import type { APIKey } from '../../api/contracts/apiKey'
import type { TenantStatus } from '../../api/contracts/tenant'

export interface StatusMeta {
  label: string
  tone: BadgeTone
}

export function tenantStatusMeta(status: TenantStatus): StatusMeta {
  return status === 'active'
    ? { label: '启用中', tone: 'success' }
    : { label: '已停用', tone: 'warning' }
}

export function isAPIKeyExpired(expiresAt: string | null): boolean {
  if (!expiresAt) {
    return false
  }
  const timestamp = Date.parse(expiresAt)
  return !Number.isNaN(timestamp) && timestamp <= Date.now()
}

export function apiKeyStatusMeta(
  key: Pick<APIKey, 'status' | 'expires_at'>,
): StatusMeta {
  if (key.status === 'disabled') {
    return { label: '已停用', tone: 'neutral' }
  }
  if (isAPIKeyExpired(key.expires_at)) {
    return { label: '已过期', tone: 'danger' }
  }
  return { label: '启用中', tone: 'success' }
}
