import type { BadgeTone } from '../../components/ui/Badge'
import type { SourceState } from '../../components/ui/SourceBadge'
import type { ConnectionStatus } from './connectionContext'

interface ConnectionStatusMeta {
  label: string
  footerLabel: string
  badgeTone: BadgeTone
  sourceState: SourceState
  sidebarDot: 'connected' | 'degraded' | 'offline'
}

export const connectionStatusMeta: Record<
  ConnectionStatus,
  ConnectionStatusMeta
> = {
  initializing: {
    label: '检查中',
    footerLabel: '正在检查管理面',
    badgeTone: 'info',
    sourceState: 'checking',
    sidebarDot: 'degraded',
  },
  connected: {
    label: '已连接',
    footerLabel: '管理面已连接',
    badgeTone: 'success',
    sourceState: 'connected',
    sidebarDot: 'connected',
  },
  unauthorized: {
    label: '需要密钥',
    footerLabel: '管理面需要密钥',
    badgeTone: 'warning',
    sourceState: 'error',
    sidebarDot: 'degraded',
  },
  offline: {
    label: '不可达',
    footerLabel: '管理面不可达',
    badgeTone: 'danger',
    sourceState: 'error',
    sidebarDot: 'offline',
  },
}
