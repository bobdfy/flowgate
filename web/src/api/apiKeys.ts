import type {
  APIKey,
  CreateAPIKeyInput,
  CreateAPIKeyResponse,
  UpdateAPIKeyInput,
} from './contracts/apiKey'
import { request } from './http'

export function listAPIKeys(
  tenantId: number,
  signal?: AbortSignal,
): Promise<APIKey[]> {
  return request<APIKey[]>(`/tenants/${tenantId}/keys`, { signal })
}

export function createAPIKey(
  tenantId: number,
  input: CreateAPIKeyInput,
): Promise<CreateAPIKeyResponse> {
  return request<CreateAPIKeyResponse>(`/tenants/${tenantId}/keys`, {
    method: 'POST',
    body: input,
  })
}

export function updateAPIKey(
  id: number,
  input: UpdateAPIKeyInput,
): Promise<APIKey> {
  return request<APIKey>(`/keys/${id}`, { method: 'PUT', body: input })
}

export function deleteAPIKey(id: number): Promise<void> {
  return request<void>(`/keys/${id}`, { method: 'DELETE' })
}
