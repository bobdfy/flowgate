import type {
  CreateTenantInput,
  Tenant,
  UpdateTenantInput,
} from './contracts/tenant'
import { request } from './http'

export function listTenants(signal?: AbortSignal): Promise<Tenant[]> {
  return request<Tenant[]>('/tenants', { signal })
}

export function getTenant(id: number, signal?: AbortSignal): Promise<Tenant> {
  return request<Tenant>(`/tenants/${id}`, { signal })
}

export function createTenant(input: CreateTenantInput): Promise<Tenant> {
  return request<Tenant>('/tenants', { method: 'POST', body: input })
}

export function updateTenant(
  id: number,
  input: UpdateTenantInput,
): Promise<Tenant> {
  return request<Tenant>(`/tenants/${id}`, { method: 'PUT', body: input })
}

export function deleteTenant(id: number): Promise<void> {
  return request<void>(`/tenants/${id}`, { method: 'DELETE' })
}
