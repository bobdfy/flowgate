import { useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type { Service } from '../../api/contracts/service'
import { getErrorMessage } from '../../lib/errorMessage'
import { useCreateService, useUpdateService } from './useServices'
import {
  validateServiceForm,
  type ServiceFormValues,
} from './validation'

export interface ServiceFormModalProps {
  open: boolean
  service?: Service
  existingNames: string[]
  onClose: () => void
  onSaved: (serviceId: number) => void
}

const defaultValues: ServiceFormValues = {
  name: '',
  protocol: 'http',
  enabled: true,
  connectTimeoutMs: '3000',
  requestTimeoutMs: '10000',
  responseHeaderTimeoutMs: '5000',
  maxRetries: '0',
  retryOnStatus: '',
  retryBackoffMs: '100',
  cbFailureThreshold: '5',
  cbCooldownMs: '10000',
  cbHalfOpenLimit: '1',
  maxConcurrency: '0',
  queueTimeoutMs: '0',
  overloadStrategy: 'fail-fast',
}

function valuesFromService(service: Service | undefined): ServiceFormValues {
  if (!service) {
    return { ...defaultValues }
  }
  return {
    name: service.name,
    protocol: service.protocol,
    enabled: service.enabled,
    connectTimeoutMs: String(service.connect_timeout_ms),
    requestTimeoutMs: String(service.request_timeout_ms),
    responseHeaderTimeoutMs: String(service.response_header_timeout_ms),
    maxRetries: String(service.max_retries),
    retryOnStatus: service.retry_on_status,
    retryBackoffMs: String(service.retry_backoff_ms),
    cbFailureThreshold: String(service.cb_failure_threshold),
    cbCooldownMs: String(service.cb_cooldown_ms),
    cbHalfOpenLimit: String(service.cb_half_open_limit),
    maxConcurrency: String(service.max_concurrency),
    queueTimeoutMs: String(service.queue_timeout_ms),
    overloadStrategy: service.overload_strategy,
  }
}

export function ServiceFormModal({
  open,
  service,
  existingNames,
  onClose,
  onSaved,
}: ServiceFormModalProps) {
  const formId = useId()
  const nameId = useId()
  const protocolId = useId()
  const statusId = useId()
  const connectTimeoutId = useId()
  const requestTimeoutId = useId()
  const responseHeaderTimeoutId = useId()
  const maxRetriesId = useId()
  const retryOnStatusId = useId()
  const retryBackoffId = useId()
  const cbFailureId = useId()
  const cbCooldownId = useId()
  const cbHalfOpenId = useId()
  const maxConcurrencyId = useId()
  const queueTimeoutId = useId()
  const overloadStrategyId = useId()

  const createMutation = useCreateService()
  const updateMutation = useUpdateService()
  const [values, setValues] = useState<ServiceFormValues>(() =>
    valuesFromService(service),
  )
  const [validationError, setValidationError] = useState<string | null>(null)

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error

  const setValue = <K extends keyof ServiceFormValues>(
    key: K,
    value: ServiceFormValues[K],
  ) => {
    setValues((current) => ({ ...current, [key]: value }))
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const result = validateServiceForm(values, service, existingNames)
    if (!result.ok) {
      setValidationError(result.error)
      return
    }

    try {
      const saved = service
        ? await updateMutation.mutateAsync({
            id: service.id,
            input: result.input,
          })
        : await createMutation.mutateAsync(result.input)
      onSaved(saved.id)
    } catch {
      // Mutation state exposes the normalized API error below the form.
    }
  }

  const knownProtocol =
    values.protocol === 'http' || values.protocol === 'grpc'
  const knownOverloadStrategy =
    values.overloadStrategy === 'fail-fast' ||
    values.overloadStrategy === 'wait' ||
    values.overloadStrategy === 'drop'

  return (
    <Modal
      open={open}
      size="lg"
      title={service ? '编辑服务' : '新建服务'}
      description="保存只写入管理面配置；需要在发布中心发布后，gateway 才会加载新快照。"
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
            {service ? '保存修改' : '创建服务'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        <section className="form-section">
          <h3 className="form-section__title">基本信息</h3>
          <div className="form-grid">
            <FormField
              label="服务名称"
              htmlFor={nameId}
              required
              fullWidth
              hint="全局唯一，最多 255 个字符。"
            >
              <input
                id={nameId}
                className="field__input"
                value={values.name}
                onChange={(event) => setValue('name', event.target.value)}
                maxLength={255}
                autoComplete="off"
                data-autofocus
                disabled={pending}
              />
            </FormField>

            <FormField
              label="协议"
              htmlFor={protocolId}
              hint="协议字段当前只作为配置记录，gateway 转发实现未按它分流。"
            >
              <select
                id={protocolId}
                className="field__input"
                value={values.protocol}
                onChange={(event) => setValue('protocol', event.target.value)}
                disabled={pending}
              >
                {knownProtocol ? null : (
                  <option value={values.protocol}>
                    {values.protocol}（当前值）
                  </option>
                )}
                <option value="http">HTTP</option>
                <option value="grpc">gRPC</option>
              </select>
            </FormField>

            <FormField label="状态" htmlFor={statusId}>
              <select
                id={statusId}
                className="field__input"
                value={values.enabled ? 'true' : 'false'}
                onChange={(event) =>
                  setValue('enabled', event.target.value === 'true')
                }
                disabled={pending}
              >
                <option value="true">启用</option>
                <option value="false">停用</option>
              </select>
            </FormField>
          </div>
        </section>

        <section className="form-section">
          <h3 className="form-section__title">超时</h3>
          <div className="form-grid">
            <FormField
              label="建连超时（毫秒）"
              htmlFor={connectTimeoutId}
              required
            >
              <input
                id={connectTimeoutId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.connectTimeoutMs}
                onChange={(event) =>
                  setValue('connectTimeoutMs', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="请求超时（毫秒）"
              htmlFor={requestTimeoutId}
              required
            >
              <input
                id={requestTimeoutId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.requestTimeoutMs}
                onChange={(event) =>
                  setValue('requestTimeoutMs', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="响应头超时（毫秒）"
              htmlFor={responseHeaderTimeoutId}
              required
              fullWidth
              hint="等待上游响应头的最长时间。"
            >
              <input
                id={responseHeaderTimeoutId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.responseHeaderTimeoutMs}
                onChange={(event) =>
                  setValue('responseHeaderTimeoutMs', event.target.value)
                }
                disabled={pending}
              />
            </FormField>
          </div>
        </section>

        <section className="form-section">
          <h3 className="form-section__title">重试</h3>
          <div className="form-grid">
            <FormField
              label="最大重试次数"
              htmlFor={maxRetriesId}
              required
              hint="0 表示不重试。"
            >
              <input
                id={maxRetriesId}
                className="field__input"
                type="number"
                min={0}
                step={1}
                inputMode="numeric"
                value={values.maxRetries}
                onChange={(event) =>
                  setValue('maxRetries', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="重试退避（毫秒）"
              htmlFor={retryBackoffId}
              required
            >
              <input
                id={retryBackoffId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.retryBackoffMs}
                onChange={(event) =>
                  setValue('retryBackoffMs', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="重试状态码"
              htmlFor={retryOnStatusId}
              fullWidth
              hint="逗号分隔，例如 502,503,504；留空表示只重试传输错误。"
            >
              <input
                id={retryOnStatusId}
                className="field__input field__input--mono"
                value={values.retryOnStatus}
                onChange={(event) =>
                  setValue('retryOnStatus', event.target.value)
                }
                placeholder="502,503,504"
                autoComplete="off"
                disabled={pending}
              />
            </FormField>
          </div>
          <p className="form-section__hint">
            只有配置了重试次数且响应状态命中列表（或发生传输错误）时才会重试。
          </p>
        </section>

        <section className="form-section">
          <h3 className="form-section__title">熔断</h3>
          <div className="form-grid">
            <FormField
              label="失败阈值"
              htmlFor={cbFailureId}
              required
              hint="连续失败达到该次数后打开熔断。"
            >
              <input
                id={cbFailureId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.cbFailureThreshold}
                onChange={(event) =>
                  setValue('cbFailureThreshold', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="冷却时间（毫秒）"
              htmlFor={cbCooldownId}
              required
            >
              <input
                id={cbCooldownId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.cbCooldownMs}
                onChange={(event) =>
                  setValue('cbCooldownMs', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="半开探测上限"
              htmlFor={cbHalfOpenId}
              required
              fullWidth
              hint="冷却结束后允许放行的探测请求数。"
            >
              <input
                id={cbHalfOpenId}
                className="field__input"
                type="number"
                min={1}
                step={1}
                inputMode="numeric"
                value={values.cbHalfOpenLimit}
                onChange={(event) =>
                  setValue('cbHalfOpenLimit', event.target.value)
                }
                disabled={pending}
              />
            </FormField>
          </div>
        </section>

        <section className="form-section">
          <h3 className="form-section__title">并发保护</h3>
          <div className="form-grid">
            <FormField
              label="服务级最大并发"
              htmlFor={maxConcurrencyId}
              required
              hint="0 表示不限制。"
            >
              <input
                id={maxConcurrencyId}
                className="field__input"
                type="number"
                min={0}
                step={1}
                inputMode="numeric"
                value={values.maxConcurrency}
                onChange={(event) =>
                  setValue('maxConcurrency', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="排队超时（毫秒）"
              htmlFor={queueTimeoutId}
              required
              hint="仅 wait / drop 策略使用；0 表示不等待。"
            >
              <input
                id={queueTimeoutId}
                className="field__input"
                type="number"
                min={0}
                step={1}
                inputMode="numeric"
                value={values.queueTimeoutMs}
                onChange={(event) =>
                  setValue('queueTimeoutMs', event.target.value)
                }
                disabled={pending}
              />
            </FormField>

            <FormField
              label="过载策略"
              htmlFor={overloadStrategyId}
              fullWidth
              hint="并发达到上限后的处置方式；未知值在 gateway 中按快速失败处理。"
            >
              <select
                id={overloadStrategyId}
                className="field__input"
                value={values.overloadStrategy}
                onChange={(event) =>
                  setValue('overloadStrategy', event.target.value)
                }
                disabled={pending}
              >
                {knownOverloadStrategy ? null : (
                  <option value={values.overloadStrategy}>
                    {values.overloadStrategy}（当前值）
                  </option>
                )}
                <option value="fail-fast">快速失败（fail-fast）</option>
                <option value="wait">排队等待（wait）</option>
                <option value="drop">排队丢弃（drop）</option>
              </select>
            </FormField>
          </div>
        </section>

        {!values.enabled ? (
          <div className="callout callout--warning">
            <span>
              停用服务不会进入发布快照。指向它的启用路由会因校验失败而无法发布。
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
