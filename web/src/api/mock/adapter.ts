import type { APIKey, CreateAPIKeyResponse } from '../contracts/apiKey'
import type { Node } from '../contracts/node'
import type { Route } from '../contracts/route'
import type { Service } from '../contracts/service'
import type { Tenant } from '../contracts/tenant'
import type { Version, VersionItem } from '../contracts/version'
import type { AIModel, AIProvider } from '../contracts/aiConfig'
import { ApiError, type ApiErrorKind } from '../errors'
import type { RequestOptions } from '../http'
import { createInitialStore, type MockStore } from './seed'
import { clone, encodeSnapshot, hashConfig } from './snapshot'

/**
 * 浏览器内 Mock 适配层。
 *
 * 仅当 VITE_DATA_MODE=mock 时由 http.ts 动态加载；命中时不会发起任何网络请求。
 * 行为尽量对齐 internal/api 与 internal/store/postgres：
 * 状态码、错误文案、PUT 全量覆盖、发布校验和 copy-forward 回滚都按后端实现。
 */

const store: MockStore = createInitialStore()

const PROVIDER_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/
const MODEL_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$/
const ENV_NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/

class MockHttpError extends Error {
  readonly status: number
  readonly kind: ApiErrorKind

  constructor(status: number, message: string) {
    super(message)
    this.name = 'MockHttpError'
    this.status = status
    this.kind = statusToKind(status)
  }
}

function statusToKind(status: number): ApiErrorKind {
  if (status === 401) return 'unauthorized'
  if (status === 403) return 'forbidden'
  if (status === 404) return 'not-found'
  if (status === 409) return 'conflict'
  if (status === 400 || status === 422) return 'validation'
  if (status >= 500) return 'server'
  if (status >= 400) return 'client'
  return 'unknown'
}

function fail(status: number, message: string): never {
  throw new MockHttpError(status, message)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function bodyRecord(body: unknown): Record<string, unknown> {
  if (!isRecord(body)) {
    fail(400, 'invalid request body')
  }
  return body
}

function stringField(
  body: Record<string, unknown>,
  key: string,
  fallback = '',
): string {
  const value = body[key]
  if (value === undefined || value === null) return fallback
  if (typeof value !== 'string') fail(400, 'invalid request body')
  return value
}

function numberField(
  body: Record<string, unknown>,
  key: string,
  fallback = 0,
): number {
  const value = body[key]
  if (value === undefined || value === null) return fallback
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    fail(400, 'invalid request body')
  }
  return value
}

function booleanField(
  body: Record<string, unknown>,
  key: string,
  fallback: boolean,
): boolean {
  const value = body[key]
  if (value === undefined || value === null) return fallback
  if (typeof value !== 'boolean') fail(400, 'invalid request body')
  return value
}

function nowISO(): string {
  return new Date().toISOString()
}

function parseId(raw: string | undefined): number {
  if (!raw || !/^\d+$/.test(raw)) {
    fail(400, 'invalid id')
  }
  const value = Number(raw)
  if (!Number.isSafeInteger(value) || value <= 0) {
    fail(400, 'invalid id')
  }
  return value
}

function decodeSegments(pathname: string): string[] {
  return pathname
    .split('/')
    .filter(Boolean)
    .map((segment) => {
      try {
        return decodeURIComponent(segment)
      } catch {
        return segment
      }
    })
}

function randomAPIKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32))
  const hex = Array.from(bytes, (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
  return `sk_${hex}`
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) {
    return Promise.reject(new ApiError('请求已取消', 'canceled'))
  }

  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer)
      reject(new ApiError('请求已取消', 'canceled'))
    }
    const timer = window.setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

// ---- 校验（与 internal/api/validate.go、aiconfig.go 对齐）----

function applyTenantInput(body: Record<string, unknown>): Omit<Tenant, 'id' | 'created_at'> {
  const name = stringField(body, 'name')
  if (name === '') fail(400, 'name is required')

  const status = stringField(body, 'status') || 'active'
  if (status !== 'active' && status !== 'disabled') {
    fail(400, 'status must be active or disabled')
  }

  let qps = numberField(body, 'qps_limit')
  if (qps <= 0) qps = 100
  let tpm = numberField(body, 'tpm_limit')
  if (tpm <= 0) tpm = 100000

  return {
    name,
    status,
    qps_limit: qps,
    tpm_limit: tpm,
  }
}

function applyServiceInput(
  body: Record<string, unknown>,
): Omit<Service, 'id' | 'created_at' | 'updated_at'> {
  const name = stringField(body, 'name')
  if (name === '') fail(400, 'name is required')

  let retryBackoff = numberField(body, 'retry_backoff_ms')
  if (retryBackoff <= 0) retryBackoff = 100
  let cbThreshold = numberField(body, 'cb_failure_threshold')
  if (cbThreshold <= 0) cbThreshold = 5
  let cbCooldown = numberField(body, 'cb_cooldown_ms')
  if (cbCooldown <= 0) cbCooldown = 10000
  let cbHalfOpen = numberField(body, 'cb_half_open_limit')
  if (cbHalfOpen <= 0) cbHalfOpen = 1
  let responseHeaderTimeout = numberField(body, 'response_header_timeout_ms')
  if (responseHeaderTimeout <= 0) responseHeaderTimeout = 5000

  return {
    name,
    protocol: stringField(body, 'protocol'),
    connect_timeout_ms: numberField(body, 'connect_timeout_ms'),
    request_timeout_ms: numberField(body, 'request_timeout_ms'),
    enabled: booleanField(body, 'enabled', false),
    max_retries: numberField(body, 'max_retries'),
    retry_on_status: stringField(body, 'retry_on_status'),
    retry_backoff_ms: retryBackoff,
    cb_failure_threshold: cbThreshold,
    cb_cooldown_ms: cbCooldown,
    cb_half_open_limit: cbHalfOpen,
    response_header_timeout_ms: responseHeaderTimeout,
    max_concurrency: numberField(body, 'max_concurrency'),
    queue_timeout_ms: numberField(body, 'queue_timeout_ms'),
    overload_strategy: stringField(body, 'overload_strategy'),
  }
}

function isPrivateHost(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '')
  if (host === 'localhost' || host === '::1' || host.endsWith('.localhost')) {
    return true
  }

  const octets = host.split('.')
  if (octets.length === 4 && octets.every((part) => /^\d+$/.test(part))) {
    const values = octets.map(Number)
    const [a, b] = values
    if (values.some((value) => value > 255)) return false
    if (a === 10 || a === 127 || a === 0) return true
    if (a === 169 && b === 254) return true
    if (a === 172 && b >= 16 && b <= 31) return true
    if (a === 192 && b === 168) return true
    if (a === 100 && b >= 64 && b <= 127) return true
    if (a >= 224) return true
  }

  return false
}

function applyNodeInput(
  body: Record<string, unknown>,
): { address: string; weight: number; enabled: boolean } {
  const address = stringField(body, 'address')
  if (address === '') fail(400, 'address is required')

  let parsed: URL
  try {
    parsed = new URL(address)
  } catch {
    fail(400, 'address must be a full URL, e.g. http://localhost:8091')
  }
  if (!parsed.host) {
    fail(400, 'address must be a full URL, e.g. http://localhost:8091')
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    fail(400, 'address scheme must be http or https')
  }
  if (isPrivateHost(parsed.hostname)) {
    fail(400, 'address points to a private/loopback host')
  }

  let weight = numberField(body, 'weight')
  if (weight <= 0) weight = 1

  return {
    address,
    weight,
    enabled: booleanField(body, 'enabled', false),
  }
}

function normalizeMethods(methods: string): string {
  if (methods.trim() === '') return ''
  return methods
    .split(',')
    .map((part) => part.trim().toUpperCase())
    .filter(Boolean)
    .sort()
    .join(',')
}

function findRouteConflict(
  route: Pick<Route, 'host' | 'path_pattern' | 'path_match_type' | 'methods'>,
  excludeId: number,
): Route | undefined {
  const wanted = normalizeMethods(route.methods)
  return store.routes.find((candidate) => {
    if (candidate.id === excludeId) return false
    if (candidate.host !== route.host) return false
    if (candidate.path_pattern !== route.path_pattern) return false
    if (candidate.path_match_type !== route.path_match_type) return false
    const existing = normalizeMethods(candidate.methods)
    return existing === wanted || existing === '' || wanted === ''
  })
}

function applyProviderInput(body: Record<string, unknown>): Omit<
  AIProvider,
  'id' | 'created_at' | 'updated_at'
> {
  const name = stringField(body, 'name').trim()
  if (name === '') fail(400, 'name is required')
  if (!PROVIDER_NAME_RE.test(name)) {
    fail(400, 'name may only contain letters, digits, dot, dash and underscore')
  }

  const type = stringField(body, 'type').trim() || 'openai'
  if (type !== 'openai') {
    fail(
      400,
      'type must be openai (only OpenAI-compatible upstreams are supported)',
    )
  }

  const endpoint = stringField(body, 'endpoint').trim().replace(/\/+$/, '')
  if (endpoint === '') fail(400, 'endpoint is required')
  try {
    const parsed = new URL(endpoint)
    if (
      !parsed.host ||
      (parsed.protocol !== 'http:' && parsed.protocol !== 'https:')
    ) {
      fail(400, 'endpoint must be a full http/https URL')
    }
  } catch (error) {
    if (error instanceof MockHttpError) throw error
    fail(400, 'endpoint must be a full http/https URL')
  }

  const upstreamPath = stringField(body, 'upstream_path').trim()
  if (upstreamPath !== '' && !upstreamPath.startsWith('/')) {
    fail(400, 'upstream_path must start with /')
  }

  const apiKeyEnv = stringField(body, 'api_key_env').trim()
  if (apiKeyEnv !== '' && !ENV_NAME_RE.test(apiKeyEnv)) {
    fail(400, 'api_key_env must be a valid environment variable name')
  }

  return {
    name,
    type,
    endpoint,
    upstream_path: upstreamPath,
    api_key_env: apiKeyEnv,
    stream_usage: booleanField(body, 'stream_usage', false),
    enabled: booleanField(body, 'enabled', true),
  }
}

function applyModelInput(body: Record<string, unknown>): {
  name: string
  provider_name: string
  upstream_model: string
  enabled: boolean
} {
  const name = stringField(body, 'name').trim()
  if (name === '') fail(400, 'name is required')
  if (!MODEL_NAME_RE.test(name)) {
    fail(
      400,
      'name may only contain letters, digits, dot, colon, dash and underscore',
    )
  }

  const providerName = stringField(body, 'provider_name').trim()
  if (providerName === '') fail(400, 'provider_name is required')

  return {
    name,
    provider_name: providerName,
    upstream_model: stringField(body, 'upstream_model').trim(),
    enabled: booleanField(body, 'enabled', true),
  }
}

function ensureProvider(providerName: string): void {
  if (!store.providers.some((provider) => provider.name === providerName)) {
    fail(400, `invalid ai config: provider "${providerName}" does not exist`)
  }
}

// ---- 版本快照 ----

function validatePublish(): void {
  const serviceById = new Map(
    store.services.map((service) => [service.id, service]),
  )
  const enabledNodeCount = new Map<number, number>()
  for (const node of store.nodes) {
    if (!node.enabled) continue
    enabledNodeCount.set(
      node.service_id,
      (enabledNodeCount.get(node.service_id) ?? 0) + 1,
    )
  }

  const seen = new Map<string, Route>()
  const duplicateErrors: string[] = []
  for (const route of store.routes) {
    const key = [
      route.host,
      route.path_pattern,
      route.path_match_type,
      normalizeMethods(route.methods),
    ].join('\u0000')
    const previous = seen.get(key)
    if (previous) {
      duplicateErrors.push(
        `路由匹配规则重复: "${previous.name}" 与 "${route.name}" 的 host="${route.host}" path="${route.path_pattern}" match="${route.path_match_type}" methods="${route.methods}" 完全相同(只能保留一条，建议用 PUT 修改)`,
      )
    } else {
      seen.set(key, route)
    }
  }
  if (duplicateErrors.length > 0) {
    fail(400, duplicateErrors.join('\n'))
  }

  for (const route of store.routes) {
    if (!route.enabled) continue
    const service = serviceById.get(route.service_id)
    if (!service) {
      fail(400, `路由 "${route.name}" 引用的服务 ${route.service_id} 不存在`)
    }
    if (!service.enabled) {
      fail(400, `路由 "${route.name}" 引用的服务 "${service.name}" 已停用`)
    }
    if ((enabledNodeCount.get(route.service_id) ?? 0) === 0) {
      fail(400, `服务 "${service.name}" 没有可用实例`)
    }
  }
}

function buildSnapshotItems(): VersionItem[] {
  const resources: Array<{
    type: string
    id: number
    snapshot: Service | Node | Route
  }> = []

  for (const service of store.services) {
    if (service.enabled) {
      resources.push({ type: 'service', id: service.id, snapshot: service })
    }
  }
  for (const node of store.nodes) {
    if (node.enabled) {
      resources.push({ type: 'node', id: node.id, snapshot: node })
    }
  }
  for (const route of store.routes) {
    if (route.enabled) {
      resources.push({ type: 'route', id: route.id, snapshot: route })
    }
  }

  return resources.map((resource) => {
    const item: VersionItem = {
      id: store.next.versionItem,
      version_id: 0,
      resource_type: resource.type,
      resource_id: resource.id,
      resource_snapshot: encodeSnapshot(resource.snapshot),
    }
    store.next.versionItem += 1
    return item
  })
}

function currentConfigHash(): string {
  return hashConfig([store.services, store.routes, store.nodes])
}

function createVersion(
  items: VersionItem[],
  configHash: string,
  timestamp: string,
): Version {
  const version: Version = {
    id: store.next.version,
    version: store.next.version,
    status: 'published',
    config_hash: configHash,
    published_at: timestamp,
    created_at: timestamp,
  }
  store.next.version += 1
  store.versions.push(version)
  for (const item of items) {
    item.version_id = version.id
    if (!item.id) item.id = store.next.versionItem++
    store.versionItems.push(item)
  }
  return version
}

// ---- 资源处理 ----

function handleTenants(
  method: string,
  segments: string[],
  body: unknown,
): unknown {
  if (segments.length === 1) {
    if (method === 'GET') return store.tenants
    if (method === 'POST') {
      const input = applyTenantInput(bodyRecord(body))
      if (store.tenants.some((tenant) => tenant.name === input.name)) {
        // 后端没有把 tenants.name 的唯一约束映射成 409。
        fail(500, 'internal server error')
      }
      const tenant: Tenant = {
        ...input,
        id: store.next.tenant,
        created_at: nowISO(),
      }
      store.next.tenant += 1
      store.tenants.push(tenant)
      return tenant
    }
    fail(405, 'method not allowed')
  }

  const tenantId = parseId(segments[1])

  if (segments.length === 2) {
    const index = store.tenants.findIndex((tenant) => tenant.id === tenantId)
    if (method === 'GET') {
      if (index === -1) fail(404, 'tenant not found')
      return store.tenants[index]
    }
    if (method === 'PUT') {
      if (index === -1) fail(500, 'internal server error')
      const input = applyTenantInput(bodyRecord(body))
      if (
        store.tenants.some(
          (tenant) => tenant.id !== tenantId && tenant.name === input.name,
        )
      ) {
        fail(500, 'internal server error')
      }
      const updated: Tenant = {
        ...store.tenants[index],
        ...input,
        id: tenantId,
      }
      store.tenants[index] = updated
      return updated
    }
    if (method === 'DELETE') {
      if (index === -1) fail(500, 'internal server error')
      store.tenants.splice(index, 1)
      store.apiKeys = store.apiKeys.filter((key) => key.tenant_id !== tenantId)
      return undefined
    }
    fail(405, 'method not allowed')
  }

  if (segments.length === 3 && segments[2] === 'keys') {
    if (method === 'GET') {
      return store.apiKeys.filter((key) => key.tenant_id === tenantId)
    }
    if (method === 'POST') {
      if (!store.tenants.some((tenant) => tenant.id === tenantId)) {
        fail(500, 'internal server error')
      }
      const input = bodyRecord(body)
      const name = stringField(input, 'name')
      if (name === '') fail(400, 'name is required')
      const status = stringField(input, 'status') || 'active'
      let qps = numberField(input, 'qps_limit')
      if (qps <= 0) qps = 100
      const expiresRaw = input.expires_at
      if (
        expiresRaw !== undefined &&
        expiresRaw !== null &&
        typeof expiresRaw !== 'string'
      ) {
        fail(400, 'invalid request body')
      }

      const apiKey: APIKey = {
        id: store.next.apiKey,
        tenant_id: tenantId,
        name,
        status: status as APIKey['status'],
        qps_limit: qps,
        expires_at: (expiresRaw as string | null | undefined) ?? null,
        created_at: nowISO(),
        last_used_at: null,
      }
      store.next.apiKey += 1
      store.apiKeys.push(apiKey)

      const response: CreateAPIKeyResponse = {
        id: apiKey.id,
        tenant_id: apiKey.tenant_id,
        name: apiKey.name,
        status: apiKey.status,
        expires_at: apiKey.expires_at,
        created_at: apiKey.created_at,
        key: randomAPIKey(),
      }
      return response
    }
    fail(405, 'method not allowed')
  }

  fail(404, 'not found')
}

function handleAPIKeys(
  method: string,
  segments: string[],
  body: unknown,
): unknown {
  const keyId = parseId(segments[1])
  const index = store.apiKeys.findIndex((key) => key.id === keyId)

  if (method === 'PUT') {
    if (index === -1) fail(500, 'internal server error')
    const input = bodyRecord(body)
    let status = stringField(input, 'status') || 'active'
    if (status !== 'active' && status !== 'disabled') {
      fail(400, 'status must be active or disabled')
    }
    let qps = numberField(input, 'qps_limit')
    if (qps <= 0) qps = 100
    const expiresRaw = input.expires_at
    if (
      expiresRaw !== undefined &&
      expiresRaw !== null &&
      typeof expiresRaw !== 'string'
    ) {
      fail(400, 'invalid request body')
    }

    const existing = store.apiKeys[index]
    const updated: APIKey = {
      ...existing,
      name: stringField(input, 'name'),
      status: status as APIKey['status'],
      qps_limit: qps,
      expires_at: (expiresRaw as string | null | undefined) ?? null,
    }
    store.apiKeys[index] = updated
    return updated
  }

  if (method === 'DELETE') {
    if (index === -1) fail(500, 'internal server error')
    store.apiKeys.splice(index, 1)
    return undefined
  }

  fail(405, 'method not allowed')
}

function handleServices(
  method: string,
  segments: string[],
  body: unknown,
): unknown {
  if (segments.length === 1) {
    if (method === 'GET') return store.services
    if (method === 'POST') {
      const input = applyServiceInput(bodyRecord(body))
      if (store.services.some((service) => service.name === input.name)) {
        fail(500, 'internal server error')
      }
      const service: Service = {
        ...input,
        id: store.next.service,
        created_at: nowISO(),
        updated_at: nowISO(),
      }
      store.next.service += 1
      store.services.push(service)
      return service
    }
    fail(405, 'method not allowed')
  }

  const serviceId = parseId(segments[1])

  if (segments.length === 3 && segments[2] === 'nodes') {
    if (method === 'GET') {
      return store.nodes.filter((node) => node.service_id === serviceId)
    }
    if (method === 'POST') {
      if (!store.services.some((service) => service.id === serviceId)) {
        fail(500, 'internal node error')
      }
      const input = applyNodeInput(bodyRecord(body))
      const node: Node = {
        ...input,
        id: store.next.node,
        service_id: serviceId,
        health_status: 'unknown',
        created_at: nowISO(),
        updated_at: nowISO(),
      }
      store.next.node += 1
      store.nodes.push(node)
      return node
    }
    fail(405, 'method not allowed')
  }

  if (segments.length !== 2) fail(404, 'not found')

  const index = store.services.findIndex((service) => service.id === serviceId)

  if (method === 'GET') {
    if (index === -1) fail(404, 'service not found')
    return store.services[index]
  }

  if (method === 'PUT') {
    if (index === -1) fail(500, 'internal server error')
    const input = applyServiceInput(bodyRecord(body))
    if (
      store.services.some(
        (service) => service.id !== serviceId && service.name === input.name,
      )
    ) {
      fail(500, 'internal server error')
    }
    const updated: Service = {
      ...store.services[index],
      ...input,
      id: serviceId,
      updated_at: nowISO(),
    }
    store.services[index] = updated
    return updated
  }

  if (method === 'DELETE') {
    if (index === -1) fail(500, 'internal server error')
    const referencedBy = store.routes.filter(
      (route) => route.service_id === serviceId,
    ).length
    if (referencedBy > 0) {
      fail(
        409,
        `service still referenced by ${referencedBy} route(s); delete them first`,
      )
    }
    store.services.splice(index, 1)
    store.nodes = store.nodes.filter((node) => node.service_id !== serviceId)
    return undefined
  }

  fail(405, 'method not allowed')
}

function handleNodes(
  method: string,
  segments: string[],
  body: unknown,
): unknown {
  const nodeId = parseId(segments[1])
  const index = store.nodes.findIndex((node) => node.id === nodeId)

  if (method === 'PUT') {
    if (index === -1) fail(500, 'internal node error')
    const input = applyNodeInput(bodyRecord(body))
    const updated: Node = {
      ...store.nodes[index],
      ...input,
      id: nodeId,
      updated_at: nowISO(),
    }
    store.nodes[index] = updated
    return updated
  }

  if (method === 'DELETE') {
    if (index === -1) fail(500, 'internal server error')
    store.nodes.splice(index, 1)
    return undefined
  }

  fail(405, 'method not allowed')
}

function handleRoutes(
  method: string,
  segments: string[],
  body: unknown,
): unknown {
  if (segments.length === 1) {
    if (method === 'GET') return store.routes
    if (method === 'POST') {
      const input = bodyRecord(body)
      const name = stringField(input, 'name')
      if (name === '') fail(400, 'name is required')
      const serviceId = numberField(input, 'service_id')
      if (serviceId <= 0) fail(400, 'service_id is required')
      const pathPattern = stringField(input, 'path_pattern')
      if (pathPattern === '') fail(400, 'path_pattern is required')

      if (!store.services.some((service) => service.id === serviceId)) {
        fail(400, `service ${serviceId} not found`)
      }

      const candidate = {
        host: stringField(input, 'host'),
        path_pattern: pathPattern,
        path_match_type: stringField(input, 'path_match_type'),
        methods: stringField(input, 'methods'),
      }
      const duplicate = findRouteConflict(candidate, 0)
      if (duplicate) {
        fail(
          409,
          `路由冲突: host="${candidate.host}" path="${candidate.path_pattern}" match="${candidate.path_match_type}" methods="${candidate.methods}" 已被路由 id=${duplicate.id} 占用，请改为修改那条路由`,
        )
      }

      const route: Route = {
        ...candidate,
        id: store.next.route,
        service_id: serviceId,
        name,
        enabled: booleanField(input, 'enabled', false),
        created_at: nowISO(),
        updated_at: nowISO(),
        max_concurrency: numberField(input, 'max_concurrency'),
      }
      store.next.route += 1
      store.routes.push(route)
      return route
    }
    fail(405, 'method not allowed')
  }

  const routeId = parseId(segments[1])
  const index = store.routes.findIndex((route) => route.id === routeId)

  if (method === 'PUT') {
    if (index === -1) fail(500, 'internal server error')
    const input = bodyRecord(body)
    const name = stringField(input, 'name')
    if (name === '') fail(400, 'name is required')
    const serviceId = numberField(input, 'service_id')
    if (serviceId <= 0) fail(400, 'service_id is required')
    const pathPattern = stringField(input, 'path_pattern')
    if (pathPattern === '') fail(400, 'path_pattern is required')

    if (!store.services.some((service) => service.id === serviceId)) {
      fail(400, `service ${serviceId} not found`)
    }

    const candidate = {
      host: stringField(input, 'host'),
      path_pattern: pathPattern,
      path_match_type: stringField(input, 'path_match_type'),
      methods: stringField(input, 'methods'),
    }
    const duplicate = findRouteConflict(candidate, routeId)
    if (duplicate) {
      fail(
        409,
        `路由冲突: host="${candidate.host}" path="${candidate.path_pattern}" match="${candidate.path_match_type}" methods="${candidate.methods}" 已被路由 id=${duplicate.id} 占用，请改为修改那条路由`,
      )
    }

    const updated: Route = {
      ...store.routes[index],
      ...candidate,
      id: routeId,
      service_id: serviceId,
      name,
      enabled: booleanField(input, 'enabled', false),
      updated_at: nowISO(),
      max_concurrency: numberField(input, 'max_concurrency'),
    }
    store.routes[index] = updated
    return updated
  }

  if (method === 'DELETE') {
    if (index === -1) fail(500, 'internal server error')
    store.routes.splice(index, 1)
    return undefined
  }

  fail(405, 'method not allowed')
}

function handleVersions(
  method: string,
  segments: string[],
  _body: unknown,
): unknown {
  if (segments.length === 1) {
    if (method !== 'GET') fail(405, 'method not allowed')
    return [...store.versions].sort((a, b) => b.version - a.version)
  }

  const versionId = parseId(segments[1])

  if (segments.length === 3 && segments[2] === 'rollback') {
    if (method !== 'POST') fail(405, 'method not allowed')
    const target = store.versions.find((version) => version.id === versionId)
    if (!target) fail(404, 'version not found')

    const sourceItems = store.versionItems.filter(
      (item) => item.version_id === versionId,
    )
    const timestamp = nowISO()
    const copiedItems = sourceItems.map((item) => ({
      ...item,
      id: 0,
    }))
    return createVersion(copiedItems, target.config_hash ?? '', timestamp)
  }

  if (segments.length !== 2) fail(404, 'not found')
  if (method !== 'GET') fail(405, 'method not allowed')

  const version = store.versions.find((item) => item.id === versionId)
  if (!version) fail(404, 'version not found')
  const items = store.versionItems
    .filter((item) => item.version_id === versionId)
    .sort((a, b) => a.id - b.id)
  return { version, items }
}

function handlePublish(method: string): unknown {
  if (method !== 'POST') fail(405, 'method not allowed')
  validatePublish()
  // 先在内存里生成条目，校验和哈希都基于同一份当前配置。
  const items = buildSnapshotItems()
  return createVersion(items, currentConfigHash(), nowISO())
}

// ---- AI 配置 ----

function handleAI(
  method: string,
  segments: string[],
  body: unknown,
): unknown {
  const resource = segments[1]
  if (resource !== 'providers' && resource !== 'models') {
    fail(404, 'not found')
  }

  if (resource === 'providers') {
    if (segments.length === 2) {
      if (method === 'GET') {
        return [...store.providers].sort((a, b) => a.id - b.id)
      }
      if (method === 'POST') {
        const input = applyProviderInput(bodyRecord(body))
        if (
          store.providers.some((provider) => provider.name === input.name)
        ) {
          fail(409, 'name already exists')
        }
        const provider: AIProvider = {
          ...input,
          id: store.next.provider,
          created_at: nowISO(),
          updated_at: nowISO(),
        }
        store.next.provider += 1
        store.providers.push(provider)
        store.aiConfigVersion += 1
        return provider
      }
      fail(405, 'method not allowed')
    }

    const providerId = parseId(segments[2])
    const index = store.providers.findIndex(
      (provider) => provider.id === providerId,
    )

    if (method === 'PUT') {
      if (index === -1) fail(404, 'provider not found')
      const existing = store.providers[index]
      const input = applyProviderInput(bodyRecord(body))
      if (
        store.providers.some(
          (provider) =>
            provider.id !== providerId && provider.name === input.name,
        )
      ) {
        fail(409, 'name already exists')
      }
      const updated: AIProvider = {
        ...existing,
        ...input,
        id: providerId,
        created_at: existing.created_at,
        updated_at: nowISO(),
      }
      store.providers[index] = updated
      if (updated.name !== existing.name) {
        for (const model of store.models) {
          if (model.provider_name === existing.name) {
            model.provider_name = updated.name
          }
        }
      }
      store.aiConfigVersion += 1
      return updated
    }

    if (method === 'DELETE') {
      if (index === -1) fail(404, 'not found')
      const name = store.providers[index].name
      store.providers.splice(index, 1)
      store.models = store.models.filter(
        (model) => model.provider_name !== name,
      )
      store.aiConfigVersion += 1
      return undefined
    }

    fail(405, 'method not allowed')
  }

  // models：别名是主键，路径参数需要 URL 解码。
  if (segments.length === 2) {
    if (method === 'GET') {
      return [...store.models].sort((a, b) => a.name.localeCompare(b.name))
    }
    if (method === 'POST') {
      const input = applyModelInput(bodyRecord(body))
      ensureProvider(input.provider_name)
      if (store.models.some((model) => model.name === input.name)) {
        fail(409, 'name already exists')
      }
      const model: AIModel = {
        ...input,
        created_at: nowISO(),
        updated_at: nowISO(),
      }
      store.models.push(model)
      store.aiConfigVersion += 1
      return model
    }
    fail(405, 'method not allowed')
  }

  if (segments.length === 3) {
    const name = segments[2]
    if (!MODEL_NAME_RE.test(name)) fail(400, 'invalid model name')
    const index = store.models.findIndex((model) => model.name === name)

    if (method === 'PUT') {
      if (index === -1) fail(404, 'not found')
      const input = applyModelInput(bodyRecord(body))
      input.name = name
      ensureProvider(input.provider_name)
      const existing = store.models[index]
      const updated: AIModel = {
        ...input,
        created_at: existing.created_at,
        updated_at: nowISO(),
      }
      store.models[index] = updated
      store.aiConfigVersion += 1
      return updated
    }

    if (method === 'DELETE') {
      if (index === -1) fail(404, 'not found')
      store.models.splice(index, 1)
      store.aiConfigVersion += 1
      return undefined
    }

    fail(405, 'method not allowed')
  }

  fail(404, 'not found')
}

function dispatch(path: string, options: RequestOptions): unknown {
  const method = options.method ?? 'GET'
  const url = new URL(path, 'http://mock.flowgate.local')
  const segments = decodeSegments(url.pathname)

  if (segments.length === 1 && segments[0] === 'health') {
    if (method !== 'GET') fail(405, 'method not allowed')
    return { status: 'ok' }
  }

  if (segments.length === 1 && segments[0] === 'publish') {
    return handlePublish(method)
  }

  if (segments[0] === 'tenants') {
    return handleTenants(method, segments, options.body)
  }
  if (segments[0] === 'keys' && segments.length === 2) {
    return handleAPIKeys(method, segments, options.body)
  }
  if (segments[0] === 'services') {
    return handleServices(method, segments, options.body)
  }
  if (segments[0] === 'nodes' && segments.length === 2) {
    return handleNodes(method, segments, options.body)
  }
  if (segments[0] === 'routes') {
    return handleRoutes(method, segments, options.body)
  }
  if (segments[0] === 'versions') {
    return handleVersions(method, segments, options.body)
  }
  if (segments[0] === 'ai') {
    return handleAI(method, segments, options.body)
  }

  fail(404, 'not found')
}

export async function mockRequest<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  if (options.signal?.aborted) {
    throw new ApiError('请求已取消', 'canceled')
  }

  // 稳定的少量延迟，让 loading / 骨架屏等状态在演示时可见。
  await delay(90 + Math.floor(Math.random() * 130), options.signal)

  try {
    return clone(dispatch(path, options)) as T
  } catch (error) {
    if (error instanceof MockHttpError) {
      throw new ApiError(error.message, error.kind, error.status)
    }
    throw error
  }
}
