import { useMemo, useState } from 'react'
import { Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react'
import type { AIModel, AIProvider } from '../api/contracts/aiConfig'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Card } from '../components/ui/Card'
import { ConfirmDialog } from '../components/ui/ConfirmDialog'
import { EmptyState } from '../components/data/EmptyState'
import { ErrorState } from '../components/data/ErrorState'
import { Pagination } from '../components/data/Pagination'
import { Table } from '../components/data/Table'
import { TableSkeleton } from '../components/data/TableSkeleton'
import { PageHeader } from '../components/layout/PageHeader'
import { AIModelFormModal } from '../features/ai/AIModelFormModal'
import { AIProviderFormModal } from '../features/ai/AIProviderFormModal'
import { aiErrorMessage } from '../features/ai/errorMessages'
import {
  aiProviderStatusMeta,
  modelRuntimeMeta,
  providerKeySourceMeta,
} from '../features/ai/status'
import { useAIModels, useDeleteAIModel } from '../features/ai/useAIModels'
import {
  useAIProviders,
  useDeleteAIProvider,
} from '../features/ai/useAIProviders'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'

type StatusFilter = 'all' | 'enabled' | 'disabled'

type ProviderFormState =
  | { mode: 'create' }
  | { mode: 'edit'; provider: AIProvider }
  | null

type ModelFormState =
  | { mode: 'create' }
  | { mode: 'edit'; model: AIModel }
  | null

const EMPTY_PROVIDERS: AIProvider[] = []
const EMPTY_MODELS: AIModel[] = []

export function AIConfigPage() {
  const providersQuery = useAIProviders()
  const modelsQuery = useAIModels()
  const deleteProviderMutation = useDeleteAIProvider()
  const deleteModelMutation = useDeleteAIModel()

  const providers = providersQuery.data ?? EMPTY_PROVIDERS
  const models = modelsQuery.data ?? EMPTY_MODELS
  const providersReady = !providersQuery.isPending && !providersQuery.isError
  const modelsReady = !modelsQuery.isPending && !modelsQuery.isError

  const [providerQuery, setProviderQuery] = useState('')
  const [providerStatus, setProviderStatus] = useState<StatusFilter>('all')
  const [modelQuery, setModelQuery] = useState('')
  const [modelProviderFilter, setModelProviderFilter] = useState('all')
  const [modelStatus, setModelStatus] = useState<StatusFilter>('all')

  const [providerFormState, setProviderFormState] =
    useState<ProviderFormState>(null)
  const [modelFormState, setModelFormState] = useState<ModelFormState>(null)
  const [deleteProviderTarget, setDeleteProviderTarget] =
    useState<AIProvider | null>(null)
  const [deleteModelTarget, setDeleteModelTarget] = useState<AIModel | null>(
    null,
  )
  const [successMessage, setSuccessMessage] = useState<string | null>(null)

  const providerByName = useMemo(
    () => new Map(providers.map((provider) => [provider.name, provider])),
    [providers],
  )

  const modelCountByProvider = useMemo(() => {
    const counts = new Map<string, number>()
    for (const model of models) {
      counts.set(model.provider_name, (counts.get(model.provider_name) ?? 0) + 1)
    }
    return counts
  }, [models])

  const filteredProviders = useMemo(() => {
    const keyword = providerQuery.trim().toLowerCase()
    return providers.filter((provider) => {
      if (
        providerStatus !== 'all' &&
        provider.enabled !== (providerStatus === 'enabled')
      ) {
        return false
      }
      if (!keyword) {
        return true
      }
      return (
        provider.name.toLowerCase().includes(keyword) ||
        provider.endpoint.toLowerCase().includes(keyword) ||
        provider.api_key_env.toLowerCase().includes(keyword) ||
        provider.type.toLowerCase().includes(keyword) ||
        String(provider.id).includes(keyword)
      )
    })
  }, [providers, providerQuery, providerStatus])

  const filteredModels = useMemo(() => {
    const keyword = modelQuery.trim().toLowerCase()
    return models.filter((model) => {
      if (
        modelStatus !== 'all' &&
        model.enabled !== (modelStatus === 'enabled')
      ) {
        return false
      }
      if (
        modelProviderFilter !== 'all' &&
        model.provider_name !== modelProviderFilter
      ) {
        return false
      }
      if (!keyword) {
        return true
      }
      return (
        model.name.toLowerCase().includes(keyword) ||
        model.provider_name.toLowerCase().includes(keyword) ||
        model.upstream_model.toLowerCase().includes(keyword)
      )
    })
  }, [models, modelQuery, modelProviderFilter, modelStatus])

  const providerPagination = usePagination(
    filteredProviders,
    `${providerQuery}\u0000${providerStatus}`,
  )
  const modelPagination = usePagination(
    filteredModels,
    `${modelQuery}\u0000${modelProviderFilter}\u0000${modelStatus}`,
  )

  const enabledProviderCount = providers.filter(
    (provider) => provider.enabled,
  ).length
  const enabledModelCount = models.filter((model) => model.enabled).length
  const providersWithoutEnv = providers.filter(
    (provider) => provider.enabled && provider.api_key_env === '',
  ).length

  const providerHasFilter =
    providerQuery.trim() !== '' || providerStatus !== 'all'
  const modelHasFilter =
    modelQuery.trim() !== '' ||
    modelProviderFilter !== 'all' ||
    modelStatus !== 'all'

  const canCreateModel = providersReady && providers.length > 0
  const createModelTitle = canCreateModel
    ? undefined
    : providersQuery.isError
      ? '提供商列表加载失败，暂时无法创建模型'
      : '需要先创建 AI 提供商'

  const deleteProviderModelCount =
    deleteProviderTarget && modelsReady
      ? (modelCountByProvider.get(deleteProviderTarget.name) ?? 0)
      : null

  const openProviderCreate = () => {
    setSuccessMessage(null)
    setProviderFormState({ mode: 'create' })
  }

  const openModelCreate = () => {
    setSuccessMessage(null)
    setModelFormState({ mode: 'create' })
  }

  const closeDeleteProvider = () => {
    if (deleteProviderMutation.isPending) {
      return
    }
    setDeleteProviderTarget(null)
    deleteProviderMutation.reset()
  }

  const closeDeleteModel = () => {
    if (deleteModelMutation.isPending) {
      return
    }
    setDeleteModelTarget(null)
    deleteModelMutation.reset()
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="AI"
        title="模型路由"
        description="维护 AI 上游提供商与对外模型映射；配置直接写入管理面数据库，由 gateway 轮询加载。"
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={providersQuery.isFetching || modelsQuery.isFetching}
              onClick={() => {
                void providersQuery.refetch()
                void modelsQuery.refetch()
              }}
            >
              刷新
            </Button>
            <Button
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={openModelCreate}
              disabled={!canCreateModel}
              title={createModelTitle}
            >
              新建模型
            </Button>
            <Button
              variant="primary"
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={openProviderCreate}
            >
              新建提供商
            </Button>
          </>
        }
      />

      {successMessage ? (
        <div className="callout callout--success" role="status">
          <span>{successMessage}</span>
        </div>
      ) : null}

      <div className="stat-grid">
        <div className="stat-card">
          <div className="stat-card__label">AI 提供商</div>
          <div className="stat-card__value">
            {providersQuery.isPending ? '—' : providers.length}
          </div>
          <div className="stat-card__meta">
            {providersQuery.isPending
              ? '正在加载提供商列表'
              : `启用 ${enabledProviderCount} · 停用 ${
                  providers.length - enabledProviderCount
                }`}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-card__label">模型映射</div>
          <div className="stat-card__value">
            {modelsQuery.isPending ? '—' : models.length}
          </div>
          <div className="stat-card__meta">
            {modelsQuery.isPending
              ? '正在加载模型列表'
              : `启用 ${enabledModelCount} · 停用 ${
                  models.length - enabledModelCount
                }`}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-card__label">启用但未指定密钥环境变量</div>
          <div className="stat-card__value">
            {providersQuery.isPending ? '—' : providersWithoutEnv}
          </div>
          <div className="stat-card__meta">
            这些提供商的模型仅在数据库存在字面量密钥时才可能加载
          </div>
        </div>
      </div>

      <div className="callout callout--info">
        <span>
          AI 配置不经过发布中心。每次写操作会在同一事务里把
          ai_config_version 加 1；gateway 每 3 秒检查一次版本号，变化后重建
          AI handler。控制台只能确认配置已保存，没有运行时加载回执。
        </span>
      </div>

      <div className="callout callout--info">
        <span>
          提供商启用只是配置条件。gateway 还需要解析到密钥——api_key_env
          指向的环境变量，或 ai.yaml 首次导入到数据库的字面量密钥——才会注册其模型；
          模型自身也必须启用。
        </span>
      </div>

      <Card>
        <div className="table-toolbar">
          <div className="table-toolbar__filters">
            <div className="table-search">
              <Search
                size={14}
                className="table-search__icon"
                aria-hidden="true"
              />
              <input
                className="field__input table-search__input"
                type="search"
                value={providerQuery}
                onChange={(event) => setProviderQuery(event.target.value)}
                placeholder="按名称、端点、环境变量或 ID 筛选"
                aria-label="按名称、端点、环境变量或 ID 筛选提供商"
                disabled={providersQuery.isPending}
              />
            </div>
            <select
              className="field__input table-filter"
              value={providerStatus}
              onChange={(event) =>
                setProviderStatus(event.target.value as StatusFilter)
              }
              aria-label="按状态筛选提供商"
              disabled={providersQuery.isPending}
            >
              <option value="all">全部状态</option>
              <option value="enabled">启用中</option>
              <option value="disabled">已停用</option>
            </select>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {providersQuery.isPending
              ? '正在加载…'
              : providersQuery.isError
                ? '提供商列表加载失败'
                : providerHasFilter
                  ? `显示 ${filteredProviders.length} / ${providers.length} 个提供商`
                  : `共 ${providers.length} 个提供商`}
          </div>
        </div>

        {providersQuery.isPending ? (
          <TableSkeleton columns={8} rows={4} minWidth={1120} />
        ) : providersQuery.isError ? (
          <div className="table-state">
            <ErrorState
              title="提供商列表加载失败"
              error={providersQuery.error}
              onRetry={() => void providersQuery.refetch()}
            />
          </div>
        ) : providers.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有 AI 提供商"
              description="创建提供商并配置密钥环境变量后，gateway 才能向上游转发 AI 请求。"
              actions={
                <Button
                  variant="primary"
                  size="sm"
                  icon={<Plus size={14} aria-hidden="true" />}
                  onClick={openProviderCreate}
                >
                  新建提供商
                </Button>
              }
            />
          </div>
        ) : filteredProviders.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="没有匹配的提供商"
              description="筛选只作用于当前已加载的提供商列表。"
              actions={
                <Button
                  size="sm"
                  onClick={() => {
                    setProviderQuery('')
                    setProviderStatus('all')
                  }}
                >
                  清除筛选
                </Button>
              }
            />
          </div>
        ) : (
          <Table minWidth={1120}>
            <thead>
              <tr>
                <th>提供商</th>
                <th>端点</th>
                <th>密钥来源</th>
                <th>模型数</th>
                <th>Stream Usage</th>
                <th>状态</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {providerPagination.pageItems.map((provider) => {
                const statusMeta = aiProviderStatusMeta(provider)
                const keyMeta = providerKeySourceMeta(provider)
                const modelCount = modelsReady
                  ? (modelCountByProvider.get(provider.name) ?? 0)
                  : null
                return (
                  <tr key={provider.id}>
                    <td>
                      <span className="table-strong">{provider.name}</span>
                      <div className="table-muted">
                        ID {provider.id} · {provider.type}
                      </div>
                    </td>
                    <td>
                      <span className="text-mono">{provider.endpoint}</span>
                      <div className="table-muted">
                        {provider.upstream_path || '默认上游路径'}
                      </div>
                    </td>
                    <td>
                      <span
                        className={
                          provider.api_key_env
                            ? 'text-mono'
                            : 'table-danger'
                        }
                        title={keyMeta.description}
                      >
                        {keyMeta.label}
                      </span>
                      <div className="table-muted">
                        {provider.api_key_env
                          ? '环境变量名'
                          : '字面量密钥不可见'}
                      </div>
                    </td>
                    <td className="table-muted">
                      {modelCount === null ? '—' : `${modelCount} 个`}
                    </td>
                    <td className="table-muted">
                      {provider.stream_usage ? '开启' : '关闭'}
                    </td>
                    <td>
                      <span title={statusMeta.description}>
                        <Badge tone={statusMeta.tone}>
                          {statusMeta.label}
                        </Badge>
                      </span>
                    </td>
                    <td className="table-muted">
                      {formatDateTime(provider.updated_at)}
                    </td>
                    <td>
                      <div className="table-actions">
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={<Pencil size={13} aria-hidden="true" />}
                          onClick={() => {
                            setSuccessMessage(null)
                            setProviderFormState({
                              mode: 'edit',
                              provider,
                            })
                          }}
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          icon={<Trash2 size={13} aria-hidden="true" />}
                          onClick={() =>
                            setDeleteProviderTarget(provider)
                          }
                        >
                          删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
        )}
        {!providersQuery.isPending &&
        !providersQuery.isError &&
        filteredProviders.length > 0 ? (
          <Pagination pagination={providerPagination} itemLabel="个提供商" />
        ) : null}
      </Card>

      <Card>
        <div className="table-toolbar">
          <div className="table-toolbar__filters">
            <div className="table-search">
              <Search
                size={14}
                className="table-search__icon"
                aria-hidden="true"
              />
              <input
                className="field__input table-search__input"
                type="search"
                value={modelQuery}
                onChange={(event) => setModelQuery(event.target.value)}
                placeholder="按模型别名、上游模型或提供商筛选"
                aria-label="按模型别名、上游模型或提供商筛选"
                disabled={modelsQuery.isPending}
              />
            </div>
            <select
              className="field__input table-filter"
              value={modelProviderFilter}
              onChange={(event) =>
                setModelProviderFilter(event.target.value)
              }
              aria-label="按提供商筛选模型"
              disabled={modelsQuery.isPending || providersQuery.isPending}
            >
              <option value="all">全部提供商</option>
              {providers.map((provider) => (
                <option key={provider.id} value={provider.name}>
                  {provider.name}
                </option>
              ))}
            </select>
            <select
              className="field__input table-filter"
              value={modelStatus}
              onChange={(event) =>
                setModelStatus(event.target.value as StatusFilter)
              }
              aria-label="按状态筛选模型"
              disabled={modelsQuery.isPending}
            >
              <option value="all">全部状态</option>
              <option value="enabled">启用中</option>
              <option value="disabled">已停用</option>
            </select>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {modelsQuery.isPending
              ? '正在加载…'
              : modelsQuery.isError
                ? '模型列表加载失败'
                : modelHasFilter
                  ? `显示 ${filteredModels.length} / ${models.length} 个模型`
                  : `共 ${models.length} 个模型`}
          </div>
        </div>

        {modelsQuery.isPending ? (
          <TableSkeleton columns={6} rows={4} minWidth={1040} />
        ) : modelsQuery.isError ? (
          <div className="table-state">
            <ErrorState
              title="模型列表加载失败"
              error={modelsQuery.error}
              onRetry={() => void modelsQuery.refetch()}
            />
          </div>
        ) : models.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有模型映射"
              description={
                providersQuery.isError
                  ? '提供商列表加载失败，暂时无法确认可用提供商。请刷新后重试。'
                  : providers.length === 0
                    ? '先创建 AI 提供商，再把对外模型别名映射到上游模型。'
                    : '创建模型别名，把客户端请求里的 model 映射到上游真实模型。'
              }
              actions={
                providersQuery.isError ? (
                  <Button size="sm" onClick={() => void providersQuery.refetch()}>
                    重试加载提供商
                  </Button>
                ) : providers.length === 0 ? (
                  <Button
                    variant="primary"
                    size="sm"
                    icon={<Plus size={14} aria-hidden="true" />}
                    onClick={openProviderCreate}
                  >
                    新建提供商
                  </Button>
                ) : (
                  <Button
                    variant="primary"
                    size="sm"
                    icon={<Plus size={14} aria-hidden="true" />}
                    onClick={openModelCreate}
                  >
                    新建模型
                  </Button>
                )
              }
            />
          </div>
        ) : filteredModels.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="没有匹配的模型"
              description="筛选只作用于当前已加载的模型列表。"
              actions={
                <Button
                  size="sm"
                  onClick={() => {
                    setModelQuery('')
                    setModelProviderFilter('all')
                    setModelStatus('all')
                  }}
                >
                  清除筛选
                </Button>
              }
            />
          </div>
        ) : (
          <Table minWidth={1040}>
            <thead>
              <tr>
                <th>模型别名</th>
                <th>提供商</th>
                <th>上游模型</th>
                <th>运行时判断</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {modelPagination.pageItems.map((model) => {
                const provider = providerByName.get(model.provider_name)
                const runtimeMeta = modelRuntimeMeta(
                  model,
                  provider,
                  providersReady,
                )
                return (
                  <tr key={model.name}>
                    <td>
                      <span className="table-strong text-mono">
                        {model.name}
                      </span>
                      <div className="table-muted">
                        创建于 {formatDateTime(model.created_at)}
                      </div>
                    </td>
                    <td>
                      {provider ? (
                        <>
                          <span className="table-strong">
                            {provider.name}
                          </span>
                          <div className="table-muted">
                            {provider.enabled
                              ? '提供商已启用'
                              : '提供商已停用'}
                          </div>
                        </>
                      ) : providersReady ? (
                        <>
                          <span className="table-danger">
                            {model.provider_name}
                          </span>
                          <div className="table-muted">提供商不存在</div>
                        </>
                      ) : (
                        <>
                          <span className="table-muted">
                            {model.provider_name}
                          </span>
                          <div className="table-muted">
                            提供商列表未就绪
                          </div>
                        </>
                      )}
                    </td>
                    <td>
                      {model.upstream_model ? (
                        <span className="text-mono">
                          {model.upstream_model}
                        </span>
                      ) : (
                        <span className="visually-muted">
                          使用模型别名
                        </span>
                      )}
                    </td>
                    <td>
                      <span title={runtimeMeta.description}>
                        <Badge tone={runtimeMeta.tone}>
                          {runtimeMeta.label}
                        </Badge>
                      </span>
                    </td>
                    <td className="table-muted">
                      {formatDateTime(model.updated_at)}
                    </td>
                    <td>
                      <div className="table-actions">
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={<Pencil size={13} aria-hidden="true" />}
                          onClick={() => {
                            setSuccessMessage(null)
                            setModelFormState({ mode: 'edit', model })
                          }}
                          disabled={!providersReady}
                          title={
                            providersReady
                              ? undefined
                              : '提供商列表加载失败，无法安全编辑关联关系'
                          }
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          icon={<Trash2 size={13} aria-hidden="true" />}
                          onClick={() => setDeleteModelTarget(model)}
                        >
                          删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
        )}
        {!modelsQuery.isPending &&
        !modelsQuery.isError &&
        filteredModels.length > 0 ? (
          <Pagination pagination={modelPagination} itemLabel="个模型" />
        ) : null}
      </Card>

      <p className="page-note">
        提供商名称是模型的关联键，改名时数据库会级联更新模型的 provider_name；模型别名是主键，创建后不能改名。AI
        配置与发布中心相互独立：修改后不需要发布，gateway 直接轮询 ai_config_version。
        控制台只能确认配置已保存，无法确认 gateway 是否成功加载。
      </p>

      {providerFormState ? (
        <AIProviderFormModal
          key={
            providerFormState.mode === 'edit'
              ? `provider-${providerFormState.provider.id}`
              : 'create-provider'
          }
          open
          provider={
            providerFormState.mode === 'edit'
              ? providerFormState.provider
              : undefined
          }
          existingNames={providers.map((provider) => provider.name)}
          onClose={() => setProviderFormState(null)}
          onSaved={(saved) => {
            setProviderFormState(null)
            setSuccessMessage(
              `已保存提供商 "${saved.name}"。gateway 每 3 秒检查 AI 配置版本；控制台没有加载回执。`,
            )
          }}
        />
      ) : null}

      {modelFormState ? (
        <AIModelFormModal
          key={
            modelFormState.mode === 'edit'
              ? `model-${modelFormState.model.name}`
              : 'create-model'
          }
          open
          model={
            modelFormState.mode === 'edit'
              ? modelFormState.model
              : undefined
          }
          providers={providers}
          existingNames={models.map((model) => model.name)}
          onClose={() => setModelFormState(null)}
          onSaved={(saved) => {
            setModelFormState(null)
            setSuccessMessage(
              `已保存模型映射 "${saved.name}"。gateway 每 3 秒检查 AI 配置版本；控制台没有加载回执。`,
            )
          }}
        />
      ) : null}

      <ConfirmDialog
        open={deleteProviderTarget !== null}
        title="删除 AI 提供商"
        description={
          deleteProviderTarget ? (
            <>
              <p>
                确定删除提供商{' '}
                <strong>{deleteProviderTarget.name}</strong>（ID{' '}
                {deleteProviderTarget.id}）吗？
              </p>
              <p className="confirm__warning">
                {deleteProviderModelCount === null
                  ? '若存在关联模型，它们会被级联删除。'
                  : deleteProviderModelCount > 0
                    ? `其下 ${deleteProviderModelCount} 个模型映射会被级联删除，无法恢复。`
                    : '当前没有关联模型，删除后无法恢复。'}{' '}
                gateway 会在下一次配置轮询时更新 AI 运行时。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除提供商"
        tone="danger"
        loading={deleteProviderMutation.isPending}
        error={
          deleteProviderMutation.error
            ? aiErrorMessage(deleteProviderMutation.error)
            : null
        }
        onConfirm={() => {
          if (!deleteProviderTarget) {
            return
          }
          const name = deleteProviderTarget.name
          deleteProviderMutation.mutate(deleteProviderTarget.id, {
            onSuccess: () => {
              setDeleteProviderTarget(null)
              setSuccessMessage(
                `已删除提供商 "${name}"。gateway 每 3 秒检查 AI 配置版本；控制台没有加载回执。`,
              )
            },
          })
        }}
        onClose={closeDeleteProvider}
      />

      <ConfirmDialog
        open={deleteModelTarget !== null}
        title="删除模型映射"
        description={
          deleteModelTarget ? (
            <>
              <p>
                确定删除模型映射{' '}
                <strong className="text-mono">
                  {deleteModelTarget.name}
                </strong>{' '}
                吗？
              </p>
              <p className="confirm__warning">
                删除后客户端再请求该模型别名会失败，且无法恢复。gateway
                会在下一次配置轮询时更新 AI 运行时。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除模型"
        tone="danger"
        loading={deleteModelMutation.isPending}
        error={
          deleteModelMutation.error
            ? aiErrorMessage(deleteModelMutation.error)
            : null
        }
        onConfirm={() => {
          if (!deleteModelTarget) {
            return
          }
          const name = deleteModelTarget.name
          deleteModelMutation.mutate(name, {
            onSuccess: () => {
              setDeleteModelTarget(null)
              setSuccessMessage(
                `已删除模型映射 "${name}"。gateway 每 3 秒检查 AI 配置版本；控制台没有加载回执。`,
              )
            },
          })
        }}
        onClose={closeDeleteModel}
      />
    </div>
  )
}
