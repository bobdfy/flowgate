import { isApiError } from '../api/errors'

export function getErrorMessage(error: unknown): string {
  if (isApiError(error)) {
    if (error.kind === 'network') {
      return '无法连接管理面，请确认 admin 已启动且代理地址正确。'
    }
    if (error.kind === 'timeout') {
      return '管理面响应超时，请稍后重试。'
    }
    if (error.kind === 'unauthorized') {
      return '管理密钥无效或已过期。'
    }
    if (error.kind === 'canceled') {
      return '请求已取消。'
    }
    return error.message
  }
  if (error instanceof Error) {
    return error.message
  }
  return '发生未知错误。'
}
