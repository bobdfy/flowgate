import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  ArrowLeft,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Card, CardBody, CardHeader } from '../components/ui/Card'
import { ConfirmDialog } from '../components/ui/ConfirmDialog'
import { EmptyState } from '../components/data/EmptyState'
import { ErrorState } from '../components/data/ErrorState'
import { Pagination } from '../components/data/Pagination'
import { Skeleton } from '../components/data/Skeleton'
import { Table } from '../components/data/Table'
import { TableSkeleton } from '../components/data/TableSkeleton'
import { PageHeader } from '../components/layout/PageHeader'
import type {
  APIKey,
  CreateAPIKeyResponse,
} from '../api/contracts/apiKey'
import { isApiError } from '../api/errors'
import { getErrorMessage } from '../lib/errorMessage'
import { isMockMode } from '../lib/capabilities'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'
import { APIKeyFormModal } from '../features/access/APIKeyFormModal'
import { APIKeyRevealModal } from '../features/access/APIKeyRevealModal'
import { TenantFormModal } from '../features/access/TenantFormModal'
import {
  apiKeyStatusMeta,
  isAPIKeyExpired,
  tenantStatusMeta,
} from '../features/access/status'
import {
  useDeleteAPIKey,
  useAPIKeys,
} from '../features/access/useAPIKeys'
import {
  useDeleteTenant,
  useTenant,
  useTenants,
} from '../features/access/useTenants'
import {
  useTenantTPMUsage,
  useUsageSummary,
} from '../features/usage/useUsage'

const EMPTY_KEYS: APIKey[] = []

type KeyFormState =
  | { mode: 'create' }
  | { mode: 'edit'; apiKey: APIKey }
  | null

function parseTenantId(raw: string | undefined): number | null {
  if (!raw || !/^[1-9]\d*$/.test(raw)) {
    return null
  }
  const parsed = Number(raw)
  return Number.isSafeInteger(parsed) ? parsed : null
}

export function TenantDetailPage() {
  const params = useParams()
  const navigate = useNavigate()
  const rawId = params.id
  const tenantId = parseTenantId(rawId)

  const tenantQuery = useTenant(tenantId)
  const keysQuery = useAPIKeys(tenantId)
  const tenantsQuery = useTenants()
  // 演示模式没有 TPM 计数数据，不发请求也不展示该区块。
  const tpmQuery = useTenantTPMUsage(isMockMode ? null : tenantId)
  // 每个 Key 最近 24 小时的 token / 请求数，直接嵌在 Key 列表里。
  const keyUsageQuery = useUsageSummary(
    '24h',
    tenantId,
    'key',
    !isMockMode && tenantId !== null,
  )
  const deleteKeyMutation = useDeleteAPIKey(tenantId ?? 0)
  const deleteTenantMutation = useDeleteTenant()

  const [editTenantOpen, setEditTenantOpen] = useState(false)
  const [keyFormState, setKeyFormState] = useState<KeyFormState>(null)
  const [deleteKeyTarget, setDeleteKeyTarget] = useState<APIKey | null>(null)
  const [deleteTenantOpen, setDeleteTenantOpen] = useState(false)
  const [createdKey, setCreatedKey] =
    useState<CreateAPIKeyResponse | null>(null)

  const keys = keysQuery.data ?? EMPTY_KEYS
  const keyPagination = usePagination(keys, 'api-keys')

  const keyUsage = new Map(
    (keyUsageQuery.data?.items ?? []).map((item) => [
      Number(item.group),
      item,
    ]),
  )

  const backLink = (
    <Link className="back-link" to="/access/tenants">
      <ArrowLeft size={14} aria-hidden="true" />
      返回租户列表
    </Link>
  )

  if (tenantId === null) {
    return (
      <div className="page">
        {backLink}
        <PageHeader
          eyebrow="访问控制 / 租户"
          title="租户详情"
          description="无法加载租户配置。"
        />
        <ErrorState
          title="租户 ID 无效"
          error={new Error('URL 中的租户 ID 必须是正整数。')}
          actions={
            <Link className="button button--secondary" to="/access/tenants">
              返回租户列表
            </Link>
          }
        />
      </div>
    )
  }

  if (tenantQuery.isPending) {
    return (
      <div className="page">
        {backLink}
        <PageHeader
          eyebrow="访问控制 / 租户"
          title="租户详情"
          description="正在加载租户配置与 API Key…"
        />
        <Card>
          <CardBody>
            <div className="stack">
              <Skeleton height={18} width="28%" />
              <Skeleton height={12} width="46%" />
              <Skeleton height={12} width="38%" />
              <Skeleton height={12} width="52%" />
            </div>
          </CardBody>
        </Card>
      </div>
    )
  }

  if (tenantQuery.isError || !tenantQuery.data) {
    const notFound =
      isApiError(tenantQuery.error) && tenantQuery.error.kind === 'not-found'
    return (
      <div className="page">
        {backLink}
        <PageHeader
          eyebrow="访问控制 / 租户"
          title="租户详情"
          description="无法加载租户配置。"
        />
        <ErrorState
          title={notFound ? '租户不存在' : '租户加载失败'}
          error={
            tenantQuery.error ?? new Error('管理面没有返回租户数据。')
          }
          actions={
            <div className="row">
              <Button
                size="sm"
                onClick={() => void tenantQuery.refetch()}
                loading={tenantQuery.isFetching}
              >
                重试
              </Button>
              <Link className="button button--ghost button--sm" to="/access/tenants">
                返回列表
              </Link>
            </div>
          }
        />
      </div>
    )
  }

  const tenant = tenantQuery.data
  const tenantMeta = tenantStatusMeta(tenant.status)
  const refreshing = tenantQuery.isFetching || keysQuery.isFetching

  const refresh = () => {
    void tenantQuery.refetch()
    void keysQuery.refetch()
    void tpmQuery.refetch()
  }

  const closeDeleteKeyDialog = () => {
    if (deleteKeyMutation.isPending) {
      return
    }
    setDeleteKeyTarget(null)
    deleteKeyMutation.reset()
  }

  const closeDeleteTenantDialog = () => {
    if (deleteTenantMutation.isPending) {
      return
    }
    setDeleteTenantOpen(false)
    deleteTenantMutation.reset()
  }

  return (
    <div className="page">
      {backLink}

      <PageHeader
        eyebrow="访问控制 / 租户"
        title={tenant.name}
        description={`租户 ID ${tenant.id} · 创建于 ${formatDateTime(tenant.created_at)}`}
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={refreshing}
              onClick={refresh}
            >
              刷新
            </Button>
            <Button
              icon={<Pencil size={14} aria-hidden="true" />}
              onClick={() => setEditTenantOpen(true)}
            >
              编辑租户
            </Button>
            <Button
              variant="danger-ghost"
              icon={<Trash2 size={14} aria-hidden="true" />}
              onClick={() => setDeleteTenantOpen(true)}
            >
              删除租户
            </Button>
          </>
        }
      />

      {tenant.status === 'disabled' ? (
        <div className="callout callout--warning">
          <span>
            该租户已停用，其名下所有 API Key 当前都无法通过 gateway 鉴权。重新启用租户后才会恢复。
          </span>
        </div>
      ) : null}

      <Card>
        <CardHeader
          title="租户配置"
          description="配额变更会立即写入管理面；运行时生效范围由 gateway 决定。"
          actions={<Badge tone={tenantMeta.tone}>{tenantMeta.label}</Badge>}
        />
        <CardBody>
          <dl className="kv-list">
            <dt>租户 ID</dt>
            <dd className="text-mono">{tenant.id}</dd>
            <dt>状态</dt>
            <dd>{tenantMeta.label}</dd>
            <dt>租户 QPS 配额</dt>
            <dd className="text-mono">
              {tenant.qps_limit.toLocaleString()} req/s
            </dd>
            <dt>租户 TPM 配额</dt>
            <dd className="text-mono">
              {tenant.tpm_limit.toLocaleString()} tokens / 窗口
            </dd>
            <dt>创建时间</dt>
            <dd>{formatDateTime(tenant.created_at)}</dd>
            <dt>API Key 数量</dt>
            <dd>{keysQuery.data ? keys.length : '—'}</dd>
          </dl>

          <div style={{ marginTop: 16 }}>
            {isMockMode ? null : tpmQuery.isPending ? (
              <Skeleton height={12} width="42%" />
            ) : tpmQuery.isError ? (
              <div className="callout callout--warning">
                <span>
                  TPM 用量不可用：{getErrorMessage(tpmQuery.error)}
                  （管理面需要配置 Redis 地址）
                </span>
              </div>
            ) : tpmQuery.data ? (
              <dl className="kv-list">
                <dt>重置时间</dt>
                <dd className="text-mono">
                  {formatDateTime(tpmQuery.data.window_end)}
                </dd>
                <dt>本窗口已用</dt>
                <dd className="text-mono">
                  {tpmQuery.data.used.toLocaleString()}
                  {tpmQuery.data.unlimited
                    ? ''
                    : ` / ${tpmQuery.data.limit.toLocaleString()}`}
                </dd>
                <dt>剩余额度</dt>
                <dd className="text-mono">
                  {tpmQuery.data.unlimited
                    ? '不限制'
                    : (tpmQuery.data.remaining ?? 0).toLocaleString()}
                </dd>
              </dl>
            ) : null}
          </div>
        </CardBody>
      </Card>

      <Card>
        <CardHeader
          title="API Keys"
          description="每个 Key 独立配置 QPS 与过期时间；Token 用量为最近 24 小时经过 gateway 的请求。"
          actions={
            <Button
              variant="primary"
              size="sm"
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={() => setKeyFormState({ mode: 'create' })}
            >
              创建 API Key
            </Button>
          }
        />

        {keysQuery.isPending ? (
          <TableSkeleton columns={8} rows={4} minWidth={1080} />
        ) : keysQuery.isError ? (
          <div className="table-state">
            <ErrorState
              title="API Key 加载失败"
              error={keysQuery.error}
              onRetry={() => void keysQuery.refetch()}
            />
          </div>
        ) : keys.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有 API Key"
              description="创建后请立即保存明文密钥，关闭弹窗后无法再次查看。"
              actions={
                <Button
                  variant="primary"
                  size="sm"
                  icon={<Plus size={14} aria-hidden="true" />}
                  onClick={() => setKeyFormState({ mode: 'create' })}
                >
                  创建 API Key
                </Button>
              }
            />
          </div>
        ) : (
          <>
            <Table minWidth={1080}>
              <thead>
                <tr>
                  <th>名称</th>
                  <th>状态</th>
                  <th>24h Token</th>
                  <th>24h 请求</th>
                  <th>QPS 配额</th>
                  <th>过期时间</th>
                  <th>创建时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {keyPagination.pageItems.map((apiKey) => {
                  const statusMeta = apiKeyStatusMeta(apiKey)
                  const expired = isAPIKeyExpired(apiKey.expires_at)
                  const usage = keyUsage.get(apiKey.id)
                  return (
                    <tr key={apiKey.id}>
                      <td>
                        <span className="table-strong">{apiKey.name}</span>
                        <div className="table-muted">ID {apiKey.id}</div>
                      </td>
                      <td>
                        <Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>
                      </td>
                      <td className="text-mono">
                        {isMockMode
                          ? '—'
                          : keyUsageQuery.isPending
                            ? '…'
                            : (usage?.total_tokens ?? 0).toLocaleString()}
                      </td>
                      <td className="text-mono">
                        {isMockMode
                          ? '—'
                          : keyUsageQuery.isPending
                            ? '…'
                            : (usage?.requests ?? 0).toLocaleString()}
                      </td>
                      <td className="text-mono">
                        {apiKey.qps_limit.toLocaleString()}
                      </td>
                      <td className={expired ? 'table-danger' : 'table-muted'}>
                        {apiKey.expires_at
                          ? formatDateTime(apiKey.expires_at)
                          : '永不过期'}
                      </td>
                      <td className="table-muted">
                        {formatDateTime(apiKey.created_at)}
                      </td>
                      <td>
                        <div className="table-actions">
                          <Button
                            size="sm"
                            variant="ghost"
                            icon={<Pencil size={13} aria-hidden="true" />}
                            onClick={() =>
                              setKeyFormState({ mode: 'edit', apiKey })
                            }
                          >
                            编辑
                          </Button>
                          <Button
                            size="sm"
                            variant="danger-ghost"
                            icon={<Trash2 size={13} aria-hidden="true" />}
                            onClick={() => setDeleteKeyTarget(apiKey)}
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
            <Pagination pagination={keyPagination} itemLabel="个 API Key" />
            <p className="table-footnote">
              24h Token / 请求数来自 ai_usage 流水，只统计经过 gateway
              的 AI 调用；失败请求也计入请求数（token 记 0）。
              后端未记录 API Key 的 last_used_at，因此不展示该字段。
            </p>
          </>
        )}
      </Card>

      <p className="page-note">
        删除 API Key 或停用 Key 后，使用该 Key
        的客户端会立即失去访问权限。已签发的明文密钥无法从管理面重新获取。
      </p>

      {editTenantOpen ? (
        <TenantFormModal
          open
          tenant={tenant}
          existingNames={(tenantsQuery.data ?? []).map(
            (item) => item.name,
          )}
          onClose={() => setEditTenantOpen(false)}
          onSaved={() => setEditTenantOpen(false)}
        />
      ) : null}

      {keyFormState ? (
        <APIKeyFormModal
          key={
            keyFormState.mode === 'edit'
              ? `key-${keyFormState.apiKey.id}`
              : 'create-key'
          }
          open
          tenantId={tenant.id}
          tenantName={tenant.name}
          tenantDisabled={tenant.status === 'disabled'}
          apiKey={
            keyFormState.mode === 'edit' ? keyFormState.apiKey : undefined
          }
          onClose={() => setKeyFormState(null)}
          onCreated={(created) => {
            setKeyFormState(null)
            setCreatedKey(created)
          }}
          onSaved={() => setKeyFormState(null)}
        />
      ) : null}

      <APIKeyRevealModal
        createdKey={createdKey}
        tenantName={tenant.name}
        onClose={() => setCreatedKey(null)}
      />

      <ConfirmDialog
        open={deleteKeyTarget !== null}
        title="删除 API Key"
        description={
          deleteKeyTarget ? (
            <>
              <p>
                确定删除 API Key <strong>{deleteKeyTarget.name}</strong>（ID{' '}
                {deleteKeyTarget.id}）吗？
              </p>
              <p className="confirm__warning">
                使用该 Key 的客户端将立即无法通过鉴权，且删除后无法恢复。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除 Key"
        tone="danger"
        loading={deleteKeyMutation.isPending}
        error={
          deleteKeyMutation.error
            ? getErrorMessage(deleteKeyMutation.error)
            : null
        }
        onConfirm={() => {
          if (!deleteKeyTarget) {
            return
          }
          deleteKeyMutation.mutate(deleteKeyTarget.id, {
            onSuccess: () => setDeleteKeyTarget(null),
          })
        }}
        onClose={closeDeleteKeyDialog}
      />

      <ConfirmDialog
        open={deleteTenantOpen}
        title="删除租户"
        description={
          <>
            <p>
              确定删除租户 <strong>{tenant.name}</strong>（ID {tenant.id}）吗？
            </p>
            <p className="confirm__warning">
              该租户名下的 {keysQuery.data ? keys.length : '全部'} 个 API Key
              会被一并删除，使用这些 Key 的客户端将立即无法通过鉴权。此操作无法撤销。
            </p>
          </>
        }
        confirmLabel="删除租户"
        tone="danger"
        loading={deleteTenantMutation.isPending}
        error={
          deleteTenantMutation.error
            ? getErrorMessage(deleteTenantMutation.error)
            : null
        }
        onConfirm={() => {
          deleteTenantMutation.mutate(tenant.id, {
            onSuccess: () => {
              navigate('/access/tenants', { replace: true })
            },
          })
        }}
        onClose={closeDeleteTenantDialog}
      />
    </div>
  )
}
