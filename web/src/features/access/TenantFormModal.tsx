import { useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type {
  Tenant,
  TenantStatus,
  TenantWriteInput,
} from '../../api/contracts/tenant'
import { getErrorMessage } from '../../lib/errorMessage'
import { useCreateTenant, useUpdateTenant } from './useTenants'

export interface TenantFormModalProps {
  open: boolean
  tenant?: Tenant
  existingNames: string[]
  onClose: () => void
  onSaved: (tenantId: number) => void
}

export function TenantFormModal({
  open,
  tenant,
  existingNames,
  onClose,
  onSaved,
}: TenantFormModalProps) {
  const formId = useId()
  const nameInputId = useId()
  const statusInputId = useId()
  const qpsInputId = useId()
  const tpmInputId = useId()
  const createMutation = useCreateTenant()
  const updateMutation = useUpdateTenant()
  const [name, setName] = useState(tenant?.name ?? '')
  const [status, setStatus] = useState<TenantStatus>(tenant?.status ?? 'active')
  const [qps, setQps] = useState(String(tenant?.qps_limit ?? 100))
  const [tpm, setTpm] = useState(String(tenant?.tpm_limit ?? 100000))
  const [validationError, setValidationError] = useState<string | null>(null)

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const trimmedName = name.trim()
    if (!trimmedName) {
      setValidationError('租户名称不能为空。')
      return
    }
    if (trimmedName.length > 255) {
      setValidationError('租户名称不能超过 255 个字符。')
      return
    }
    if (
      existingNames.some(
        (existing) => existing === trimmedName && existing !== tenant?.name,
      )
    ) {
      setValidationError('租户名称已存在，请更换名称。')
      return
    }

    const qpsValue = Number(qps)
    const tpmValue = Number(tpm)
    if (!Number.isInteger(qpsValue) || qpsValue <= 0) {
      setValidationError('QPS 配额必须是大于 0 的整数。')
      return
    }
    if (!Number.isInteger(tpmValue) || tpmValue <= 0) {
      setValidationError('TPM 配额必须是大于 0 的整数。')
      return
    }

    const input: TenantWriteInput = {
      name: trimmedName,
      status,
      qps_limit: qpsValue,
      tpm_limit: tpmValue,
    }

    try {
      const saved = tenant
        ? await updateMutation.mutateAsync({ id: tenant.id, input })
        : await createMutation.mutateAsync(input)
      onSaved(saved.id)
    } catch {
      // Mutation state exposes the normalized API error below the form.
    }
  }

  return (
    <Modal
      open={open}
      title={tenant ? '编辑租户' : '新建租户'}
      description={
        tenant
          ? '更新租户名称、状态与配额。停用租户会使其名下所有 API Key 失效。'
          : '创建租户后，可在详情页为其签发 API Key。'
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
            {tenant ? '保存修改' : '创建租户'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        <div className="form-grid">
          <FormField
            label="租户名称"
            htmlFor={nameInputId}
            required
            fullWidth
            hint="名称需唯一，最多 255 个字符。"
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
                setStatus(event.target.value as TenantStatus)
              }
              disabled={pending}
            >
              <option value="active">启用</option>
              <option value="disabled">停用</option>
            </select>
          </FormField>

          <FormField
            label="租户 QPS 配额"
            htmlFor={qpsInputId}
            required
            hint="每秒请求数；租户级与 Key 级分别校验。"
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
            label="租户 TPM 配额"
            htmlFor={tpmInputId}
            required
            fullWidth
            hint="每个 TPM 窗口允许的 token 总数；窗口长度在「租户与 API Key」页顶部配置，租户详情页可查看当窗已用与剩余。"
          >
            <input
              id={tpmInputId}
              className="field__input"
              type="number"
              min={1}
              step={1}
              inputMode="numeric"
              value={tpm}
              onChange={(event) => setTpm(event.target.value)}
              disabled={pending}
            />
          </FormField>
        </div>

        {status === 'disabled' ? (
          <div className="callout callout--warning">
            <span>
              停用后，该租户名下所有 API Key 会立即无法通过 gateway 鉴权，直到租户重新启用。
            </span>
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
      </form>
    </Modal>
  )
}
