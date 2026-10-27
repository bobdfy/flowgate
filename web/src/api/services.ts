import type {
  Service,
  ServiceWriteInput,
} from './contracts/service'
import { request } from './http'

export function listServices(signal?: AbortSignal): Promise<Service[]> {
  return request<Service[]>('/services', { signal })
}

export function getService(
  id: number,
  signal?: AbortSignal,
): Promise<Service> {
  return request<Service>(`/services/${id}`, { signal })
}

export function createService(input: ServiceWriteInput): Promise<Service> {
  return request<Service>('/services', { method: 'POST', body: input })
}

export function updateService(
  id: number,
  input: ServiceWriteInput,
): Promise<Service> {
  return request<Service>(`/services/${id}`, { method: 'PUT', body: input })
}

export function deleteService(id: number): Promise<void> {
  return request<void>(`/services/${id}`, { method: 'DELETE' })
}
