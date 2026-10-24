import type { TPMWindowSettings } from './contracts/settings'
import { request } from './http'

export function getTPMWindowSettings(
  signal?: AbortSignal,
): Promise<TPMWindowSettings> {
  return request<TPMWindowSettings>('/settings/tpm-window', { signal })
}

export function updateTPMWindowSettings(
  windowSeconds: number,
): Promise<TPMWindowSettings> {
  return request<TPMWindowSettings>('/settings/tpm-window', {
    method: 'PUT',
    body: { window_seconds: windowSeconds },
  })
}
