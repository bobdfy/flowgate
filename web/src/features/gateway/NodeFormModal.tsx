import { useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type { Node } from '../../api/contracts/node'
import { getErrorMessage } from '../../lib/errorMessage'
import { useCreateNode, useUpdateNode } from './useNodes'
import { validateNodeForm, type NodeFormValues } from './validation'

export interface NodeFormModalProps {
  open: boolean
  serviceId: number
  serviceName: string
  node?: Node
  onClose: () => void
  onSaved: () => void
}

function valuesFromNode(node: Node | undefined): NodeFormValues {
  return {
    address: node?.address ?? '',
    weight: String(node?.weight ?? 1),
    enabled: node?.enabled ?? true,
  }
}

export function NodeFormModal({
  open,
  serviceId,
  serviceName,
  node,
  onClose,
  onSaved,
}: NodeFormModalProps) {
  const formId = useId()
  const addressId = useId()
  const weightId = useId()
  const statusId = useId()
  const createMutation = useCreateNode(serviceId)
  const updateMutation = useUpdateNode(serviceId)
  const [values, setValues] = useState<NodeFormValues>(() =>
    valuesFromNode(node),
  )
  const [validationError, setValidationError] = useState<string | null>(null)

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const result = validateNodeForm(values)
    if (!result.ok) {
      setValidationError(result.error)
      return
    }

    try {
      if (node) {
        await updateMutation.mutateAsync({ id: node.id, input: result.input })
      } else {
        await createMutation.mutateAsync(result.input)
      }
      onSaved()
    } catch {
      // Mutation state exposes the normalized API error below the form.
    }
  }

  return (
    <Modal
      open={open}
      size="md"
      title={node ? '编辑节点' : '新增节点'}
      description={`服务：${serviceName}（ID ${serviceId}）`}
      onClose={onClose}
      dismissible={!pending}
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
            {node ? '保存修改' : '新增节点'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        <div className="form-grid">
          <FormField
            label="节点地址"
            htmlFor={addressId}
            required
            fullWidth
            hint="必须是完整的 http/https URL，例如 http://10.0.0.8:8080。admin 会解析并拒绝内网 / 回环地址，除非该网段写进了 ALLOW_UPSTREAM_CIDRS 白名单。"
          >
            <input
              id={addressId}
              className="field__input field__input--mono"
              value={values.address}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  address: event.target.value,
                }))
              }
              maxLength={255}
              placeholder="http://127.0.0.1:8091"
              autoComplete="off"
              data-autofocus
              disabled={pending}
            />
          </FormField>

          <FormField
            label="权重"
            htmlFor={weightId}
            required
            hint="正整数；权重越大被轮询选中的比例越高。"
          >
            <input
              id={weightId}
              className="field__input"
              type="number"
              min={1}
              step={1}
              inputMode="numeric"
              value={values.weight}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  weight: event.target.value,
                }))
              }
              disabled={pending}
            />
          </FormField>

          <FormField label="状态" htmlFor={statusId}>
            <select
              id={statusId}
              className="field__input"
              value={values.enabled ? 'true' : 'false'}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  enabled: event.target.value === 'true',
                }))
              }
              disabled={pending}
            >
              <option value="true">启用</option>
              <option value="false">停用</option>
            </select>
          </FormField>
        </div>

        {!values.enabled ? (
          <div className="callout callout--warning">
            <span>
              停用节点不会进入发布快照。若服务下没有其他启用节点，指向该服务的启用路由将无法发布。
            </span>
          </div>
        ) : null}

        <div className="callout callout--info">
          <span>
            health_status 是持久化配置字段，不是 gateway 的实时健康检查结果。节点真实健康状态只保存在 gateway 内存中，管理 API 无法查询。
          </span>
        </div>

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
