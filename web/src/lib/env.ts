function normalizeBaseUrl(value: string | undefined): string {
  const raw = (value ?? '/api/v1').trim()
  if (!raw) {
    return '/api/v1'
  }
  return raw.replace(/\/+$/, '')
}

export const env = {
  adminBaseUrl: normalizeBaseUrl(import.meta.env.VITE_ADMIN_BASE_URL),
  environmentLabel: (import.meta.env.VITE_ENV_LABEL ?? 'local').trim() || 'local',
  dataMode: import.meta.env.VITE_DATA_MODE === 'mock' ? 'mock' : 'live',
} as const

export type DataMode = (typeof env)['dataMode']
