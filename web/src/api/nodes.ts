import type { Node, NodeWriteInput } from './contracts/node'
import { request } from './http'

export function listNodes(
  serviceId: number,
  signal?: AbortSignal,
): Promise<Node[]> {
  return request<Node[]>(`/services/${serviceId}/nodes`, { signal })
}

export function createNode(
  serviceId: number,
  input: NodeWriteInput,
): Promise<Node> {
  return request<Node>(`/services/${serviceId}/nodes`, {
    method: 'POST',
    body: input,
  })
}

export function updateNode(
  id: number,
  input: NodeWriteInput,
): Promise<Node> {
  return request<Node>(`/nodes/${id}`, { method: 'PUT', body: input })
}

export function deleteNode(id: number): Promise<void> {
  return request<void>(`/nodes/${id}`, { method: 'DELETE' })
}
