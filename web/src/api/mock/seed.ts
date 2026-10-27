import type { AIModel, AIProvider } from '../contracts/aiConfig'
import type { APIKey } from '../contracts/apiKey'
import type { Node } from '../contracts/node'
import type { Route } from '../contracts/route'
import type { Service } from '../contracts/service'
import type { Tenant } from '../contracts/tenant'
import type { Version, VersionItem } from '../contracts/version'
import { encodeSnapshot, hashConfig } from './snapshot'

/**
 * 演示用的内存数据库。
 *
 * 这些记录只在当前页面生命周期内存在，刷新即重置；它们不会发往 admin、
 * gateway 或任何远端服务。UI 在 Mock 模式下会持续显示演示横幅。
 */
export interface MockStore {
  tenants: Tenant[]
  apiKeys: APIKey[]
  services: Service[]
  nodes: Node[]
  routes: Route[]
  versions: Version[]
  versionItems: VersionItem[]
  providers: AIProvider[]
  models: AIModel[]
  aiConfigVersion: number
  next: {
    tenant: number
    apiKey: number
    service: number
    node: number
    route: number
    version: number
    versionItem: number
    provider: number
  }
}

const NOW = Date.now()
const DAY_MS = 86_400_000

function daysAgo(days: number, hour = 9, minute = 0): string {
  const date = new Date(NOW - days * DAY_MS)
  date.setUTCHours(hour, minute, 0, 0)
  return date.toISOString()
}

function daysFromNow(days: number, hour = 9): string {
  const date = new Date(NOW + days * DAY_MS)
  date.setUTCHours(hour, 0, 0, 0)
  return date.toISOString()
}

function buildTenants(): Tenant[] {
  const specs: Array<{
    name: string
    status: Tenant['status']
    qps: number
    tpm: number
    days: number
  }> = [
    { name: 'acme-platform', status: 'active', qps: 600, tpm: 2_000_000, days: 210 },
    { name: 'docs-preview', status: 'active', qps: 40, tpm: 80_000, days: 148 },
    { name: 'partner-sandbox', status: 'disabled', qps: 20, tpm: 50_000, days: 132 },
    { name: 'internal-data', status: 'active', qps: 250, tpm: 800_000, days: 121 },
    { name: 'mobile-gateway', status: 'active', qps: 1200, tpm: 4_000_000, days: 110 },
    { name: 'demo-lab', status: 'active', qps: 10, tpm: 20_000, days: 96 },
    { name: 'legacy-billing', status: 'disabled', qps: 30, tpm: 90_000, days: 88 },
    { name: 'search-team', status: 'active', qps: 150, tpm: 400_000, days: 72 },
    { name: 'observability', status: 'active', qps: 80, tpm: 200_000, days: 54 },
    { name: 'growth-experiments', status: 'active', qps: 60, tpm: 150_000, days: 41 },
    { name: 'compliance-archive', status: 'active', qps: 25, tpm: 75_000, days: 27 },
    { name: 'qa-regression', status: 'active', qps: 45, tpm: 100_000, days: 12 },
  ]

  return specs.map((spec, index) => ({
    id: index + 1,
    name: spec.name,
    status: spec.status,
    qps_limit: spec.qps,
    tpm_limit: spec.tpm,
    created_at: daysAgo(spec.days, 8, 30),
  }))
}

function buildAPIKeys(): APIKey[] {
  const rows: Array<{
    tenantId: number
    name: string
    status: APIKey['status']
    qps: number
    createdDays: number
    expiresInDays: number | null
  }> = []

  const acmeKeys = [
    ['prod-web', 'active', 300, 205, null],
    ['prod-mobile', 'active', 250, 203, null],
    ['prod-partner', 'active', 120, 190, 180],
    ['staging-web', 'active', 60, 160, 30],
    ['staging-mobile', 'active', 60, 158, 30],
    ['ci-regression', 'active', 100, 140, 14],
    ['load-test', 'disabled', 5000, 120, null],
    ['docs-site', 'active', 30, 100, null],
    ['internal-tools', 'active', 80, 84, 90],
    ['legacy-export', 'disabled', 20, 76, null],
    ['expired-canary', 'active', 10, 70, -4],
    ['data-warehouse', 'active', 40, 33, null],
  ] as const

  for (const [name, status, qps, createdDays, expiresInDays] of acmeKeys) {
    rows.push({
      tenantId: 1,
      name,
      status,
      qps,
      createdDays,
      expiresInDays,
    })
  }

  const otherKeys: Array<[number, string, APIKey['status'], number, number, number | null]> = [
    [2, 'docs-preview-main', 'active', 30, 146, 60],
    [2, 'docs-preview-pr-bot', 'active', 15, 120, 20],
    [3, 'partner-a', 'active', 20, 128, null],
    [3, 'partner-b', 'disabled', 20, 126, null],
    [4, 'internal-etl', 'active', 200, 118, null],
    [4, 'internal-bi', 'active', 100, 112, 120],
    [4, 'internal-ml', 'active', 80, 64, null],
    [5, 'mobile-ios-prod', 'active', 800, 108, null],
    [5, 'mobile-android-prod', 'active', 800, 106, null],
    [5, 'mobile-ios-beta', 'active', 200, 44, 15],
    [6, 'demo-readonly', 'active', 10, 92, null],
    [7, 'legacy-billing-main', 'disabled', 30, 86, null],
    [8, 'search-prod', 'active', 120, 66, null],
    [8, 'search-reindex', 'active', 60, 52, 45],
    [9, 'observability-read', 'active', 60, 50, null],
    [10, 'growth-api', 'active', 45, 38, null],
    [11, 'archive-writer', 'active', 20, 24, null],
    [12, 'qa-smoke', 'active', 45, 10, 7],
  ]

  for (const [tenantId, name, status, qps, createdDays, expiresInDays] of otherKeys) {
    rows.push({ tenantId, name, status, qps, createdDays, expiresInDays })
  }

  return rows.map((row, index) => ({
    id: index + 1,
    tenant_id: row.tenantId,
    name: row.name,
    status: row.status,
    qps_limit: row.qps,
    expires_at:
      row.expiresInDays === null ? null : daysFromNow(row.expiresInDays, 12),
    created_at: daysAgo(row.createdDays, 10, 15),
    last_used_at: null,
  }))
}

interface ServiceSpec {
  name: string
  enabled: boolean
  updatedDays: number
  overrides?: Partial<Service>
}

function buildServices(): Service[] {
  const specs: ServiceSpec[] = [
    {
      name: 'edge-payments',
      enabled: true,
      updatedDays: 2,
      overrides: {
        max_retries: 2,
        retry_on_status: '502,503,504',
        request_timeout_ms: 12000,
        response_header_timeout_ms: 6000,
        cb_failure_threshold: 8,
        max_concurrency: 200,
        queue_timeout_ms: 250,
        overload_strategy: 'wait',
      },
    },
    {
      name: 'catalog-api',
      enabled: true,
      updatedDays: 6,
      overrides: {
        max_retries: 1,
        retry_on_status: '503',
        connect_timeout_ms: 2000,
        request_timeout_ms: 8000,
        max_concurrency: 120,
        queue_timeout_ms: 100,
        overload_strategy: 'fail-fast',
      },
    },
    {
      name: 'search-indexer',
      enabled: true,
      updatedDays: 1,
      overrides: {
        max_retries: 2,
        request_timeout_ms: 15000,
        max_concurrency: 80,
        queue_timeout_ms: 200,
        overload_strategy: 'wait',
      },
    },
    {
      name: 'identity-service',
      enabled: true,
      updatedDays: 12,
      overrides: {
        max_retries: 0,
        request_timeout_ms: 6000,
        max_concurrency: 60,
      },
    },
    {
      name: 'notifications',
      enabled: true,
      updatedDays: 4,
      overrides: {
        max_retries: 3,
        retry_on_status: '429,503',
        retry_backoff_ms: 250,
        request_timeout_ms: 10000,
        max_concurrency: 30,
        queue_timeout_ms: 500,
        overload_strategy: 'drop',
      },
    },
    {
      name: 'media-transcoder',
      enabled: false,
      updatedDays: 21,
      overrides: {
        connect_timeout_ms: 3000,
        request_timeout_ms: 120000,
        max_retries: 0,
        max_concurrency: 8,
        queue_timeout_ms: 2000,
        overload_strategy: 'wait',
      },
    },
    {
      name: 'analytics-collector',
      enabled: true,
      updatedDays: 3,
      overrides: {
        max_retries: 1,
        request_timeout_ms: 5000,
        max_concurrency: 500,
      },
    },
    {
      name: 'legacy-soap-bridge',
      enabled: false,
      updatedDays: 95,
      overrides: {
        connect_timeout_ms: 5000,
        request_timeout_ms: 30000,
      },
    },
    {
      name: 'partner-onboarding',
      enabled: true,
      updatedDays: 9,
      overrides: {
        max_retries: 2,
        retry_on_status: '502',
        request_timeout_ms: 9000,
        max_concurrency: 40,
        queue_timeout_ms: 150,
        overload_strategy: 'wait',
      },
    },
    {
      name: 'invoice-renderer',
      enabled: true,
      updatedDays: 7,
      overrides: {
        max_retries: 1,
        request_timeout_ms: 20000,
        max_concurrency: 20,
        queue_timeout_ms: 300,
        overload_strategy: 'wait',
      },
    },
    {
      name: 'feature-flags',
      enabled: true,
      updatedDays: 1,
      overrides: {
        max_retries: 0,
        request_timeout_ms: 3000,
        max_concurrency: 300,
      },
    },
    {
      name: 'sandbox-upstream',
      enabled: true,
      updatedDays: 15,
      overrides: {
        request_timeout_ms: 15000,
        max_concurrency: 10,
      },
    },
  ]

  return specs.map((spec, index) => ({
    id: index + 1,
    name: spec.name,
    protocol: 'http',
    connect_timeout_ms: 3000,
    request_timeout_ms: 10000,
    enabled: spec.enabled,
    created_at: daysAgo(180 - index * 4, 8, 15),
    updated_at: daysAgo(spec.updatedDays, 10, 30),
    max_retries: 0,
    retry_on_status: '',
    retry_backoff_ms: 100,
    cb_failure_threshold: 5,
    cb_cooldown_ms: 10000,
    cb_half_open_limit: 1,
    response_header_timeout_ms: 5000,
    max_concurrency: 0,
    queue_timeout_ms: 0,
    overload_strategy: 'fail-fast',
    ...spec.overrides,
  }))
}

function buildNodes(): Node[] {
  const nodes: Node[] = []

  const add = (
    serviceId: number,
    host: string,
    weight: number,
    enabled: boolean,
    health: string,
    updatedDays: number,
  ) => {
    const id = nodes.length + 1
    nodes.push({
      id,
      service_id: serviceId,
      address: `https://${host}`,
      weight,
      enabled,
      health_status: health,
      created_at: daysAgo(150 - id, 9, 45),
      updated_at: daysAgo(updatedDays, 11, 5),
    })
  }

  const paymentHosts = [
    ['payments-01.upstream.example.com', 5, true, 'healthy', 2],
    ['payments-02.upstream.example.com', 5, true, 'healthy', 2],
    ['payments-03.upstream.example.com', 5, true, 'unknown', 3],
    ['payments-canary-01.upstream.example.com', 1, true, 'healthy', 1],
    ['payments-canary-02.upstream.example.com', 1, true, 'unknown', 1],
    ['payments-blue-01.upstream.example.com', 3, true, 'healthy', 8],
    ['payments-blue-02.upstream.example.com', 3, true, 'healthy', 8],
    ['payments-green-01.upstream.example.com', 3, true, 'healthy', 6],
    ['payments-green-02.upstream.example.com', 3, true, 'unhealthy', 6],
    ['payments-legacy-01.upstream.example.com', 1, false, 'unhealthy', 40],
    ['payments-dr-01.upstream.example.com', 2, true, 'unknown', 12],
    ['payments-dr-02.upstream.example.com', 2, true, 'unknown', 12],
  ] as const

  for (const [host, weight, enabled, health, updatedDays] of paymentHosts) {
    add(1, host, weight, enabled, health, updatedDays)
  }

  add(2, 'catalog-01.upstream.example.com', 4, true, 'healthy', 6)
  add(2, 'catalog-02.upstream.example.com', 4, true, 'healthy', 6)
  add(2, 'catalog-readonly.upstream.example.com', 1, false, 'unknown', 30)

  add(3, 'search-01.upstream.example.com', 3, true, 'healthy', 1)
  add(3, 'search-02.upstream.example.com', 3, true, 'healthy', 1)

  add(4, 'identity-01.upstream.example.com', 5, true, 'healthy', 12)
  add(4, 'identity-02.upstream.example.com', 5, true, 'unknown', 12)

  add(5, 'notify-01.upstream.example.com', 2, true, 'healthy', 4)
  add(5, 'notify-02.upstream.example.com', 2, true, 'unknown', 4)

  add(6, 'transcoder-01.upstream.example.com', 1, true, 'unhealthy', 21)

  add(7, 'events-01.upstream.example.com', 8, true, 'healthy', 3)
  add(7, 'events-02.upstream.example.com', 8, true, 'healthy', 3)
  add(7, 'events-03.upstream.example.com', 4, true, 'unknown', 3)

  add(8, 'soap-bridge-01.upstream.example.com', 1, true, 'unknown', 95)

  add(9, 'partner-01.upstream.example.com', 2, true, 'healthy', 9)
  add(9, 'partner-02.upstream.example.com', 2, true, 'unknown', 9)

  add(10, 'invoice-01.upstream.example.com', 1, true, 'healthy', 7)

  add(11, 'flags-01.upstream.example.com', 4, true, 'healthy', 1)
  add(11, 'flags-02.upstream.example.com', 4, true, 'healthy', 1)

  add(12, 'sandbox-01.upstream.example.com', 1, true, 'unknown', 15)
  add(12, 'sandbox-02.upstream.example.com', 1, true, 'unknown', 15)

  return nodes
}

function buildRoutes(): Route[] {
  const specs: Array<{
    serviceId: number
    name: string
    host: string
    path: string
    matchType?: 'prefix' | 'exact'
    methods: string
    enabled: boolean
    maxConcurrency: number
    updatedDays: number
  }> = [
    {
      serviceId: 1,
      name: 'payments-public',
      host: 'api.flowgate.example.com',
      path: '/v1/payments',
      methods: 'GET,POST',
      enabled: true,
      maxConcurrency: 200,
      updatedDays: 2,
    },
    {
      serviceId: 1,
      name: 'payments-refunds',
      host: 'api.flowgate.example.com',
      path: '/v1/refunds',
      methods: 'POST',
      enabled: true,
      maxConcurrency: 80,
      updatedDays: 5,
    },
    {
      serviceId: 2,
      name: 'catalog-read',
      host: 'api.flowgate.example.com',
      path: '/v1/catalog',
      methods: 'GET',
      enabled: true,
      maxConcurrency: 120,
      updatedDays: 6,
    },
    {
      serviceId: 2,
      name: 'catalog-admin',
      host: 'admin.flowgate.example.com',
      path: '/v1/catalog',
      methods: 'GET,POST,PUT,DELETE',
      enabled: true,
      maxConcurrency: 40,
      updatedDays: 6,
    },
    {
      serviceId: 3,
      name: 'search-query',
      host: 'api.flowgate.example.com',
      path: '/v1/search',
      methods: 'GET,POST',
      enabled: true,
      maxConcurrency: 100,
      updatedDays: 1,
    },
    {
      serviceId: 4,
      name: 'identity-oidc',
      host: 'auth.flowgate.example.com',
      path: '/v1/auth',
      methods: 'GET,POST',
      enabled: true,
      maxConcurrency: 60,
      updatedDays: 12,
    },
    {
      serviceId: 5,
      name: 'notification-webhooks',
      host: 'hooks.flowgate.example.com',
      path: '/v1/webhooks',
      methods: 'POST',
      enabled: true,
      maxConcurrency: 30,
      updatedDays: 4,
    },
    {
      serviceId: 6,
      name: 'media-transcode',
      host: 'media.flowgate.example.com',
      path: '/v1/media',
      methods: 'POST',
      enabled: false,
      maxConcurrency: 8,
      updatedDays: 21,
    },
    {
      serviceId: 7,
      name: 'analytics-ingest',
      host: 'events.flowgate.example.com',
      path: '/v1/events',
      methods: 'POST',
      enabled: true,
      maxConcurrency: 500,
      updatedDays: 3,
    },
    {
      serviceId: 8,
      name: 'legacy-bridge',
      host: 'legacy.flowgate.example.com',
      path: '/legacy',
      methods: 'POST',
      enabled: false,
      maxConcurrency: 0,
      updatedDays: 95,
    },
    {
      serviceId: 9,
      name: 'partner-onboarding',
      host: 'partners.flowgate.example.com',
      path: '/partner/v2',
      methods: 'GET,POST',
      enabled: true,
      maxConcurrency: 40,
      updatedDays: 9,
    },
    {
      serviceId: 10,
      name: 'invoice-pdf',
      host: 'api.flowgate.example.com',
      path: '/v1/invoices',
      methods: 'GET',
      enabled: true,
      maxConcurrency: 20,
      updatedDays: 7,
    },
    {
      serviceId: 11,
      name: 'feature-flags',
      host: 'flags.flowgate.example.com',
      path: '/v1/flags',
      methods: 'GET',
      enabled: true,
      maxConcurrency: 300,
      updatedDays: 1,
    },
    {
      serviceId: 12,
      name: 'sandbox-echo',
      host: 'sandbox.flowgate.example.com',
      path: '/sandbox',
      methods: 'GET,POST,PUT,DELETE',
      enabled: true,
      maxConcurrency: 10,
      updatedDays: 15,
    },
  ]

  return specs.map((spec, index) => ({
    id: index + 1,
    service_id: spec.serviceId,
    name: spec.name,
    host: spec.host,
    path_pattern: spec.path,
    path_match_type: spec.matchType ?? 'prefix',
    methods: spec.methods,
    enabled: spec.enabled,
    created_at: daysAgo(170 - index * 5, 9, 20),
    updated_at: daysAgo(spec.updatedDays, 11, 40),
    max_concurrency: spec.maxConcurrency,
  }))
}

function buildVersionHistory(
  services: Service[],
  nodes: Node[],
  routes: Route[],
): { versions: Version[]; items: VersionItem[] } {
  const versions: Version[] = []
  const items: VersionItem[] = []
  let itemId = 1

  for (let number = 1; number <= 12; number += 1) {
    const cutoff = number === 12 ? Number.POSITIVE_INFINITY : number
    const selectedServices = services.filter((service) => service.id <= cutoff)
    const selectedNodes = nodes.filter((node) => node.service_id <= cutoff)
    const selectedRoutes = routes.filter((route) => route.service_id <= cutoff)

    const versionId = number
    const timestamp = daysAgo(2 + (12 - number) * 5, 11, 0)

    versions.push({
      id: versionId,
      version: number,
      status: 'published',
      config_hash: hashConfig([selectedServices, selectedRoutes, selectedNodes]),
      published_at: timestamp,
      created_at: timestamp,
    })

    const resourceItems = [
      ...selectedServices
        .filter((service) => service.enabled)
        .map((service) => ({ type: 'service', id: service.id, snapshot: service })),
      ...selectedNodes
        .filter((node) => node.enabled)
        .map((node) => ({ type: 'node', id: node.id, snapshot: node })),
      ...selectedRoutes
        .filter((route) => route.enabled)
        .map((route) => ({ type: 'route', id: route.id, snapshot: route })),
    ]

    for (const resource of resourceItems) {
      items.push({
        id: itemId,
        version_id: versionId,
        resource_type: resource.type,
        resource_id: resource.id,
        resource_snapshot: encodeSnapshot(resource.snapshot),
      })
      itemId += 1
    }
  }

  return { versions, items }
}

function buildProviders(): AIProvider[] {
  return [
    {
      id: 1,
      name: 'deepseek-cloud',
      type: 'openai',
      endpoint: 'https://api.deepseek.com',
      upstream_path: '/v1/chat/completions',
      api_key_env: 'DEEPSEEK_API_KEY',
      stream_usage: true,
      enabled: true,
      created_at: daysAgo(160, 9, 0),
      updated_at: daysAgo(2, 14, 20),
    },
    {
      id: 2,
      name: 'azure-eu',
      type: 'openai',
      endpoint: 'https://eu.openai.azure.com',
      upstream_path: '/openai/deployments',
      api_key_env: 'AZURE_OPENAI_API_KEY',
      stream_usage: false,
      enabled: true,
      created_at: daysAgo(140, 10, 30),
      updated_at: daysAgo(9, 16, 10),
    },
    {
      id: 3,
      name: 'local-ollama',
      type: 'openai',
      endpoint: 'http://127.0.0.1:11434',
      upstream_path: '/v1/chat/completions',
      api_key_env: '',
      stream_usage: false,
      enabled: true,
      created_at: daysAgo(80, 8, 45),
      updated_at: daysAgo(4, 9, 5),
    },
    {
      id: 4,
      name: 'legacy-vendor',
      type: 'openai',
      endpoint: 'https://llm.legacy.example.com',
      upstream_path: '/v1/chat/completions',
      api_key_env: 'LEGACY_AI_KEY',
      stream_usage: false,
      enabled: false,
      created_at: daysAgo(220, 13, 0),
      updated_at: daysAgo(60, 13, 0),
    },
    {
      id: 5,
      name: 'qwen-proxy',
      type: 'openai',
      endpoint: 'https://dashscope.aliyuncs.com',
      upstream_path: '/compatible-mode/v1/chat/completions',
      api_key_env: 'DASHSCOPE_API_KEY',
      stream_usage: true,
      enabled: true,
      created_at: daysAgo(65, 11, 15),
      updated_at: daysAgo(3, 15, 40),
    },
  ]
}

function buildModels(): AIModel[] {
  const specs: Array<{
    name: string
    provider: string
    upstream: string
    enabled: boolean
    createdDays: number
    updatedDays: number
  }> = [
    { name: 'flowgate-chat', provider: 'deepseek-cloud', upstream: 'deepseek-chat', enabled: true, createdDays: 155, updatedDays: 2 },
    { name: 'flowgate-reasoner', provider: 'deepseek-cloud', upstream: 'deepseek-reasoner', enabled: true, createdDays: 150, updatedDays: 8 },
    { name: 'flowgate-mini', provider: 'deepseek-cloud', upstream: 'deepseek-chat', enabled: true, createdDays: 90, updatedDays: 21 },
    { name: 'azure-gpt-4o', provider: 'azure-eu', upstream: 'gpt-4o', enabled: true, createdDays: 138, updatedDays: 9 },
    { name: 'azure-gpt-4o-mini', provider: 'azure-eu', upstream: 'gpt-4o-mini', enabled: true, createdDays: 130, updatedDays: 11 },
    { name: 'azure-embedding', provider: 'azure-eu', upstream: 'text-embedding-3-large', enabled: true, createdDays: 125, updatedDays: 18 },
    { name: 'ollama-qwen', provider: 'local-ollama', upstream: 'qwen2.5:7b', enabled: true, createdDays: 78, updatedDays: 4 },
    { name: 'ollama-llama', provider: 'local-ollama', upstream: 'llama3.2:3b', enabled: false, createdDays: 76, updatedDays: 30 },
    { name: 'qwen-plus', provider: 'qwen-proxy', upstream: 'qwen-plus', enabled: true, createdDays: 62, updatedDays: 3 },
    { name: 'qwen-max', provider: 'qwen-proxy', upstream: 'qwen-max', enabled: true, createdDays: 60, updatedDays: 6 },
    { name: 'legacy-chat', provider: 'legacy-vendor', upstream: 'legacy-chat', enabled: true, createdDays: 200, updatedDays: 60 },
  ]

  return specs.map((spec) => ({
    name: spec.name,
    provider_name: spec.provider,
    upstream_model: spec.upstream,
    enabled: spec.enabled,
    created_at: daysAgo(spec.createdDays, 12, 0),
    updated_at: daysAgo(spec.updatedDays, 12, 0),
  }))
}

export function createInitialStore(): MockStore {
  const tenants = buildTenants()
  const apiKeys = buildAPIKeys()
  const services = buildServices()
  const nodes = buildNodes()
  const routes = buildRoutes()
  const { versions, items } = buildVersionHistory(services, nodes, routes)
  const providers = buildProviders()

  return {
    tenants,
    apiKeys,
    services,
    nodes,
    routes,
    versions,
    versionItems: items,
    providers,
    models: buildModels(),
    aiConfigVersion: 37,
    next: {
      tenant: tenants.length + 1,
      apiKey: apiKeys.length + 1,
      service: services.length + 1,
      node: nodes.length + 1,
      route: routes.length + 1,
      version: versions.length + 1,
      versionItem: items.length + 1,
      provider: providers.length + 1,
    },
  }
}
