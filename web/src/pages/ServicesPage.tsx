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
import type { Service } from '../api/contracts/service'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'
import { ServiceFormModal } from '../features/gateway/ServiceFormModal'
import { serviceDeleteErrorMessage } from '../features/gateway/errorMessages'
import {
  enabledStatusMeta,
  overloadStrategyLabel,
  protocolLabel,
} from '../features/gateway/status'
import {
  useDeleteService,
  useServices,
} from '../features/gateway/useServices'

type FormState =
  | { mode: 'create' }
  | { mode: 'edit'; service: Service }
  | null

type StatusFilter = 'all' | 'enabled' | 'disabled'

const EMPTY_SERVICES: Service[] = []

export function ServicesPage() {
  const navigate = useNavigate()
  const servicesQuery = useServices()
  const deleteMutation = useDeleteService()
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [formState, setFormState] = useState<FormState>(null)
  const [deleteTarget, setDeleteTarget] = useState<Service | null>(null)

  const services = servicesQuery.data ?? EMPTY_SERVICES
  const enabledCount = services.filter((service) => service.enabled).length

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return services.filter((service) => {
      if (
        statusFilter !== 'all' &&
        service.enabled !== (statusFilter === 'enabled')
      ) {
        return false
      }
      if (!keyword) {
        return true
      }
      return (
        service.name.toLowerCase().includes(keyword) ||
        String(service.id).includes(keyword)
      )
    })
  }, [services, query, statusFilter])

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
        eyebrow="流量配置"
        title="服务治理"
        description="维护上游服务与超时、重试、熔断和并发保护参数；节点在服务详情中管理。"
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={servicesQuery.isFetching}
              onClick={() => void servicesQuery.refetch()}
            >
              刷新
            </Button>
            <Button
              variant="primary"
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={() => setFormState({ mode: 'create' })}
            >
              新建服务
            </Button>
          </>
        }
      />

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
                aria-label="按名称或 ID 筛选服务"
                disabled={servicesQuery.isPending}
              />
            </div>
            <select
              className="field__input table-filter"
              value={statusFilter}
              onChange={(event) =>
                setStatusFilter(event.target.value as StatusFilter)
              }
              aria-label="按状态筛选服务"
              disabled={servicesQuery.isPending}
            >
              <option value="all">全部状态</option>
              <option value="enabled">启用中</option>
              <option value="disabled">已停用</option>
            </select>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {servicesQuery.isPending
              ? '正在加载…'
              : servicesQuery.isError
                ? '列表加载失败'
                : hasFilter
                  ? `显示 ${filtered.length} / ${services.length} 个服务`
                  : `共 ${services.length} 个服务 · 启用 ${enabledCount} · 停用 ${services.length - enabledCount}`}
          </div>
        </div>

        {servicesQuery.isPending ? (
          <TableSkeleton columns={7} rows={5} minWidth={1000} />
        ) : servicesQuery.isError ? (
          <div className="table-state">
            <ErrorState
              error={servicesQuery.error}
              onRetry={() => void servicesQuery.refetch()}
            />
          </div>
        ) : services.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有上游服务"
              description="先创建服务并在详情中添加节点，然后才能配置路由并发布。"
              actions={
                <Button
                  variant="primary"
                  size="sm"
                  icon={<Plus size={14} aria-hidden="true" />}
                  onClick={() => setFormState({ mode: 'create' })}
                >
                  新建服务
                </Button>
              }
            />
          </div>
        ) : filtered.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="没有匹配的服务"
              description="筛选只作用于当前已加载的服务列表。"
              actions={
                <Button size="sm" onClick={clearFilters}>
                  清除筛选
                </Button>
              }
            />
          </div>
        ) : (
          <Table minWidth={1000}>
            <thead>
              <tr>
                <th>服务</th>
                <th>协议</th>
                <th>状态</th>
                <th>超时（建连 / 请求）</th>
                <th>重试</th>
                <th>并发保护</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {pagination.pageItems.map((service) => {
                const statusMeta = enabledStatusMeta(service.enabled)
                return (
                  <tr key={service.id}>
                    <td>
                      <Link
                        className="table-link"
                        to={`/gateway/services/${service.id}`}
                      >
                        {service.name}
                      </Link>
                      <div className="table-muted">ID {service.id}</div>
                    </td>
                    <td>{protocolLabel(service.protocol)}</td>
                    <td>
                      <Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>
                    </td>
                    <td className="text-mono">
                      {service.connect_timeout_ms} /{' '}
                      {service.request_timeout_ms} ms
                    </td>
                    <td>
                      {service.max_retries > 0 ? (
                        <>
                          <span className="text-mono">
                            {service.max_retries} 次
                          </span>
                          <div className="table-muted">
                            {service.retry_on_status || '仅传输错误'}
                          </div>
                        </>
                      ) : (
                        <span className="visually-muted">不重试</span>
                      )}
                    </td>
                    <td>
                      {service.max_concurrency > 0 ? (
                        <>
                          <span className="text-mono">
                            {service.max_concurrency} 并发
                          </span>
                          <div className="table-muted">
                            {overloadStrategyLabel(
                              service.overload_strategy,
                            )}
                          </div>
                        </>
                      ) : (
                        <span className="visually-muted">不限制</span>
                      )}
                    </td>
                    <td className="table-muted">
                      {formatDateTime(service.updated_at)}
                    </td>
                    <td>
                      <div className="table-actions">
                        <Link
                          className="button button--ghost button--sm"
                          to={`/gateway/services/${service.id}`}
                        >
                          管理节点
                        </Link>
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={<Pencil size={13} aria-hidden="true" />}
                          onClick={() =>
                            setFormState({ mode: 'edit', service })
                          }
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          icon={<Trash2 size={13} aria-hidden="true" />}
                          onClick={() => setDeleteTarget(service)}
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
        {!servicesQuery.isPending &&
        !servicesQuery.isError &&
        filtered.length > 0 ? (
          <Pagination pagination={pagination} itemLabel="个服务" />
        ) : null}
      </Card>

      <p className="page-note">
        服务与节点修改只写入管理面。需要在发布中心发布新版本后，gateway
        才会在约 5 秒内加载新的路由快照。删除被路由引用的服务会被管理面拒绝。
      </p>

      {formState ? (
        <ServiceFormModal
          key={
            formState.mode === 'edit'
              ? `edit-${formState.service.id}`
              : 'create'
          }
          open
          service={formState.mode === 'edit' ? formState.service : undefined}
          existingNames={services.map((service) => service.name)}
          onClose={() => setFormState(null)}
          onSaved={(serviceId) => {
            const wasCreate = formState.mode === 'create'
            setFormState(null)
            if (wasCreate) {
              navigate(`/gateway/services/${serviceId}`)
            }
          }}
        />
      ) : null}

      <ConfirmDialog
        open={deleteTarget !== null}
        title="删除服务"
        description={
          deleteTarget ? (
            <>
              <p>
                确定删除服务 <strong>{deleteTarget.name}</strong>（ID{' '}
                {deleteTarget.id}）吗？
              </p>
              <p className="confirm__warning">
                该服务下的全部节点会被级联删除。若仍有路由引用该服务，管理面会拒绝删除。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除服务"
        tone="danger"
        loading={deleteMutation.isPending}
        error={
          deleteMutation.error
            ? serviceDeleteErrorMessage(deleteMutation.error)
            : null
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
