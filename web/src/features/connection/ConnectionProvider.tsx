import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  clearAdminKey,
  getAdminKeySnapshot,
  setAdminKey,
  subscribeAdminKey,
} from '../../api/adminKey'
import { getHealth } from '../../api/health'
import type { HealthResponse } from '../../api/contracts/common'
import { ApiError, isUnauthorizedError, toApiError } from '../../api/errors'
import { UNAUTHORIZED_EVENT } from '../../api/http'
import {
  ConnectionContext,
  type ConnectionContextValue,
  type ConnectionStatus,
} from './connectionContext'

export function ConnectionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const adminKey = useSyncExternalStore(
    subscribeAdminKey,
    getAdminKeySnapshot,
    getAdminKeySnapshot,
  )
  const [status, setStatus] = useState<ConnectionStatus>('initializing')
  const [isChecking, setIsChecking] = useState(true)
  const [error, setError] = useState<ApiError | null>(null)
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [lastCheckedAt, setLastCheckedAt] = useState<string | null>(null)
  const requestIdRef = useRef(0)

  const finishCheck = useCallback((response: HealthResponse) => {
    setStatus('connected')
    setError(null)
    setHealth(response)
    setLastCheckedAt(new Date().toISOString())
  }, [])

  const finishFailure = useCallback((apiError: ApiError) => {
    setStatus(
      isUnauthorizedError(apiError) ? 'unauthorized' : 'offline',
    )
    setError(apiError)
    setHealth(null)
    setLastCheckedAt(new Date().toISOString())
  }, [])

  const retry = useCallback(async () => {
    const requestId = ++requestIdRef.current
    setIsChecking(true)
    setError(null)
    try {
      const response = await getHealth()
      if (requestId !== requestIdRef.current) {
        return
      }
      finishCheck(response)
    } catch (caught) {
      const apiError = toApiError(caught)
      if (apiError.kind === 'canceled' || requestId !== requestIdRef.current) {
        return
      }
      finishFailure(apiError)
    } finally {
      if (requestId === requestIdRef.current) {
        setIsChecking(false)
      }
    }
  }, [finishCheck, finishFailure])

  const authenticate = useCallback(
    async (key: string, remember: boolean) => {
      const requestId = ++requestIdRef.current
      setIsChecking(true)
      setError(null)
      setAdminKey(key, remember)
      try {
        const response = await getHealth()
        if (requestId !== requestIdRef.current) {
          return
        }
        finishCheck(response)
      } catch (caught) {
        const apiError = toApiError(caught)
        if (isUnauthorizedError(apiError)) {
          clearAdminKey()
        }
        if (requestId === requestIdRef.current) {
          finishFailure(apiError)
        }
        throw apiError
      } finally {
        if (requestId === requestIdRef.current) {
          setIsChecking(false)
        }
      }
    },
    [finishCheck, finishFailure],
  )

  const clearKey = useCallback(() => {
    clearAdminKey()
    void retry()
  }, [retry])

  useEffect(() => {
    const requestId = ++requestIdRef.current
    let canceled = false

    getHealth()
      .then((response) => {
        if (!canceled && requestId === requestIdRef.current) {
          finishCheck(response)
        }
      })
      .catch((caught: unknown) => {
        const apiError = toApiError(caught)
        if (
          canceled ||
          apiError.kind === 'canceled' ||
          requestId !== requestIdRef.current
        ) {
          return
        }
        finishFailure(apiError)
      })
      .finally(() => {
        if (!canceled && requestId === requestIdRef.current) {
          setIsChecking(false)
        }
      })

    return () => {
      canceled = true
    }
  }, [finishCheck, finishFailure])

  useEffect(() => {
    const handleUnauthorized = () => {
      requestIdRef.current += 1
      setIsChecking(false)
      setStatus('unauthorized')
      setError(new ApiError('管理密钥无效或已过期', 'unauthorized', 401))
      setHealth(null)
      setLastCheckedAt(new Date().toISOString())
      queryClient.clear()
    }
    window.addEventListener(UNAUTHORIZED_EVENT, handleUnauthorized)
    return () => {
      window.removeEventListener(UNAUTHORIZED_EVENT, handleUnauthorized)
    }
  }, [queryClient])

  const value = useMemo<ConnectionContextValue>(
    () => ({
      status,
      isChecking,
      error,
      health,
      lastCheckedAt,
      keyConfigured: adminKey.key.length > 0,
      rememberKey: adminKey.remember,
      retry,
      authenticate,
      clearKey,
    }),
    [
      adminKey.key,
      adminKey.remember,
      authenticate,
      clearKey,
      error,
      health,
      isChecking,
      lastCheckedAt,
      retry,
      status,
    ],
  )

  return (
    <ConnectionContext.Provider value={value}>
      {children}
    </ConnectionContext.Provider>
  )
}
