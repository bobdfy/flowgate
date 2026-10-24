export type APIKeyStatus = 'active' | 'disabled'

export interface APIKey {
  id: number
  tenant_id: number
  name: string
  status: APIKeyStatus
  qps_limit: number
  expires_at: string | null
  created_at: string
  last_used_at: string | null
}

export interface CreateAPIKeyInput {
  name: string
  status: APIKeyStatus
  qps_limit: number
  expires_at: string | null
}

/** 创建响应是唯一一次返回明文 key 的接口。 */
export interface CreateAPIKeyResponse {
  id: number
  tenant_id: number
  name: string
  status: APIKeyStatus
  expires_at: string | null
  created_at: string
  key: string
}

export interface UpdateAPIKeyInput {
  name: string
  status: APIKeyStatus
  qps_limit: number
  expires_at: string | null
}
