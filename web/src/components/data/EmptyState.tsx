import type { ReactNode } from 'react'
import { Inbox } from 'lucide-react'
import { StatePanel } from './StatePanel'

export interface EmptyStateProps {
  title: string
  description?: ReactNode
  actions?: ReactNode
}

export function EmptyState({ title, description, actions }: EmptyStateProps) {
  return (
    <StatePanel
      icon={<Inbox size={18} strokeWidth={1.8} />}
      title={title}
      description={description}
      actions={actions}
    />
  )
}
