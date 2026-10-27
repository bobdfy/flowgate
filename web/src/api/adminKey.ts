const SESSION_KEY = 'flowgate.admin.key'
const REMEMBER_KEY = 'flowgate.admin.remember'

export interface AdminKeySnapshot {
  key: string
  remember: boolean
}

function readSnapshot(): AdminKeySnapshot {
  if (typeof window === 'undefined') {
    return { key: '', remember: false }
  }
  try {
    const remember = window.sessionStorage.getItem(REMEMBER_KEY) === '1'
    if (!remember) {
      return { key: '', remember: false }
    }
    return {
      key: window.sessionStorage.getItem(SESSION_KEY) ?? '',
      remember: true,
    }
  } catch {
    return { key: '', remember: false }
  }
}

let snapshot = readSnapshot()
const listeners = new Set<() => void>()

export function subscribeAdminKey(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function getAdminKeySnapshot(): AdminKeySnapshot {
  return snapshot
}

export function getAdminKey(): string {
  return snapshot.key
}

export function setAdminKey(key: string, remember: boolean): void {
  const normalized = key.trim()
  if (typeof window !== 'undefined') {
    try {
      if (remember && normalized) {
        window.sessionStorage.setItem(SESSION_KEY, normalized)
        window.sessionStorage.setItem(REMEMBER_KEY, '1')
      } else {
        window.sessionStorage.removeItem(SESSION_KEY)
        window.sessionStorage.removeItem(REMEMBER_KEY)
      }
    } catch {
      // sessionStorage can be unavailable in restricted browser modes.
    }
  }
  snapshot = { key: normalized, remember: remember && normalized.length > 0 }
  for (const listener of listeners) {
    listener()
  }
}

export function clearAdminKey(): void {
  if (typeof window !== 'undefined') {
    try {
      window.sessionStorage.removeItem(SESSION_KEY)
      window.sessionStorage.removeItem(REMEMBER_KEY)
    } catch {
      // Ignore storage failures; the in-memory key is cleared below.
    }
  }
  snapshot = { key: '', remember: false }
  for (const listener of listeners) {
    listener()
  }
}
