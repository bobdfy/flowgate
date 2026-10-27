import type { ReactNode } from 'react'
import { Unplug } from 'lucide-react'
import { StatePanel } from './StatePanel'

export interface UnavailableStateProps {
  title: string
  description?: ReactNode
  requirements?: string[]
  actions?: ReactNode
}

export function UnavailableState({
  title,
  description,
  requirements,
  actions,
}: UnavailableStateProps) {
  return (
    <StatePanel
      tone="unavailable"
      icon={<Unplug size={18} strokeWidth={1.8} />}
      title={title}
      description={
        <>
          {description}
          {requirements?.length ? (
            <ul className="api-list" style={{ marginTop: 12, textAlign: 'left' }}>
              {requirements.map((item) => (
                <li key={item}>
                  <code>{item}</code>
                </li>
              ))}
            </ul>
          ) : null}
        </>
      }
      actions={actions}
    />
  )
}
