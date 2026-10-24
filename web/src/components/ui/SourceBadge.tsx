import { cx } from '../../lib/cx'

export type SourceState = 'connected' | 'checking' | 'error' | 'unavailable'

export interface SourceBadgeProps {
  label: string
  state: SourceState
  title?: string
}

export function SourceBadge({ label, state, title }: SourceBadgeProps) {
  return (
    <span
      className={cx('source-badge', `source-badge--${state}`)}
      title={title}
      aria-label={title ? `${label}：${title}` : label}
    >
      <span className="source-badge__dot" aria-hidden="true" />
      <span className="source-badge__label">{label}</span>
    </span>
  )
}
