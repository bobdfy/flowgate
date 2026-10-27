/**
 * HTTP 路由契约。
 *
 * 匹配规则唯一性由后端按
 * host + path_pattern + path_match_type + 规范化后的 methods 判断。
 * `path_match_type` 只有 "exact" 是特殊值，其余值在 gateway 中按前缀匹配处理；
 * 控制台表单只提供 prefix / exact。
 */
export interface Route {
  id: number
  service_id: number
  name: string
  host: string
  path_pattern: string
  path_match_type: string
  methods: string
  enabled: boolean
  created_at: string
  updated_at: string
  max_concurrency: number
}

export interface RouteWriteInput {
  service_id: number
  name: string
  host: string
  path_pattern: string
  path_match_type: string
  methods: string
  enabled: boolean
  max_concurrency: number
}
