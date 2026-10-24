import {
  useEffect,
  useId,
  useRef,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { X } from 'lucide-react'
import { cx } from '../../lib/cx'

export interface ModalProps {
  open: boolean
  title: ReactNode
  description?: ReactNode
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg'
  dismissible?: boolean
}

export function Modal({
  open,
  title,
  description,
  onClose,
  children,
  footer,
  size = 'md',
  dismissible = true,
}: ModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const descriptionId = useId()

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) {
      return
    }
    if (open && !dialog.open) {
      dialog.showModal()
      dialog.querySelector<HTMLElement>('[data-autofocus]')?.focus()
    }
    if (!open && dialog.open) {
      dialog.close()
    }
  }, [open])

  const handleBackdropClick = (event: MouseEvent<HTMLDialogElement>) => {
    if (event.target === dialogRef.current && dismissible) {
      onClose()
    }
  }

  return (
    <dialog
      ref={dialogRef}
      className={cx('dialog', `dialog--${size}`)}
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      onCancel={(event) => {
        event.preventDefault()
        if (dismissible) {
          onClose()
        }
      }}
      onClick={handleBackdropClick}
    >
      <header className="dialog__header">
        <div className="dialog__copy">
          <h2 className="dialog__title" id={titleId}>
            {title}
          </h2>
          {description ? (
            <p className="dialog__description" id={descriptionId}>
              {description}
            </p>
          ) : null}
        </div>
        <button
          type="button"
          className="dialog__close"
          onClick={onClose}
          disabled={!dismissible}
          aria-label="关闭对话框"
        >
          <X size={16} aria-hidden="true" />
        </button>
      </header>
      <div className="dialog__body">{children}</div>
      {footer ? <footer className="dialog__footer">{footer}</footer> : null}
    </dialog>
  )
}
