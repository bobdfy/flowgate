import type { ReactNode } from 'react'
import { cx } from '../../lib/cx'

export interface FormFieldProps {
  label: string
  htmlFor: string
  hint?: ReactNode
  error?: string | null
  required?: boolean
  fullWidth?: boolean
  children: ReactNode
}

export function FormField({
  label,
  htmlFor,
  hint,
  error,
  required = false,
  fullWidth = false,
  children,
}: FormFieldProps) {
  return (
    <div className={cx('field', fullWidth && 'form-field--full')}>
      <label className="field__label" htmlFor={htmlFor}>
        {label}
        {required ? (
          <span className="field__required" aria-hidden="true">
            *
          </span>
        ) : null}
      </label>
      {children}
      {error ? (
        <div className="field__error" role="alert">
          {error}
        </div>
      ) : null}
      {hint ? <div className="field__hint">{hint}</div> : null}
    </div>
  )
}
