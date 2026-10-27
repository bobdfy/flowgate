import { createContext } from 'react'
import type { HealthResponse } from '../../api/contracts/common'
import type { ApiError } from '../../api/errors'

export type ConnectionStatus =
  | 'initializing'
  | 'connected'
  | 'unauthorized'
  | 'offline'

export interface ConnectionContextValue {
  status: ConnectionStatus
  isChecking: boolean
  error: ApiError | null
  health: HealthResponse | null
  lastCheckedAt: string | null
  keyConfigured: boolean
  rememberKey: boolean
  retry: () => Promise<void>
  authenticate: (key: string, remember: boolean) => Promise<void>
  clearKey: () => void
}

export const ConnectionContext = createContext<ConnectionContextValue | null>(
  null,
)
