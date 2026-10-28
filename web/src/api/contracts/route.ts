/**
 * HTTP 路由契约。
 *
 * 匹配规则唯一性由后端按
 * host + path_pattern + path_match_type + 规范化后的 methods 判断。
 *
 * 写接口会归一化这两个字段：`path_match_type` 只接受 prefix / exact
 * （不传按 prefix），`methods` 统一转大写、去重、排序，非标准 HTTP 方法会被拒绝。
 * 读取时按原样展示即可，存进去的一定已经是规范形式。
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
