import type { ReactNode } from 'react'
import { cx } from '../../lib/cx'

export interface StatePanelProps {
  icon?: ReactNode
  title: string
  description?: ReactNode
  actions?: ReactNode
  tone?: 'neutral' | 'error' | 'unavailable'
}

export function StatePanel({
  icon,
  title,
  description,
  actions,
  tone = 'neutral',
}: StatePanelProps) {
  return (
    <div
      className={cx(
        'state-panel',
        tone === 'error' && 'state-panel--error',
        tone === 'unavailable' && 'state-panel--unavailable',
      )}
    >
      {icon ? <div className="state-panel__icon">{icon}</div> : null}
      <div className="state-panel__title">{title}</div>
      {description ? (
        <div className="state-panel__description">{description}</div>
      ) : null}
      {actions ? <div className="state-panel__actions">{actions}</div> : null}
    </div>
  )
}
