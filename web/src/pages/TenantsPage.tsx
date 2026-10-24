import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react'
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
import type { Tenant, TenantStatus } from '../api/contracts/tenant'
import { getErrorMessage } from '../lib/errorMessage'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'
import { TenantFormModal } from '../features/access/TenantFormModal'
import { tenantStatusMeta } from '../features/access/status'
import { useDeleteTenant, useTenants } from '../features/access/useTenants'
import { TPMWindowCard } from '../features/settings/TPMWindowCard'

type FormState =
  | { mode: 'create' }
  | { mode: 'edit'; tenant: Tenant }
  | null

type StatusFilter = 'all' | TenantStatus

const EMPTY_TENANTS: Tenant[] = []

export function TenantsPage() {
  const navigate = useNavigate()
  const tenantsQuery = useTenants()
  const deleteMutation = useDeleteTenant()
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [formState, setFormState] = useState<FormState>(null)
  const [deleteTarget, setDeleteTarget] = useState<Tenant | null>(null)

  const tenants = tenantsQuery.data ?? EMPTY_TENANTS
  const activeCount = tenants.filter(
    (tenant) => tenant.status === 'active',
  ).length
  const disabledCount = tenants.length - activeCount

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return tenants.filter((tenant) => {
      if (statusFilter !== 'all' && tenant.status !== statusFilter) {
        return false
      }
      if (!keyword) {
        return true
      }
      return (
        tenant.name.toLowerCase().includes(keyword) ||
        String(tenant.id).includes(keyword)
      )
    })
  }, [tenants, query, statusFilter])

  const pagination = usePagination(
    filtered,
    `${query}\u0000${statusFilter}`,
  )

  const hasFilter = query.trim() !== '' || statusFilter !== 'all'

  const clearFilters = () => {
    setQuery('')
    setStatusFilter('all')
  }

  const closeDeleteDialog = () => {
    if (deleteMutation.isPending) {
      return
    }
    setDeleteTarget(null)
    deleteMutation.reset()
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="访问控制"
        title="租户与 API Key"
        description="管理租户状态、QPS/TPM 配额，并为租户签发独立的 API Key。"
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={tenantsQuery.isFetching}
              onClick={() => void tenantsQuery.refetch()}
            >
              刷新
            </Button>
            <Button
              variant="primary"
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={() => setFormState({ mode: 'create' })}
            >
              新建租户
            </Button>
          </>
        }
      />

      <TPMWindowCard />

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
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="按名称或 ID 筛选"
                aria-label="按名称或 ID 筛选租户"
                disabled={tenantsQuery.isPending}
              />
            </div>
            <select
              className="field__input table-filter"
              value={statusFilter}
              onChange={(event) =>
                setStatusFilter(event.target.value as StatusFilter)
              }
              aria-label="按状态筛选租户"
              disabled={tenantsQuery.isPending}
            >
              <option value="all">全部状态</option>
              <option value="active">启用中</option>
              <option value="disabled">已停用</option>
            </select>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {tenantsQuery.isPending
              ? '正在加载…'
              : tenantsQuery.isError
                ? '列表加载失败'
                : hasFilter
                  ? `显示 ${filtered.length} / ${tenants.length} 个租户`
                  : `共 ${tenants.length} 个租户 · 启用 ${activeCount} · 停用 ${disabledCount}`}
          </div>
        </div>

        {tenantsQuery.isPending ? (
          <TableSkeleton columns={6} rows={5} minWidth={900} />
        ) : tenantsQuery.isError ? (
          <div className="table-state">
            <ErrorState
              error={tenantsQuery.error}
              onRetry={() => void tenantsQuery.refetch()}
            />
          </div>
        ) : tenants.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有租户"
              description="创建租户后即可配置配额并签发 API Key。"
              actions={
                <Button
                  variant="primary"
                  size="sm"
                  icon={<Plus size={14} aria-hidden="true" />}
                  onClick={() => setFormState({ mode: 'create' })}
                >
                  新建租户
                </Button>
              }
            />
          </div>
        ) : filtered.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="没有匹配的租户"
              description="筛选只作用于当前已加载的租户列表。"
              actions={
                <Button size="sm" onClick={clearFilters}>
                  清除筛选
                </Button>
              }
            />
          </div>
        ) : (
          <Table minWidth={900}>
            <thead>
              <tr>
                <th>租户</th>
                <th>状态</th>
                <th>QPS 配额</th>
                <th>TPM 配额</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {pagination.pageItems.map((tenant) => {
                const statusMeta = tenantStatusMeta(tenant.status)
                return (
                  <tr key={tenant.id}>
                    <td>
                      <Link
                        className="table-link"
                        to={`/access/tenants/${tenant.id}`}
                      >
                        {tenant.name}
                      </Link>
                      <div className="table-muted">ID {tenant.id}</div>
                    </td>
                    <td>
                      <Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>
                    </td>
                    <td className="text-mono">
                      {tenant.qps_limit.toLocaleString()}
                    </td>
                    <td className="text-mono">
                      {tenant.tpm_limit.toLocaleString()}
                    </td>
                    <td className="table-muted">
                      {formatDateTime(tenant.created_at)}
                    </td>
                    <td>
                      <div className="table-actions">
                        <Link
                          className="button button--ghost button--sm"
                          to={`/access/tenants/${tenant.id}`}
                        >
                          管理 Key
                        </Link>
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={<Pencil size={13} aria-hidden="true" />}
                          onClick={() =>
                            setFormState({ mode: 'edit', tenant })
                          }
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          icon={<Trash2 size={13} aria-hidden="true" />}
                          onClick={() => setDeleteTarget(tenant)}
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
        {!tenantsQuery.isPending &&
        !tenantsQuery.isError &&
        filtered.length > 0 ? (
          <Pagination pagination={pagination} itemLabel="个租户" />
        ) : null}
      </Card>

      <p className="page-note">
        管理 API 没有分页参数，筛选与分页只作用于当前已加载的完整列表。删除租户会级联删除其名下全部 API Key。
      </p>

      {formState ? (
        <TenantFormModal
          key={
            formState.mode === 'edit'
              ? `edit-${formState.tenant.id}`
              : 'create'
          }
          open
          tenant={formState.mode === 'edit' ? formState.tenant : undefined}
          existingNames={tenants.map((tenant) => tenant.name)}
          onClose={() => setFormState(null)}
          onSaved={(tenantId) => {
            const wasCreate = formState.mode === 'create'
            setFormState(null)
            if (wasCreate) {
              navigate(`/access/tenants/${tenantId}`)
            }
          }}
        />
      ) : null}

      <ConfirmDialog
        open={deleteTarget !== null}
        title="删除租户"
        description={
          deleteTarget ? (
            <>
              <p>
                确定删除租户 <strong>{deleteTarget.name}</strong>（ID{' '}
                {deleteTarget.id}）吗？
              </p>
              <p className="confirm__warning">
                该租户名下的所有 API Key 会被一并删除，使用这些 Key
                的客户端将立即无法通过鉴权。此操作无法撤销。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除租户"
        tone="danger"
        loading={deleteMutation.isPending}
        error={
          deleteMutation.error ? getErrorMessage(deleteMutation.error) : null
        }
        onConfirm={() => {
          if (!deleteTarget) {
            return
          }
          deleteMutation.mutate(deleteTarget.id, {
            onSuccess: () => setDeleteTarget(null),
          })
        }}
        onClose={closeDeleteDialog}
      />
    </div>
  )
}
