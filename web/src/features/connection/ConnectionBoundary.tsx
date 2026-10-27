import { Outlet } from 'react-router-dom'
import { AdminKeyGate } from './AdminKeyGate'
import { BootScreen } from './BootScreen'
import { ConnectionErrorPage } from './ConnectionErrorPage'
import { useConnection } from './useConnection'

export function ConnectionBoundary() {
  const { status } = useConnection()

  if (status === 'initializing') {
    return <BootScreen />
  }
  if (status === 'unauthorized') {
    return <AdminKeyGate />
  }
  if (status === 'offline') {
    return <ConnectionErrorPage />
  }
  return <Outlet />
}
