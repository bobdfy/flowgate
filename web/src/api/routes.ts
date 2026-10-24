import type { Route, RouteWriteInput } from './contracts/route'
import { request } from './http'

export function listRoutes(signal?: AbortSignal): Promise<Route[]> {
  return request<Route[]>('/routes', { signal })
}

export function createRoute(input: RouteWriteInput): Promise<Route> {
  return request<Route>('/routes', { method: 'POST', body: input })
}

export function updateRoute(
  id: number,
  input: RouteWriteInput,
): Promise<Route> {
  return request<Route>(`/routes/${id}`, { method: 'PUT', body: input })
}

export function deleteRoute(id: number): Promise<void> {
  return request<void>(`/routes/${id}`, { method: 'DELETE' })
}
