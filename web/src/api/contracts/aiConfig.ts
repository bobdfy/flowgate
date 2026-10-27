/**
 * AI 提供商与模型映射契约。
 *
 * 字段与 `internal/model/aiconfig.go` 的 JSON 标签一致。
 * `APIKey` 在 Go 模型里是 `json:"-"`，管理 API 永远不会返回密钥；
 * 写请求结构（`internal/api/aiconfig.go`）也不接受明文 `api_key`。
 */
export interface AIProvider {
  id: number
  name: string
  type: string
  endpoint: string
  upstream_path: string
  api_key_env: string
  stream_usage: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface AIModel {
  name: string
  provider_name: string
  upstream_model: string
  enabled: boolean
  created_at: string
  updated_at: string
}

/**
 * Provider 写请求。PUT 全量覆盖除密钥外的可写字段。
 * 后端会把空 type 默认成 openai，并去掉 endpoint 末尾的 `/`。
 */
export interface AIProviderWriteInput {
  name: string
  type: string
  endpoint: string
  upstream_path: string
  api_key_env: string
  stream_usage: boolean
  enabled: boolean
}

/**
 * Model 写请求。`name` 是主键，`PUT /ai/models/{name}` 不接受改名。
 */
export interface AIModelWriteInput {
  name: string
  provider_name: string
  upstream_model: string
  enabled: boolean
}
