import type {
  AIModelWriteInput,
  AIProviderWriteInput,
} from '../../api/contracts/aiConfig'

export type ValidationResult<T> =
  | { ok: true; input: T }
  | { ok: false; error: string }

class FieldError extends Error {}

function fail(message: string): never {
  throw new FieldError(message)
}

function requireText(raw: string, label: string, maxLength: number): string {
  const value = raw.trim()
  if (!value) {
    fail(`${label}不能为空。`)
  }
  if (value.length > maxLength) {
    fail(`${label}不能超过 ${maxLength} 个字符。`)
  }
  return value
}

function optionalText(raw: string, label: string, maxLength: number): string {
  const value = raw.trim()
  if (value.length > maxLength) {
    fail(`${label}不能超过 ${maxLength} 个字符。`)
  }
  return value
}

function validateWith<T>(build: () => T): ValidationResult<T> {
  try {
    return { ok: true, input: build() }
  } catch (error) {
    if (error instanceof FieldError) {
      return { ok: false, error: error.message }
    }
    throw error
  }
}

// 与 `internal/api/aiconfig.go` 的校验保持一致。
const providerNamePattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/
const modelNamePattern = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$/
const envNamePattern = /^[A-Za-z_][A-Za-z0-9_]*$/

export interface AIProviderFormValues {
  name: string
  type: string
  endpoint: string
  upstreamPath: string
  apiKeyEnv: string
  streamUsage: boolean
  enabled: boolean
}

export function validateAIProviderForm(
  values: AIProviderFormValues,
  current: { name: string } | undefined,
  existingNames: string[],
): ValidationResult<AIProviderWriteInput> {
  return validateWith(() => {
    const name = values.name.trim()
    if (!name) {
      fail('提供商名称不能为空。')
    }
    if (!providerNamePattern.test(name)) {
      fail(
        '提供商名称需以字母或数字开头，只能包含字母、数字、点、短横线和下划线，最长 64 个字符。',
      )
    }
    if (
      existingNames.some(
        (existing) => existing === name && existing !== current?.name,
      )
    ) {
      fail('提供商名称已存在，请更换名称。')
    }

    const type = values.type.trim() || 'openai'
    if (type !== 'openai') {
      fail('提供商类型目前只支持 openai（OpenAI 兼容上游）。')
    }

    // 后端保存时会 TrimSpace 并去掉末尾的 `/`，这里保持一致。
    const endpoint = values.endpoint.trim().replace(/\/+$/, '')
    if (!endpoint) {
      fail('上游端点不能为空。')
    }
    if (endpoint.length > 512) {
      fail('上游端点不能超过 512 个字符。')
    }
    let parsed: URL
    try {
      parsed = new URL(endpoint)
    } catch {
      fail('上游端点必须是完整的 http/https URL，例如 https://api.example.com。')
    }
    if (
      (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') ||
      !parsed.hostname
    ) {
      fail('上游端点必须是完整的 http/https URL，例如 https://api.example.com。')
    }

    const upstreamPath = optionalText(values.upstreamPath, '上游路径', 255)
    if (upstreamPath && !upstreamPath.startsWith('/')) {
      fail('上游路径必须以 / 开头，或留空使用默认路径。')
    }

    const apiKeyEnv = optionalText(values.apiKeyEnv, '密钥环境变量名', 128)
    if (apiKeyEnv && !envNamePattern.test(apiKeyEnv)) {
      fail(
        '密钥环境变量名只能包含字母、数字和下划线，且不能以数字开头。',
      )
    }

    return {
      name,
      type,
      endpoint,
      upstream_path: upstreamPath,
      api_key_env: apiKeyEnv,
      stream_usage: values.streamUsage,
      enabled: values.enabled,
    }
  })
}

export interface AIModelFormValues {
  name: string
  providerName: string
  upstreamModel: string
  enabled: boolean
}

export function validateAIModelForm(
  values: AIModelFormValues,
  current: { name: string } | undefined,
  existingNames: string[],
  providerNames: string[],
): ValidationResult<AIModelWriteInput> {
  return validateWith(() => {
    const name = values.name.trim()
    if (!name) {
      fail('模型别名不能为空。')
    }
    if (!modelNamePattern.test(name)) {
      fail(
        '模型别名需以字母或数字开头，只能包含字母、数字、点、冒号、短横线和下划线，最长 64 个字符。',
      )
    }
    if (name !== current?.name && existingNames.includes(name)) {
      fail('模型别名已存在，请更换名称。')
    }

    const providerName = requireText(
      values.providerName,
      '所属提供商',
      64,
    )
    if (!providerNames.includes(providerName)) {
      fail('所选提供商不存在，请刷新提供商列表后重试。')
    }

    const upstreamModel = optionalText(
      values.upstreamModel,
      '上游模型名',
      128,
    )

    return {
      name,
      provider_name: providerName,
      upstream_model: upstreamModel,
      enabled: values.enabled,
    }
  })
}
