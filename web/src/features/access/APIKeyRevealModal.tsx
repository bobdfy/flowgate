import { useState } from 'react'
import { Copy, Eye, EyeOff } from 'lucide-react'
import { Badge } from '../../components/ui/Badge'
import { Button } from '../../components/ui/Button'
import { Modal } from '../../components/ui/Modal'
import type { CreateAPIKeyResponse } from '../../api/contracts/apiKey'
import { formatDateTime } from '../../lib/time'
import { apiKeyStatusMeta } from './status'

export interface APIKeyRevealModalProps {
  createdKey: CreateAPIKeyResponse | null
  tenantName: string
  onClose: () => void
}

export function APIKeyRevealModal({
  createdKey,
  tenantName,
  onClose,
}: APIKeyRevealModalProps) {
  if (!createdKey) {
    return null
  }
  return (
    <APIKeyRevealContent
      key={createdKey.id}
      createdKey={createdKey}
      tenantName={tenantName}
      onClose={onClose}
    />
  )
}

interface APIKeyRevealContentProps {
  createdKey: CreateAPIKeyResponse
  tenantName: string
  onClose: () => void
}

function APIKeyRevealContent({
  createdKey,
  tenantName,
  onClose,
}: APIKeyRevealContentProps) {
  const [revealed, setRevealed] = useState(false)
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)
  const statusMeta = apiKeyStatusMeta(createdKey)
  const maskedKey = `${createdKey.key.slice(0, 3)}${'•'.repeat(16)}${createdKey.key.slice(-4)}`

  const handleCopy = async () => {
    setCopyError(null)
    try {
      await navigator.clipboard.writeText(createdKey.key)
      setCopied(true)
    } catch {
      setCopyError('复制失败，请显示密钥后手动复制。')
    }
  }

  return (
    <Modal
      open
      size="md"
      title="API Key 已创建"
      description="请立即把密钥保存到安全位置，关闭后无法再次查看。"
      onClose={onClose}
      dismissible={false}
      footer={
        <Button variant="primary" onClick={onClose}>
          我已保存，关闭
        </Button>
      }
    >
      <div className="callout callout--warning">
        <span>
          这是唯一一次显示明文密钥。控制台不会把它写入 localStorage、URL 或日志。
        </span>
      </div>

      <div className="key-reveal">
        <div className="key-reveal__label">API Key</div>
        <code className="key-reveal__value">
          {revealed ? createdKey.key : maskedKey}
        </code>
        <div className="key-reveal__actions">
          <Button
            size="sm"
            icon={
              revealed ? (
                <EyeOff size={14} aria-hidden="true" />
              ) : (
                <Eye size={14} aria-hidden="true" />
              )
            }
            onClick={() => setRevealed((current) => !current)}
          >
            {revealed ? '隐藏密钥' : '显示密钥'}
          </Button>
          <Button
            size="sm"
            icon={<Copy size={14} aria-hidden="true" />}
            onClick={() => void handleCopy()}
          >
            复制完整密钥
          </Button>
        </div>
        {copyError ? (
          <div className="field__error" role="alert">
            {copyError}
          </div>
        ) : null}
        {copied ? (
          <div className="field__hint" role="status">
            已复制到剪贴板。
          </div>
        ) : null}
      </div>

      <dl className="kv-list" style={{ marginTop: 18 }}>
        <dt>名称</dt>
        <dd>{createdKey.name}</dd>
        <dt>租户</dt>
        <dd>{tenantName}</dd>
        <dt>状态</dt>
        <dd>
          <Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>
        </dd>
        <dt>过期时间</dt>
        <dd>
          {createdKey.expires_at
            ? formatDateTime(createdKey.expires_at)
            : '永不过期'}
        </dd>
        <dt>创建时间</dt>
        <dd>{formatDateTime(createdKey.created_at)}</dd>
      </dl>
    </Modal>
  )
}
