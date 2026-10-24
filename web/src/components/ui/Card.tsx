import type { ReactNode } from 'react'
import { cx } from '../../lib/cx'

export interface CardProps {
  children: ReactNode
  className?: string
}

export function Card({ children, className }: CardProps) {
  return <section className={cx('card', className)}>{children}</section>
}

export interface CardHeaderProps {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
}

export function CardHeader({ title, description, actions }: CardHeaderProps) {
  return (
    <header className="card__header">
      <div>
        <h2 className="card__title">{title}</h2>
        {description ? <p className="card__description">{description}</p> : null}
      </div>
      {actions ? <div className="row">{actions}</div> : null}
    </header>
  )
}

export interface CardBodyProps {
  children: ReactNode
  flush?: boolean
  className?: string
}

export function CardBody({ children, flush = false, className }: CardBodyProps) {
  return (
    <div className={cx('card__body', flush && 'card__body--flush', className)}>
      {children}
    </div>
  )
}
