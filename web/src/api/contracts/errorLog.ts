/** 错误分类：与 gateway 的 internal/errorlog.Kind 一一对应。 */
export type ErrorKind =
  | 'upstream_error'
  | 'timeout'
  | 'overload'
  | 'circuit_open'
  | 'stream_terminated'
  | 'auth_failed'
  | 'internal_error'

/**
 * 错误看板的一行 = 一种错误（方法 + 路由 + 状态码 + 分类），不是一次请求。
 * 其余字段取自该种错误最近一次发生时的实际值（不含请求头、请求体与 Key 原文）。
 */
export interface ErrorLogEntry {
  ts: string
  instance: string
  request_id: string
  method: string
  route: string
  status: number
  duration_ms: number
  kind: ErrorKind
  detail?: string
  tenant_id?: number
  key_id?: number
  /** 窗口内该种错误的累计次数。 */
  count: number
}

export interface ErrorLogResponse {
  entries: ErrorLogEntry[]
  /** 过滤后的行数（= 错误种类数）；entries 可能因 limit 被截短。 */
  total: number
  /** 窗口上限（500 种错误）：更久没发生的错误已经被挤掉。 */
  capacity: number
}

export interface ErrorLogQuery {
  kind?: string
  status?: number
  route?: string
  request_id?: string
  tenant_id?: number
  /** 时间长度，如 15m、1h；不传表示窗口内全部。 */
  since?: string
  limit?: number
}
