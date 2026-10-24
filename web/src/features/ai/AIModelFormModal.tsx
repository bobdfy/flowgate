import { useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type { AIModel, AIProvider } from '../../api/contracts/aiConfig'
import { aiErrorMessage } from './errorMessages'
import { useCreateAIModel, useUpdateAIModel } from './useAIModels'
import {
  validateAIModelForm,
  type AIModelFormValues,
} from './validation'

export interface AIModelFormModalProps {
  open: boolean
  model?: AIModel
  providers: AIProvider[]
  existingNames: string[]
  onClose: () => void
  onSaved: (model: AIModel) => void
}

function defaultProviderName(
  model: AIModel | undefined,
  providers: AIProvider[],
): string {
  if (model) {
    return model.provider_name
  }
  return (
    providers.find((provider) => provider.enabled)?.name ??
    providers[0]?.name ??
    ''
  )
}

function valuesFromModel(
  model: AIModel | undefined,
  providers: AIProvider[],
): AIModelFormValues {
  return {
    name: model?.name ?? '',
    providerName: defaultProviderName(model, providers),
    upstreamModel: model?.upstream_model ?? '',
    enabled: model?.enabled ?? true,
  }
}

export function AIModelFormModal({
  open,
  model,
  providers,
  existingNames,
  onClose,
  onSaved,
}: AIModelFormModalProps) {
  const formId = useId()
  const nameId = useId()
  const providerId = useId()
  const upstreamModelId = useId()
  const enabledId = useId()

  const createMutation = useCreateAIModel()
  const updateMutation = useUpdateAIModel()
  const [values, setValues] = useState<AIModelFormValues>(() =>
    valuesFromModel(model, providers),
  )
  const [validationError, setValidationError] = useState<string | null>(null)

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error
  const selectedProvider = providers.find(
    (provider) => provider.name === values.providerName,
  )

  const setValue = <K extends keyof AIModelFormValues>(
    key: K,
    value: AIModelFormValues[K],
  ) => {
    setValidationError(null)
    setValues((current) => ({ ...current, [key]: value }))
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const result = validateAIModelForm(
      values,
      model,
      existingNames,
      providers.map((provider) => provider.name),
    )
    if (!result.ok) {
      setValidationError(result.error)
      return
    }

    try {
      const saved = model
        ? await updateMutation.mutateAsync({
            name: model.name,
            input: result.input,
          })
        : await createMutation.mutateAsync(result.input)
      onSaved(saved)
    } catch {
      // Mutation 状态会在表单下方展示标准化后的错误。
    }
  }

  return (
    <Modal
      open={open}
      size="lg"
      title={model ? '编辑模型映射' : '新建模型映射'}
      description="模型别名是客户端请求体里的 model 字段；保存只写入管理面数据库。"
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
            disabled={providers.length === 0}
          >
            {model ? '保存修改' : '创建模型'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        <div className="form-grid">
          <FormField
            label="模型别名"
            htmlFor={nameId}
            required
            hint="全局唯一，也是客户端请求体中的 model 值。允许字母、数字、点、冒号、短横线和下划线，最长 64 个字符。"
          >
            <input
              id={nameId}
              className="field__input field__input--mono"
              value={values.name}
              onChange={(event) => setValue('name', event.target.value)}
              maxLength={64}
              placeholder="flowgate-chat"
              autoComplete="off"
              spellCheck={false}
              data-autofocus
              disabled={pending || model !== undefined}
            />
          </FormField>

          <FormField
            label="所属提供商"
            htmlFor={providerId}
            required
            hint="下拉选项来自真实提供商列表；提供商必须已存在。"
          >
            <select
              id={providerId}
              className="field__input"
              value={values.providerName}
              onChange={(event) =>
                setValue('providerName', event.target.value)
              }
              disabled={pending || providers.length === 0}
            >
              {providers.length === 0 ? (
                <option value="">没有可用提供商</option>
              ) : null}
              {providers.map((provider) => (
                <option key={provider.id} value={provider.name}>
                  {provider.name}
                  {provider.enabled ? '' : '（已停用）'}
                </option>
              ))}
            </select>
          </FormField>

          <FormField
            label="上游模型名"
            htmlFor={upstreamModelId}
            fullWidth
            hint="发给上游的真实模型名。留空时 gateway 直接使用模型别名。"
          >
            <input
              id={upstreamModelId}
              className="field__input field__input--mono"
              value={values.upstreamModel}
              onChange={(event) =>
                setValue('upstreamModel', event.target.value)
              }
              maxLength={128}
              placeholder="gpt-4o-mini"
              autoComplete="off"
              spellCheck={false}
              disabled={pending}
            />
          </FormField>

          <FormField
            label="状态"
            htmlFor={enabledId}
            hint="停用后客户端请求该模型别名会失败。"
          >
            <select
              id={enabledId}
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

        {model ? (
          <div className="callout callout--info">
            <span>
              模型别名是主键，创建后不能改名。需要更换别名时，请删除后重新创建。
            </span>
          </div>
        ) : null}

        {providers.length === 0 ? (
          <div className="callout callout--danger" role="alert">
            <span>
              没有可用提供商，无法创建模型。请先创建提供商并确认列表加载成功。
            </span>
          </div>
        ) : null}

        {selectedProvider && !selectedProvider.enabled ? (
          <div className="callout callout--warning">
            <span>
              提供商 <strong>{selectedProvider.name}</strong>{' '}
              已停用，gateway 会跳过引用它的模型。
            </span>
          </div>
        ) : null}

        {selectedProvider &&
        selectedProvider.enabled &&
        selectedProvider.api_key_env === '' ? (
          <div className="callout callout--warning">
            <span>
              提供商未指定密钥环境变量。只有数据库里存在字面量密钥时，gateway
              才会加载该模型。
            </span>
          </div>
        ) : null}

        {!values.enabled ? (
          <div className="callout callout--warning">
            <span>
              停用后，客户端请求该模型别名会失败；配置记录仍会保留。
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
            <span>{aiErrorMessage(mutationError)}</span>
          </div>
        ) : null}
      </form>
    </Modal>
  )
}
