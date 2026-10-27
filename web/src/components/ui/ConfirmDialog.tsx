import type { ReactNode } from 'react'
import { Button } from './Button'
import { Modal } from './Modal'

export interface ConfirmDialogProps {
  open: boolean
  title: string
  description: ReactNode
  confirmLabel: string
  onConfirm: () => void
  onClose: () => void
  loading?: boolean
  error?: string | null
  tone?: 'primary' | 'danger'
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  onConfirm,
  onClose,
  loading = false,
  error,
  tone = 'primary',
}: ConfirmDialogProps) {
  return (
    <Modal
      open={open}
      size="sm"
      title={title}
      onClose={onClose}
      dismissible={!loading}
      footer={
        <>
          <Button onClick={onClose} disabled={loading}>
            取消
          </Button>
          <Button
            variant={tone === 'danger' ? 'danger' : 'primary'}
            loading={loading}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div className="confirm__text">{description}</div>
      {error ? (
        <div className="callout callout--danger" style={{ marginTop: 16 }}>
          <span>{error}</span>
        </div>
      ) : null}
    </Modal>
  )
}
