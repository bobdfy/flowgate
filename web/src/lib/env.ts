function normalizeBaseUrl(value: string | undefined): string {
  const raw = (value ?? '/api/v1').trim()
  if (!raw) {
    return '/api/v1'
  }
  return raw.replace(/\/+$/, '')
}

function normalizeUrl(value: string | undefined, fallback: string): string {
  const raw = (value ?? fallback).trim()
  return raw || fallback
}

export const env = {
  adminBaseUrl: normalizeBaseUrl(import.meta.env.VITE_ADMIN_BASE_URL),
  /**
   * gateway 的 Prometheus 指标端点。控制台只做外链（新标签打开），
   * 不解析指标、也不做聚合查询；部署时可用 VITE_METRICS_URL 指向
   * gateway 对控制台用户可达的地址。
   */
  metricsUrl: normalizeUrl(
    import.meta.env.VITE_METRICS_URL,
    'http://127.0.0.1:9090/metrics',
  ),
  environmentLabel: (import.meta.env.VITE_ENV_LABEL ?? 'local').trim() || 'local',
  dataMode: import.meta.env.VITE_DATA_MODE === 'mock' ? 'mock' : 'live',
} as const

export type DataMode = (typeof env)['dataMode']
