import type {
  AIModel,
  AIModelWriteInput,
  AIProvider,
  AIProviderWriteInput,
} from './contracts/aiConfig'
import { request } from './http'

export function listAIProviders(signal?: AbortSignal): Promise<AIProvider[]> {
  return request<AIProvider[]>('/ai/providers', { signal })
}

export function createAIProvider(
  input: AIProviderWriteInput,
): Promise<AIProvider> {
  return request<AIProvider>('/ai/providers', { method: 'POST', body: input })
}

export function updateAIProvider(
  id: number,
  input: AIProviderWriteInput,
): Promise<AIProvider> {
  return request<AIProvider>(`/ai/providers/${id}`, {
    method: 'PUT',
    body: input,
  })
}

export function deleteAIProvider(id: number): Promise<void> {
  return request<void>(`/ai/providers/${id}`, { method: 'DELETE' })
}

export function listAIModels(signal?: AbortSignal): Promise<AIModel[]> {
  return request<AIModel[]>('/ai/models', { signal })
}

export function createAIModel(input: AIModelWriteInput): Promise<AIModel> {
  return request<AIModel>('/ai/models', { method: 'POST', body: input })
}

/**
 * 模型别名是主键，路径参数需要编码；后端允许 `.`、`:`、`-`、`_`。
 */
export function updateAIModel(
  name: string,
  input: AIModelWriteInput,
): Promise<AIModel> {
  return request<AIModel>(`/ai/models/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: input,
  })
}

export function deleteAIModel(name: string): Promise<void> {
  return request<void>(`/ai/models/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}
