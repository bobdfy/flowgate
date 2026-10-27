import { isApiError } from '../../api/errors'
import { getErrorMessage } from '../../lib/errorMessage'

/**
 * 服务删除被路由引用时，后端返回 409 与英文原文
 * `service still referenced by N route(s); delete them first`。
 * 这里翻译成可执行的中文提示；无法解析时保留原文，避免掩盖真实错误。
 */
export function serviceDeleteErrorMessage(error: unknown): string {
  if (isApiError(error) && error.status === 409) {
    const match = /referenced by (\d+) route/i.exec(error.message)
    if (match) {
      return `该服务仍被 ${match[1]} 条路由引用，请先修改或删除这些路由，再删除服务。`
    }
    return `服务仍被路由引用，无法删除。管理面返回：${error.message}`
  }
  return getErrorMessage(error)
}
