export interface TPMWindowSettings {
  configured: boolean
  /** 数据库里保存的窗口；0 = 还没通过控制台配置过 */
  window_seconds: number
  /** gateway 当前实际使用的窗口；0 = 尚未上报 */
  effective_window_seconds: number
  updated_at: string | null
}
