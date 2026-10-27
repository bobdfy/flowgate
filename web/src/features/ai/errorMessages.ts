import { isApiError } from '../../api/errors'
import { getErrorMessage } from '../../lib/errorMessage'

/**
 * 管理 API 的 AI 校验文案统一为英文。这里翻译用户最可能触发的几种；
 * 未覆盖的消息保留原文，避免掩盖真实错误。
 */
function translateAIMessage(message: string): string {
  switch (message) {
    case 'name is required':
      return '名称不能为空。'
    case 'provider_name is required':
      return '所属提供商不能为空。'
    case 'name already exists':
      return '名称已存在，请更换后重试。'
    case 'not found':
      return '该记录已不存在，可能已被其他会话删除。请刷新列表。'
    case 'invalid id':
      return '提供商 ID 无效。请刷新列表后重试。'
    case 'invalid model name':
      return '模型别名不符合后端约束，无法通过管理 API 修改或删除。'
    case 'provider does not exist':
      return '所选提供商不存在，请刷新提供商列表后重试。'
    case 'type must be openai (only OpenAI-compatible upstreams are supported)':
      return '提供商类型只支持 openai（OpenAI 兼容上游）。'
    case 'endpoint is required':
      return '上游端点不能为空。'
    case 'endpoint must be a full http/https URL':
      return '上游端点必须是完整的 http/https URL。'
    case 'upstream_path must start with /':
      return '上游路径必须以 / 开头，或留空使用默认路径。'
    case 'api_key_env must be a valid environment variable name':
      return '密钥环境变量名只能包含字母、数字和下划线，且不能以数字开头。'
    case 'name may only contain letters, digits, dot, dash and underscore':
      return '提供商名称只能包含字母、数字、点、短横线和下划线，且需以字母或数字开头。'
    case 'name may only contain letters, digits, dot, colon, dash and underscore':
      return '模型别名只能包含字母、数字、点、冒号、短横线和下划线，且需以字母或数字开头。'
    default:
      return ''
  }
}

export function aiErrorMessage(error: unknown): string {
  if (isApiError(error)) {
    const translated = translateAIMessage(error.message)
    if (translated) {
      return translated
    }
    if (error.status === 409) {
      return '名称已存在，请更换后重试。'
    }
    if (error.status === 404) {
      return '该记录已不存在，可能已被其他会话删除。请刷新列表。'
    }
  }
  return getErrorMessage(error)
}
