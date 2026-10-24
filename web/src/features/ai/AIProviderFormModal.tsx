import { useId, useState, type FormEvent } from 'react'
import { Button } from '../../components/ui/Button'
import { FormField } from '../../components/ui/FormField'
import { Modal } from '../../components/ui/Modal'
import type { AIProvider } from '../../api/contracts/aiConfig'
import { aiErrorMessage } from './errorMessages'
import {
  useCreateAIProvider,
  useUpdateAIProvider,
} from './useAIProviders'
import {
  validateAIProviderForm,
  type AIProviderFormValues,
} from './validation'

export interface AIProviderFormModalProps {
  open: boolean
  provider?: AIProvider
  existingNames: string[]
  onClose: () => void
  onSaved: (provider: AIProvider) => void
}

function valuesFromProvider(provider: AIProvider | undefined): AIProviderFormValues {
  if (!provider) {
    return {
      name: '',
      type: 'openai',
      endpoint: '',
      upstreamPath: '',
      apiKeyEnv: '',
      streamUsage: false,
      enabled: true,
    }
  }
  return {
    name: provider.name,
    type: provider.type,
    endpoint: provider.endpoint,
    upstreamPath: provider.upstream_path,
    apiKeyEnv: provider.api_key_env,
    streamUsage: provider.stream_usage,
    enabled: provider.enabled,
  }
}

export function AIProviderFormModal({
  open,
  provider,
  existingNames,
  onClose,
  onSaved,
}: AIProviderFormModalProps) {
  const formId = useId()
  const nameId = useId()
  const typeId = useId()
  const endpointId = useId()
  const upstreamPathId = useId()
  const apiKeyEnvId = useId()
  const streamUsageId = useId()
  const enabledId = useId()

  const createMutation = useCreateAIProvider()
  const updateMutation = useUpdateAIProvider()
  const [values, setValues] = useState<AIProviderFormValues>(() =>
    valuesFromProvider(provider),
  )
  const [validationError, setValidationError] = useState<string | null>(null)

  const pending = createMutation.isPending || updateMutation.isPending
  const mutationError = createMutation.error ?? updateMutation.error
  const knownType = values.type === 'openai'
  const renamed = provider !== undefined && values.name.trim() !== provider.name
  const apiKeyEnvMissing = values.apiKeyEnv.trim() === ''

  const setValue = <K extends keyof AIProviderFormValues>(
    key: K,
    value: AIProviderFormValues[K],
  ) => {
    setValidationError(null)
    setValues((current) => ({ ...current, [key]: value }))
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setValidationError(null)

    const result = validateAIProviderForm(values, provider, existingNames)
    if (!result.ok) {
      setValidationError(result.error)
      return
    }

    try {
      const saved = provider
        ? await updateMutation.mutateAsync({
            id: provider.id,
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
      title={provider ? '编辑 AI 提供商' : '新建 AI 提供商'}
      description="保存只写入管理面数据库；gateway 通过配置版本轮询感知变更。"
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
            {provider ? '保存修改' : '创建提供商'}
          </Button>
        </>
      }
    >
      <form id={formId} className="form-stack" onSubmit={handleSubmit}>
        <section className="form-section">
          <h3 className="form-section__title">基本信息</h3>
          <div className="form-grid">
            <FormField
              label="提供商名称"
              htmlFor={nameId}
              required
              hint="全局唯一，会作为模型的关联键。改名时数据库会级联更新模型的 provider_name。"
            >
              <input
                id={nameId}
                className="field__input"
                value={values.name}
                onChange={(event) => setValue('name', event.target.value)}
                maxLength={64}
                autoComplete="off"
                spellCheck={false}
                data-autofocus
                disabled={pending}
              />
            </FormField>

            <FormField
              label="类型"
              htmlFor={typeId}
              required
              hint="gateway 当前只实现了 OpenAI 兼容适配器，其他类型会在保存时被拒绝。"
            >
              <select
                id={typeId}
                className="field__input"
                value={values.type}
                onChange={(event) => setValue('type', event.target.value)}
                disabled={pending}
              >
                {knownType ? null : (
                  <option value={values.type}>
                    {values.type || '（未设置）'}（后端不支持）
                  </option>
                )}
                <option value="openai">openai（OpenAI 兼容）</option>
              </select>
            </FormField>

            <FormField
              label="上游端点"
              htmlFor={endpointId}
              required
              fullWidth
              hint="根地址，例如 https://api.example.com。保存时后端会去掉末尾的 /。"
            >
              <input
                id={endpointId}
                className="field__input field__input--mono"
                value={values.endpoint}
                onChange={(event) => setValue('endpoint', event.target.value)}
                maxLength={512}
                placeholder="https://api.example.com"
                autoComplete="off"
                spellCheck={false}
                disabled={pending}
              />
            </FormField>

            <FormField
              label="上游路径"
              htmlFor={upstreamPathId}
              fullWidth
              hint="留空时使用对外 AI 路径。非空会覆盖所有 AI 请求的上游路径，通常只在单端点场景使用；必须以 / 开头。"
            >
              <input
                id={upstreamPathId}
                className="field__input field__input--mono"
                value={values.upstreamPath}
                onChange={(event) =>
                  setValue('upstreamPath', event.target.value)
                }
                maxLength={255}
                placeholder="/v1/chat/completions"
                autoComplete="off"
                spellCheck={false}
                disabled={pending}
              />
            </FormField>
          </div>
        </section>

        <section className="form-section">
          <h3 className="form-section__title">凭证与运行时</h3>
          <div className="form-grid">
            <FormField
              label="密钥环境变量名"
              htmlFor={apiKeyEnvId}
              fullWidth
              hint="填写变量名而不是密钥本身，例如 ZHIPU_API_KEY。控制台无法查看或修改实际密钥。"
            >
              <input
                id={apiKeyEnvId}
                className="field__input field__input--mono"
                value={values.apiKeyEnv}
                onChange={(event) => setValue('apiKeyEnv', event.target.value)}
                maxLength={128}
                placeholder="ZHIPU_API_KEY"
                autoComplete="off"
                spellCheck={false}
                disabled={pending}
              />
            </FormField>

            <FormField
              label="Stream Usage"
              htmlFor={streamUsageId}
              hint="开启后，网关会给未显式要求 usage 的流式请求注入 stream_options.include_usage，用于真实 token 结算。"
            >
              <select
                id={streamUsageId}
                className="field__input"
                value={values.streamUsage ? 'true' : 'false'}
                onChange={(event) =>
                  setValue('streamUsage', event.target.value === 'true')
                }
                disabled={pending}
              >
                <option value="false">关闭（不注入）</option>
                <option value="true">开启（上游支持 include_usage）</option>
              </select>
            </FormField>

            <FormField
              label="状态"
              htmlFor={enabledId}
              hint="停用后，其下模型不会进入 gateway 的 AI 运行时。"
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
        </section>

        <div className="callout callout--info">
          <span>
            管理 API 不返回 api_key，也不接受明文密钥写入。这里只能配置环境变量名；
            字面量密钥仅可能来自 ai.yaml 首次导入，控制台无法查看、修改或清除。
          </span>
        </div>

        {provider && provider.api_key_env === '' ? (
          <div className="callout callout--warning">
            <span>
              当前记录没有配置环境变量名。若它来自 ai.yaml
              首次导入，数据库里可能保存着字面量密钥；控制台无法确认该值是否存在。
            </span>
          </div>
        ) : null}

        {!values.enabled ? (
          <div className="callout callout--warning">
            <span>
              提供商停用后，其下全部模型会在 gateway
              下一次轮询配置版本时移出运行时；配置记录仍会保留。
            </span>
          </div>
        ) : apiKeyEnvMissing ? (
          <div className="callout callout--warning">
            <span>
              未指定环境变量名。只有数据库里存在字面量密钥时，gateway
              才会加载该提供商。
            </span>
          </div>
        ) : null}

        {renamed ? (
          <div className="callout callout--info">
            <span>
              提供商将改名为 <strong>{values.name.trim() || '（空）'}</strong>。
              数据库会通过 ON UPDATE CASCADE 同步更新关联模型的
              provider_name，模型别名不受影响。
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
