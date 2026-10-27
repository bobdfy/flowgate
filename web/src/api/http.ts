import { getAdminKey } from './adminKey'
import { ApiError, type ApiErrorKind } from './errors'
import { env } from '../lib/env'

export const UNAUTHORIZED_EVENT = 'flowgate:unauthorized'

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  headers?: HeadersInit
  signal?: AbortSignal
  timeoutMs?: number
  auth?: boolean
}

const DEFAULT_TIMEOUT_MS = 15_000

export function getApiUrl(path: string): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return `${env.adminBaseUrl}${normalizedPath}`
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

async function readErrorMessage(response: Response): Promise<string> {
  const text = (await response.text()).trim()
  if (!text) {
    return response.statusText || `HTTP ${response.status}`
  }

  const contentType = response.headers.get('content-type') ?? ''
  if (contentType.includes('application/json')) {
    try {
      const parsed: unknown = JSON.parse(text)
      if (isRecord(parsed)) {
        if (typeof parsed.error === 'string' && parsed.error.trim()) {
          return parsed.error
        }
        if (isRecord(parsed.error) && typeof parsed.error.message === 'string') {
          return parsed.error.message
        }
      }
    } catch {
      // Fall through to the raw response text.
    }
  }

  return text
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

export async function request<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  if (env.dataMode === 'mock') {
    // 动态导入让 Mock 数据与逻辑只出现在演示构建的独立 chunk 中。
    const { mockRequest } = await import('./mock/adapter')
    return mockRequest<T>(path, options)
  }

  const controller = new AbortController()
  let timedOut = false
  let timeoutId: ReturnType<typeof setTimeout> | undefined

  const onExternalAbort = () => {
    controller.abort()
  }

  if (options.signal) {
    if (options.signal.aborted) {
      controller.abort()
    } else {
      options.signal.addEventListener('abort', onExternalAbort, { once: true })
    }
  }

  timeoutId = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, options.timeoutMs ?? DEFAULT_TIMEOUT_MS)

  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')

  const adminKey = getAdminKey()
  if (options.auth !== false && adminKey) {
    headers.set('X-Admin-Key', adminKey)
  }

  let body: string | undefined
  if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(options.body)
  }

  try {
    const response = await fetch(getApiUrl(path), {
      method: options.method ?? 'GET',
      headers,
      body,
      signal: controller.signal,
    })

    if (!response.ok) {
      const message = await readErrorMessage(response)
      if (response.status === 401) {
        window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
      }
      throw new ApiError(message, statusToKind(response.status), response.status)
    }

    if (response.status === 204) {
      return undefined as T
    }

    const contentType = response.headers.get('content-type') ?? ''
    if (!contentType.includes('application/json')) {
      const text = await response.text()
      return (text || undefined) as T
    }

    const text = await response.text()
    if (!text) {
      return undefined as T
    }
    try {
      return JSON.parse(text) as T
    } catch {
      throw new ApiError(
        '管理面返回了无法解析的 JSON 响应',
        'unknown',
        response.status,
      )
    }
  } catch (error) {
    if (error instanceof ApiError) {
      throw error
    }
    if (controller.signal.aborted) {
      if (options.signal?.aborted) {
        throw new ApiError('请求已取消', 'canceled')
      }
      if (timedOut) {
        throw new ApiError('请求超时', 'timeout')
      }
    }
    throw new ApiError('无法连接管理面', 'network')
  } finally {
    if (timeoutId !== undefined) {
      clearTimeout(timeoutId)
    }
    options.signal?.removeEventListener('abort', onExternalAbort)
  }
}
