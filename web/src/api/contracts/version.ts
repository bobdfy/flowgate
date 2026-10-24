/**
 * 配置版本契约。
 *
 * `resource_snapshot` 在 JSON 中是 base64 字符串（Go []byte 的编码结果），
 * 解码后才是 service / node / route 的 JSON 快照。
 */
export interface Version {
  id: number
  version: number
  status: string
  config_hash: string | null
  published_at: string | null
  created_at: string
}

export interface VersionItem {
  id: number
  version_id: number
  resource_type: string
  resource_id: number
  resource_snapshot: string
}

export interface VersionDetail {
  version: Version
  items: VersionItem[]
}
