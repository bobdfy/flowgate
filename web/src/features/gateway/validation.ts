import type { NodeWriteInput } from '../../api/contracts/node'
import type { RouteWriteInput } from '../../api/contracts/route'
import type { ServiceWriteInput } from '../../api/contracts/service'

export type ValidationResult<T> =
  | { ok: true; input: T }
  | { ok: false; error: string }

interface ServiceLike {
  id: number
  name: string
}

class FieldError extends Error {}

function fail(message: string): never {
  throw new FieldError(message)
}

function readInteger(
  raw: string,
  label: string,
  min: number,
  max = 2_147_483_647,
): number {
  if (!raw.trim()) {
    fail(`${label}不能为空。`)
  }
  const value = Number(raw)
  if (!Number.isInteger(value)) {
    fail(`${label}必须是整数。`)
  }
  if (value < min || value > max) {
    fail(`${label}必须在 ${min} 到 ${max} 之间。`)
  }
  return value
}

function requireText(
  raw: string,
  label: string,
  maxLength: number,
): string {
  const value = raw.trim()
  if (!value) {
    fail(`${label}不能为空。`)
  }
  if (value.length > maxLength) {
    fail(`${label}不能超过 ${maxLength} 个字符。`)
  }
  return value
}

function optionalText(
  raw: string,
  label: string,
  maxLength: number,
): string {
  const value = raw.trim()
  if (value.length > maxLength) {
    fail(`${label}不能超过 ${maxLength} 个字符。`)
  }
  return value
}

function validateWith<T>(build: () => T): ValidationResult<T> {
  try {
    return { ok: true, input: build() }
  } catch (error) {
    if (error instanceof FieldError) {
      return { ok: false, error: error.message }
    }
    throw error
  }
}

function normalizeRetryStatuses(raw: string): string {
  const parts = raw
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)

  const normalizedParts = parts.map((part) => {
    const status = Number(part)
    if (!Number.isInteger(status) || status < 100 || status > 599) {
      fail(`重试状态码 "${part}" 无效，需为 100–599 的整数，逗号分隔。`)
    }
    return String(status)
  })

  const normalized = normalizedParts.join(',')
  if (normalized.length > 255) {
    fail('重试状态码不能超过 255 个字符。')
  }
  return normalized
}

function normalizeMethods(raw: string): string {
  const tokens = raw
    .split(',')
    .map((part) => part.trim().toUpperCase())
    .filter(Boolean)

  for (const token of tokens) {
    if (!/^[A-Z][A-Z0-9-]*$/.test(token)) {
      fail(`方法 "${token}" 格式无效，请使用逗号分隔的 HTTP 方法，例如 GET,POST。`)
    }
  }

  const normalized = tokens.join(',')
  if (normalized.length > 255) {
    fail('方法列表不能超过 255 个字符。')
  }
  return normalized
}

export interface ServiceFormValues {
  name: string
  protocol: string
  enabled: boolean
  connectTimeoutMs: string
  requestTimeoutMs: string
  responseHeaderTimeoutMs: string
  maxRetries: string
  retryOnStatus: string
  retryBackoffMs: string
  cbFailureThreshold: string
  cbCooldownMs: string
  cbHalfOpenLimit: string
  maxConcurrency: string
  queueTimeoutMs: string
  overloadStrategy: string
}

export function validateServiceForm(
  values: ServiceFormValues,
  current: ServiceLike | undefined,
  existingNames: string[],
): ValidationResult<ServiceWriteInput> {
  return validateWith(() => {
    const name = requireText(values.name, '服务名称', 255)
    if (
      existingNames.some(
        (existing) => existing === name && existing !== current?.name,
      )
    ) {
      fail('服务名称已存在，请更换名称。')
    }

    const protocol = requireText(values.protocol, '协议', 20)
    const overloadStrategy = requireText(
      values.overloadStrategy,
      '过载策略',
      20,
    )

    return {
      name,
      protocol,
      enabled: values.enabled,
      connect_timeout_ms: readInteger(
        values.connectTimeoutMs,
        '建连超时',
        1,
      ),
      request_timeout_ms: readInteger(
        values.requestTimeoutMs,
        '请求超时',
        1,
      ),
      response_header_timeout_ms: readInteger(
        values.responseHeaderTimeoutMs,
        '响应头超时',
        1,
      ),
      max_retries: readInteger(values.maxRetries, '最大重试次数', 0),
      retry_on_status: normalizeRetryStatuses(values.retryOnStatus),
      retry_backoff_ms: readInteger(
        values.retryBackoffMs,
        '重试退避时间',
        1,
      ),
      cb_failure_threshold: readInteger(
        values.cbFailureThreshold,
        '熔断失败阈值',
        1,
      ),
      cb_cooldown_ms: readInteger(
        values.cbCooldownMs,
        '熔断冷却时间',
        1,
      ),
      cb_half_open_limit: readInteger(
        values.cbHalfOpenLimit,
        '半开探测上限',
        1,
      ),
      max_concurrency: readInteger(
        values.maxConcurrency,
        '服务级最大并发',
        0,
      ),
      queue_timeout_ms: readInteger(
        values.queueTimeoutMs,
        '排队超时',
        0,
      ),
      overload_strategy: overloadStrategy,
    }
  })
}

function parseNodeUrl(address: string): URL {
  try {
    return new URL(address)
  } catch {
    return fail('节点地址必须是完整 URL，例如 http://127.0.0.1:8091。')
  }
}

export interface NodeFormValues {
  address: string
  weight: string
  enabled: boolean
}

export function validateNodeForm(
  values: NodeFormValues,
): ValidationResult<NodeWriteInput> {
  return validateWith(() => {
    const address = requireText(values.address, '节点地址', 255)
    const parsed = parseNodeUrl(address)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      fail('节点地址只支持 http 或 https 协议。')
    }
    if (!parsed.hostname) {
      fail('节点地址必须包含主机名。')
    }

    return {
      address,
      weight: readInteger(values.weight, '权重', 1),
      enabled: values.enabled,
    }
  })
}

export interface RouteFormValues {
  serviceId: string
  name: string
  host: string
  pathPattern: string
  pathMatchType: string
  methods: string
  enabled: boolean
  maxConcurrency: string
}

export function validateRouteForm(
  values: RouteFormValues,
): ValidationResult<RouteWriteInput> {
  return validateWith(() => {
    const name = requireText(values.name, '路由名称', 255)
    const host = optionalText(values.host, 'Host', 255)
    if (host && (/[/\s]/.test(host) || host.includes('://'))) {
      fail('Host 只填写域名（可含端口），不要包含协议或路径。')
    }

    const pathPattern = requireText(values.pathPattern, '路径模式', 255)
    if (!pathPattern.startsWith('/')) {
      fail('路径模式必须以 / 开头。')
    }

    const pathMatchType = requireText(
      values.pathMatchType,
      '匹配类型',
      20,
    )
    if (pathMatchType !== 'prefix' && pathMatchType !== 'exact') {
      fail('匹配类型只能是 prefix 或 exact。')
    }

    return {
      service_id: readInteger(values.serviceId, '目标服务', 1),
      name,
      host,
      path_pattern: pathPattern,
      path_match_type: pathMatchType,
      methods: normalizeMethods(values.methods),
      enabled: values.enabled,
      max_concurrency: readInteger(values.maxConcurrency, '路由级最大并发', 0),
    }
  })
}
