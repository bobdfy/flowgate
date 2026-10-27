import type { ReactNode } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { queryClient } from '../api/queryClient'
import { ConnectionProvider } from '../features/connection/ConnectionProvider'

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <ConnectionProvider>{children}</ConnectionProvider>
    </QueryClientProvider>
  )
}
