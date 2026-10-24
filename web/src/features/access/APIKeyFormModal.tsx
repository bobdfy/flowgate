import { useEffect, useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type {
  APIKey,
  APIKeyStatus,
  CreateAPIKeyInput,
  CreateAPIKeyResponse,
} from '../../api/contracts/apiKey'
import { getErrorMessage } from '../../lib/errorMessage'
import {
  fromDateTimeLocalValue,
  toDateTimeLocalValue,
} from '../../lib/time'
import { useCreateAPIKey, useUpdateAPIKey } from './useAPIKeys'

export interface APIKeyFormModalProps {
  open: boolean
  tenantId: number
  tenantName: string
  tenantDisabled?: boolean
  apiKey?: APIKey
  onClose: () => void
  onCreated: (created: CreateAPIKeyResponse) => void
  onSaved: () => void
}

export function APIKeyFormModal({
  open,
  tenantId,
  tenantName,
  tenantDisabled = false,
  apiKey,
  onClose,
  onCreated,
  onSaved,
}: APIKeyFormModalProps) {
  const formId = useId()
  const nameInputId = useId()
  const statusInputId = useId()
  const qpsInputId = useId()
  const expiresInputId = useId()
  const createMutation = useCreateAPIKey(tenantId)
  const updateMutation = useUpdateAPIKey(tenantId)
  const [name, setName] = useState(apiKey?.name ?? '')
  const [status, setStatus] = useState<APIKeyStatus>(
    apiKey?.status ?? 'active',
  )
  const [qps, setQps] = useState(String(apiKey?.qps_limit ?? 100))
  const [expires, setExpires] = useState(
    toDateTimeLocalValue(apiKey?.expires_at),
  )
  const [validationError, setValidationError] = useState<string | null>(null)
  const [now, setNow] = useState<number | null>(null)

  useEffect(() => {
    const updateNow = () => setNow(Date.now())
    const initialTimer = window.setTimeout(updateNow, 0)
    const timer = window.setInterval(updateNow, 30_000)
    return () => {
      window.clearTimeout(initialTimer)
      window.clearInterval(timer)
    }
  }, [])

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error
  const expiresAt = fromDateTimeLocalValue(expires)
  const expiresInPast =
    now !== null &&
    expiresAt !== null &&
    Date.parse(expiresAt) <= now

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const trimmedName = name.trim()
    if (!trimmedName) {
      setValidationError('Key 名称不能为空。')
      return
    }
    if (trimmedName.length > 255) {
      setValidationError('Key 名称不能超过 255 个字符。')
      return
    }
    if (expires.trim() && expiresAt === null) {
      setValidationError('过期时间格式无效。')
      return
    }

    const qpsValue = Number(qps)
    if (!Number.isInteger(qpsValue) || qpsValue <= 0) {
      setValidationError('Key QPS 配额必须是大于 0 的整数。')
      return
    }

    const input: CreateAPIKeyInput = {
      name: trimmedName,
      status,
      qps_limit: qpsValue,
      expires_at: expiresAt,
    }

    try {
      if (apiKey) {
        await updateMutation.mutateAsync({ id: apiKey.id, input })
        onSaved()
      } else {
        const created = await createMutation.mutateAsync(input)
        onCreated(created)
      }
    } catch {
      // Mutation state exposes the normalized API error below the form.
    }
  }

  return (
    <Modal
      open={open}
      title={apiKey ? '编辑 API Key' : '创建 API Key'}
      description={
        apiKey
          ? `更新 ${tenantName} 的 Key 属性。已签发的明文无法重新查看。`
          : `为 ${tenantName} 签发新的 API Key。明文只在创建成功后显示一次。`
      }
      onClose={onClose}
      dismissible={!pending}
      size="md"
      footer={
        <>
          <Button onClick={onClose} disabled={pending}>
            取消
          </Button>
          <Button
            type="submit"
            form={formId}
            variant="primary"
            loading={pending}
          >
            {apiKey ? '保存修改' : '创建 API Key'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        {tenantDisabled ? (
          <div className="callout callout--warning">
            <span>
              租户当前已停用。新建或修改的 Key 在租户重新启用前无法通过鉴权。
            </span>
          </div>
        ) : null}

        <div className="form-grid">
          <FormField
            label="Key 名称"
            htmlFor={nameInputId}
            required
            fullWidth
            hint="用于区分环境或用途，例如 prod / staging。"
          >
            <input
              id={nameInputId}
              className="field__input"
              value={name}
              onChange={(event) => setName(event.target.value)}
              maxLength={255}
              autoFocus
              data-autofocus
              autoComplete="off"
              disabled={pending}
            />
          </FormField>

          <FormField label="状态" htmlFor={statusInputId} required>
            <select
              id={statusInputId}
              className="field__input"
              value={status}
              onChange={(event) =>
                setStatus(event.target.value as APIKeyStatus)
              }
              disabled={pending}
            >
              <option value="active">启用</option>
              <option value="disabled">停用</option>
            </select>
          </FormField>

          <FormField
            label="Key QPS 配额"
            htmlFor={qpsInputId}
            required
            hint="与租户 QPS 同时校验，取更严格的一层。"
          >
            <input
              id={qpsInputId}
              className="field__input"
              type="number"
              min={1}
              step={1}
              inputMode="numeric"
              value={qps}
              onChange={(event) => setQps(event.target.value)}
              disabled={pending}
            />
          </FormField>

          <FormField
            label="过期时间"
            htmlFor={expiresInputId}
            fullWidth
            hint="按本地时区填写；留空表示永不过期。"
          >
            <input
              id={expiresInputId}
              className="field__input"
              type="datetime-local"
              value={expires}
              onChange={(event) => setExpires(event.target.value)}
              disabled={pending}
            />
          </FormField>
        </div>

        {expiresInPast ? (
          <div className="callout callout--warning">
            <span>
              过期时间早于当前时间，保存后该 Key 将立即无法通过鉴权。
            </span>
          </div>
        ) : null}

        {status === 'disabled' ? (
          <div className="callout callout--warning">
            <span>Key 停用后，使用它的客户端会立即被 gateway 拒绝。</span>
          </div>
        ) : null}

        {validationError ? (
          <div className="callout callout--danger" role="alert">
            <span>{validationError}</span>
          </div>
        ) : null}

        {mutationError ? (
          <div className="callout callout--danger" role="alert">
            <span>{getErrorMessage(mutationError)}</span>
          </div>
        ) : null}

        {!apiKey ? (
          <div className="callout callout--info">
            <span>
              创建成功后只显示一次明文密钥。关闭弹窗后无法找回，只能重新创建。
            </span>
          </div>
        ) : null}
      </form>
    </Modal>
  )
}
