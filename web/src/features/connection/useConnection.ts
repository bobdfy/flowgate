import { use } from 'react'
import { ConnectionContext } from './connectionContext'

export function useConnection() {
  const context = use(ConnectionContext)
  if (!context) {
    throw new Error('useConnection 必须在 ConnectionProvider 内使用')
  }
  return context
}
