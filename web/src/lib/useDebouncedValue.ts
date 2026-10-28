import { useEffect, useState } from 'react'

/**
 * 延迟返回输入值，避免每敲一个字符就打一次服务端查询。
 * 过滤器输入用 300ms：足够让中文输入法完成一次组合，用户感觉仍是即时。
 */
export function useDebouncedValue<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState(value)

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(timer)
  }, [value, delayMs])

  return debounced
}
