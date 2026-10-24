import type {
  Version,
  VersionDetail,
} from './contracts/version'
import { request } from './http'

export function listVersions(signal?: AbortSignal): Promise<Version[]> {
  return request<Version[]>('/versions', { signal })
}

export function getVersion(
  id: number,
  signal?: AbortSignal,
): Promise<VersionDetail> {
  return request<VersionDetail>(`/versions/${id}`, { signal })
}

export function publishConfig(): Promise<Version> {
  return request<Version>('/publish', { method: 'POST' })
}

export function rollbackVersion(id: number): Promise<Version> {
  return request<Version>(`/versions/${id}/rollback`, { method: 'POST' })
}
