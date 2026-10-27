/**
 * Mock 适配层共用的序列化工具。
 *
 * 版本条目必须和真实管理面一致：`resource_snapshot` 是 JSON UTF-8 字节的
 * base64，控制台用 `atob` + `TextDecoder` 还原。这里按同样方向编码。
 */
export function encodeSnapshot(value: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(value))
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return window.btoa(binary)
}

/**
 * 真实后端用 SHA-256。Mock 只要求稳定、定长的展示值，不参与任何校验，
 * 因此用多路 FNV-1a 生成 64 位十六进制字符串。
 */
export function hashConfig(parts: unknown[]): string {
  const text = JSON.stringify(parts)
  const seeds = [
    0x811c9dc5, 0x9e3779b9, 0x85ebca6b, 0xc2b2ae35,
    0x27d4eb2f, 0x165667b1, 0xd3a2646c, 0xfd7046c5,
  ]

  return seeds
    .map((seed) => {
      let hash = seed >>> 0
      for (let index = 0; index < text.length; index += 1) {
        hash ^= text.charCodeAt(index)
        hash = Math.imul(hash, 0x01000193) >>> 0
      }
      return hash.toString(16).padStart(8, '0')
    })
    .join('')
}

/** 返回深拷贝，模拟 HTTP 响应的 JSON 边界，避免调用方直接改动内存库。 */
export function clone<T>(value: T): T {
  return structuredClone(value)
}
