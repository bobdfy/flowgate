import { useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type { Route } from '../../api/contracts/route'
import type { Service } from '../../api/contracts/service'
import { getErrorMessage } from '../../lib/errorMessage'
import { useCreateRoute, useUpdateRoute } from './useRoutes'
import { validateRouteForm, type RouteFormValues } from './validation'

export interface RouteFormModalProps {
  open: boolean
  services: Service[]
  route?: Route
  defaultServiceId?: number
  onClose: () => void
  onSaved: () => void
}

function valuesFromRoute(
  route: Route | undefined,
  services: Service[],
  defaultServiceId: number | undefined,
): RouteFormValues {
  const initialServiceId =
    route?.service_id ?? defaultServiceId ?? services[0]?.id
  return {
    serviceId: initialServiceId ? String(initialServiceId) : '',
    name: route?.name ?? '',
    host: route?.host ?? '',
    pathPattern: route?.path_pattern ?? '',
    pathMatchType: route?.path_match_type ?? 'prefix',
    methods: route?.methods ?? '',
    enabled: route?.enabled ?? true,
    maxConcurrency: String(route?.max_concurrency ?? 0),
  }
}

export function RouteFormModal({
  open,
  services,
  route,
  defaultServiceId,
  onClose,
  onSaved,
}: RouteFormModalProps) {
  const formId = useId()
  const serviceId = useId()
  const nameId = useId()
  const hostId = useId()
  const pathId = useId()
  const matchTypeId = useId()
  const methodsId = useId()
  const statusId = useId()
  const maxConcurrencyId = useId()

  const createMutation = useCreateRoute()
  const updateMutation = useUpdateRoute()
  const [values, setValues] = useState<RouteFormValues>(() =>
    valuesFromRoute(route, services, defaultServiceId),
  )
  const [validationError, setValidationError] = useState<string | null>(null)

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error

  const selectedServiceId = Number(values.serviceId)
  const selectedService = services.find(
    (service) => service.id === selectedServiceId,
  )
  const knownMatchType =
    values.pathMatchType === 'prefix' || values.pathMatchType === 'exact'

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const result = validateRouteForm(values)
    if (!result.ok) {
      setValidationError(result.error)
      return
    }

    try {
      if (route) {
        await updateMutation.mutateAsync({ id: route.id, input: result.input })
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
      size="lg"
      title={route ? '编辑路由' : '新建路由'}
      description="保存只写入管理面配置；发布后会生成新的路由快照。"
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
            {route ? '保存修改' : '创建路由'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        <div className="form-grid">
          <FormField
            label="目标服务"
            htmlFor={serviceId}
            required
            hint="路由只会转发到所选服务的启用节点。"
          >
            <select
              id={serviceId}
              className="field__input"
              value={values.serviceId}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  serviceId: event.target.value,
                }))
              }
              disabled={pending || services.length === 0}
            >
              {services.length === 0 ? (
                <option value="">没有可用服务</option>
              ) : null}
              {services.map((service) => (
                <option key={service.id} value={String(service.id)}>
                  {service.name}（ID {service.id}
                  {service.enabled ? '' : '，已停用'}）
                </option>
              ))}
            </select>
          </FormField>

          <FormField
            label="路由名称"
            htmlFor={nameId}
            required
            hint="仅用于识别，不参与匹配。"
          >
            <input
              id={nameId}
              className="field__input"
              value={values.name}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  name: event.target.value,
                }))
              }
              maxLength={255}
              autoComplete="off"
              data-autofocus
              disabled={pending}
            />
          </FormField>

          <FormField
            label="Host"
            htmlFor={hostId}
            hint="可留空表示匹配任意 Host；填写后按请求 Host 精确比较（可含端口）。"
          >
            <input
              id={hostId}
              className="field__input field__input--mono"
              value={values.host}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  host: event.target.value,
                }))
              }
              maxLength={255}
              placeholder="api.example.com"
              autoComplete="off"
              disabled={pending}
            />
          </FormField>

          <FormField
            label="路径模式"
            htmlFor={pathId}
            required
            hint="以 / 开头。prefix 使用字符串前缀匹配，exact 要求路径完全相同；不支持通配符。"
          >
            <input
              id={pathId}
              className="field__input field__input--mono"
              value={values.pathPattern}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  pathPattern: event.target.value,
                }))
              }
              maxLength={255}
              placeholder="/api/"
              autoComplete="off"
              disabled={pending}
            />
          </FormField>

          <FormField
            label="匹配类型"
            htmlFor={matchTypeId}
            fullWidth
            hint="gateway 只识别 exact；其他值按前缀匹配处理，因此这里只提供两种。"
          >
            <select
              id={matchTypeId}
              className="field__input"
              value={values.pathMatchType}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  pathMatchType: event.target.value,
                }))
              }
              disabled={pending}
            >
              {knownMatchType ? null : (
                <option value={values.pathMatchType}>
                  {values.pathMatchType}（当前值，按前缀匹配）
                </option>
              )}
              <option value="prefix">前缀匹配（prefix）</option>
              <option value="exact">精确匹配（exact）</option>
            </select>
          </FormField>

          <FormField
            label="允许的方法"
            htmlFor={methodsId}
            fullWidth
            hint="逗号分隔，例如 GET,POST；留空表示不限制方法。保存时统一转为大写。"
          >
            <input
              id={methodsId}
              className="field__input field__input--mono"
              value={values.methods}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  methods: event.target.value,
                }))
              }
              maxLength={255}
              placeholder="GET,POST,PUT,DELETE"
              autoComplete="off"
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

          <FormField
            label="路由级最大并发"
            htmlFor={maxConcurrencyId}
            hint="0 表示不限制；超过上限时按快速失败拒绝。"
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
                setValues((current) => ({
                  ...current,
                  maxConcurrency: event.target.value,
                }))
              }
              disabled={pending}
            />
          </FormField>
        </div>

        {selectedService && !selectedService.enabled ? (
          <div className="callout callout--warning">
            <span>
              目标服务已停用。启用中的路由必须指向启用服务且有至少一个启用节点，否则发布校验会失败。
            </span>
          </div>
        ) : null}

        {!values.enabled ? (
          <div className="callout callout--warning">
            <span>停用路由不会进入发布快照，也不会参与 gateway 匹配。</span>
          </div>
        ) : null}

        <div className="callout callout--info">
          <span>
            host + 路径 + 匹配类型 + 方法（去空格、转大写、排序后）相同的路由不能重复，服务端会返回 409。
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
