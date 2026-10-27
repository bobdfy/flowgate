import type { Node } from '../../api/contracts/node'
import type { Route } from '../../api/contracts/route'
import type { Service } from '../../api/contracts/service'
import type { VersionItem } from '../../api/contracts/version'

/**
 * 版本条目里的 resource_snapshot 是 Go []byte 的 JSON 编码，
 * 即 snapshot 对象的 UTF-8 字节再做 base64。这里按相反顺序还原。
 */
export function decodeSnapshot<T>(snapshot: string): T {
  const binary = window.atob(snapshot)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return JSON.parse(new TextDecoder().decode(bytes)) as T
}

export type DecodedSnapshot =
  | { type: 'service'; data: Service }
  | { type: 'node'; data: Node }
  | { type: 'route'; data: Route }
  | { type: 'unknown'; data: unknown }

export function decodeVersionItem(item: VersionItem): DecodedSnapshot {
  if (item.resource_type === 'service') {
    return { type: 'service', data: decodeSnapshot<Service>(item.resource_snapshot) }
  }
  if (item.resource_type === 'node') {
    return { type: 'node', data: decodeSnapshot<Node>(item.resource_snapshot) }
  }
  if (item.resource_type === 'route') {
    return { type: 'route', data: decodeSnapshot<Route>(item.resource_snapshot) }
  }
  return {
    type: 'unknown',
    data: decodeSnapshot<unknown>(item.resource_snapshot),
  }
}

export const resourceTypeLabels: Record<string, string> = {
  service: '服务',
  node: '节点',
  route: '路由',
}

export function resourceTypeLabel(type: string): string {
  return resourceTypeLabels[type] ?? type
}
