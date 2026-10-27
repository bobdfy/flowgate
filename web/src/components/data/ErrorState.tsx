import type { ReactNode } from 'react'
import { CircleAlert } from 'lucide-react'
import { Button } from '../ui/Button'
import { getErrorMessage } from '../../lib/errorMessage'
import { StatePanel } from './StatePanel'

export interface ErrorStateProps {
  title?: string
  error: unknown
  onRetry?: () => void
  actions?: ReactNode
}

export function ErrorState({
  title = '加载失败',
  error,
  onRetry,
  actions,
}: ErrorStateProps) {
  return (
    <StatePanel
      tone="error"
      icon={<CircleAlert size={18} strokeWidth={1.8} />}
      title={title}
      description={getErrorMessage(error)}
      actions={
        actions ?? (
          onRetry ? (
            <Button size="sm" onClick={onRetry}>
              重试
            </Button>
          ) : null
        )
      }
    />
  )
}
