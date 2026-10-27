/**
 * 上游服务契约。
 *
 * 字段与 `internal/model/model.go` 的 Service JSON 标签一致。
 * `protocol` / `overload_strategy` 后端不做枚举校验，因此读取按 string 处理，
 * 控制台表单只提供当前实现认识的值。
 */
export interface Service {
  id: number
  name: string
  protocol: string
  connect_timeout_ms: number
  request_timeout_ms: number
  enabled: boolean
  created_at: string
  updated_at: string
  max_retries: number
  retry_on_status: string
  retry_backoff_ms: number
  cb_failure_threshold: number
  cb_cooldown_ms: number
  cb_half_open_limit: number
  response_header_timeout_ms: number
  max_concurrency: number
  queue_timeout_ms: number
  overload_strategy: string
}

/**
 * 服务写请求。PUT 是全量覆盖语义，因此表单必须提交全部可写字段，
 * 不能只发送被修改的项。
 */
export interface ServiceWriteInput {
  name: string
  protocol: string
  connect_timeout_ms: number
  request_timeout_ms: number
  enabled: boolean
  max_retries: number
  retry_on_status: string
  retry_backoff_ms: number
  cb_failure_threshold: number
  cb_cooldown_ms: number
  cb_half_open_limit: number
  response_header_timeout_ms: number
  max_concurrency: number
  queue_timeout_ms: number
  overload_strategy: string
}
