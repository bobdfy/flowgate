import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
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
import type { Route } from '../api/contracts/route'
import type { Service } from '../api/contracts/service'
import { isApiError } from '../api/errors'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'
import { RouteFormModal } from '../features/gateway/RouteFormModal'
import {
  enabledStatusMeta,
  matchTypeLabel,
  methodsLabel,
} from '../features/gateway/status'
import { useDeleteRoute, useRoutes } from '../features/gateway/useRoutes'
import { useServices } from '../features/gateway/useServices'

type FormState =
  | { mode: 'create' }
  | { mode: 'edit'; route: Route }
  | null

type StatusFilter = 'all' | 'enabled' | 'disabled'

const EMPTY_ROUTES: Route[] = []
const EMPTY_SERVICES: Service[] = []

export function RoutesPage() {
  const routesQuery = useRoutes()
  const servicesQuery = useServices()
  const deleteMutation = useDeleteRoute()
  const [searchParams, setSearchParams] = useSearchParams()

  const rawServiceFilter = searchParams.get('service')
  const serviceFilter =
    rawServiceFilter && /^[1-9]\d*$/.test(rawServiceFilter)
      ? rawServiceFilter
      : 'all'

  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [formState, setFormState] = useState<FormState>(null)
  const [deleteTarget, setDeleteTarget] = useState<Route | null>(null)

  const routes = routesQuery.data ?? EMPTY_ROUTES
  const services = servicesQuery.data ?? EMPTY_SERVICES

  const serviceById = useMemo(
    () => new Map(services.map((service) => [service.id, service])),
    [services],
  )

  const setServiceFilter = (value: string) => {
    const next = new URLSearchParams(searchParams)
    if (value === 'all') {
      next.delete('service')
    } else {
      next.set('service', value)
    }
    setSearchParams(next, { replace: true })
  }

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return routes.filter((route) => {
      if (
        statusFilter !== 'all' &&
        route.enabled !== (statusFilter === 'enabled')
      ) {
        return false
      }
      if (
        serviceFilter !== 'all' &&
        String(route.service_id) !== serviceFilter
      ) {
        return false
      }
      if (!keyword) {
        return true
      }
      return (
        route.name.toLowerCase().includes(keyword) ||
        route.path_pattern.toLowerCase().includes(keyword) ||
        route.host.toLowerCase().includes(keyword) ||
        String(route.id).includes(keyword)
      )
    })
  }, [routes, query, statusFilter, serviceFilter])

  const pagination = usePagination(
    filtered,
    `${query}\u0000${statusFilter}\u0000${serviceFilter}`,
  )

  const enabledCount = routes.filter((route) => route.enabled).length
  const hasFilter =
    query.trim() !== '' || statusFilter !== 'all' || serviceFilter !== 'all'
  const serviceOptionsReady =
    !servicesQuery.isPending &&
    !servicesQuery.isError &&
    services.length > 0

  const clearFilters = () => {
    setQuery('')
    setStatusFilter('all')
    setServiceFilter('all')
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
        title="HTTP 路由"
        description="配置 Host、路径匹配与允许方法，并把请求转发到上游服务。"
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={routesQuery.isFetching || servicesQuery.isFetching}
              onClick={() => {
                void routesQuery.refetch()
                void servicesQuery.refetch()
              }}
            >
              刷新
            </Button>
            <Button
              variant="primary"
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={() => setFormState({ mode: 'create' })}
              disabled={!serviceOptionsReady}
              title={
                serviceOptionsReady
                  ? undefined
                  : '需要先创建并成功加载上游服务'
              }
            >
              新建路由
            </Button>
          </>
        }
      />

      {servicesQuery.isError ? (
        <div className="callout callout--danger" role="alert">
          <span>
            上游服务列表加载失败，路由仍可查看，但无法可靠解析服务名称或创建新路由。
          </span>
        </div>
      ) : null}

      {!servicesQuery.isPending &&
      !servicesQuery.isError &&
      services.length === 0 ? (
        <div className="callout callout--warning">
          <span>
            还没有上游服务。创建服务并添加至少一个启用节点后，才能创建路由。
            <Link to="/gateway/services" style={{ marginLeft: 6 }}>
              前往服务治理
            </Link>
          </span>
        </div>
      ) : null}

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
                placeholder="按名称、路径、Host 或 ID 筛选"
                aria-label="按名称、路径、Host 或 ID 筛选路由"
                disabled={routesQuery.isPending}
              />
            </div>
            <select
              className="field__input table-filter"
              value={serviceFilter}
              onChange={(event) => setServiceFilter(event.target.value)}
              aria-label="按目标服务筛选路由"
              disabled={routesQuery.isPending || servicesQuery.isPending}
            >
              <option value="all">全部服务</option>
              {services.map((service) => (
                <option key={service.id} value={String(service.id)}>
                  {service.name}
                </option>
              ))}
            </select>
            <select
              className="field__input table-filter"
              value={statusFilter}
              onChange={(event) =>
                setStatusFilter(event.target.value as StatusFilter)
              }
              aria-label="按状态筛选路由"
              disabled={routesQuery.isPending}
            >
              <option value="all">全部状态</option>
              <option value="enabled">启用中</option>
              <option value="disabled">已停用</option>
            </select>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {routesQuery.isPending
              ? '正在加载…'
              : routesQuery.isError
                ? '列表加载失败'
                : hasFilter
                  ? `显示 ${filtered.length} / ${routes.length} 条路由`
                  : `共 ${routes.length} 条路由 · 启用 ${enabledCount} · 停用 ${routes.length - enabledCount}`}
          </div>
        </div>

        {routesQuery.isPending ? (
          <TableSkeleton columns={7} rows={5} minWidth={1040} />
        ) : routesQuery.isError ? (
          <div className="table-state">
            <ErrorState
              error={routesQuery.error}
              onRetry={() => void routesQuery.refetch()}
            />
          </div>
        ) : routes.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有 HTTP 路由"
              description={
                services.length === 0
                  ? '先创建上游服务与节点，再回来配置路由。'
                  : '创建第一条路由，把请求路径映射到上游服务。'
              }
              actions={
                services.length === 0 ? (
                  <Link
                    className="button button--primary button--sm"
                    to="/gateway/services"
                  >
                    前往服务治理
                  </Link>
                ) : (
                  <Button
                    variant="primary"
                    size="sm"
                    icon={<Plus size={14} aria-hidden="true" />}
                    onClick={() => setFormState({ mode: 'create' })}
                  >
                    新建路由
                  </Button>
                )
              }
            />
          </div>
        ) : filtered.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="没有匹配的路由"
              description="筛选只作用于当前已加载的路由列表。"
              actions={
                <Button size="sm" onClick={clearFilters}>
                  清除筛选
                </Button>
              }
            />
          </div>
        ) : (
          <Table minWidth={1040}>
            <thead>
              <tr>
                <th>路由</th>
                <th>目标服务</th>
                <th>匹配规则</th>
                <th>方法</th>
                <th>状态</th>
                <th>路由级并发</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {pagination.pageItems.map((route) => {
                const statusMeta = enabledStatusMeta(route.enabled)
                const service = serviceById.get(route.service_id)
                return (
                  <tr key={route.id}>
                    <td>
                      <span className="table-strong">{route.name}</span>
                      <div className="table-muted">ID {route.id}</div>
                    </td>
                    <td>
                      {service ? (
                        <Link
                          className="table-link"
                          to={`/gateway/services/${service.id}`}
                        >
                          {service.name}
                        </Link>
                      ) : (
                        <span
                          className={
                            servicesQuery.isPending || servicesQuery.isError
                              ? 'table-muted'
                              : 'table-danger'
                          }
                        >
                          服务 #{route.service_id}
                        </span>
                      )}
                      <div className="table-muted">
                        ID {route.service_id}
                        {service && !service.enabled ? ' · 服务已停用' : ''}
                      </div>
                    </td>
                    <td>
                      <span className="text-mono">
                        {route.host || '任意 Host'}
                      </span>
                      <div className="text-mono">{route.path_pattern}</div>
                      <div className="table-muted">
                        {matchTypeLabel(route.path_match_type)}
                      </div>
                    </td>
                    <td className="text-mono">
                      {methodsLabel(route.methods)}
                    </td>
                    <td>
                      <Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>
                    </td>
                    <td className="text-mono">
                      {route.max_concurrency > 0
                        ? route.max_concurrency
                        : '不限制'}
                    </td>
                    <td className="table-muted">
                      {formatDateTime(route.updated_at)}
                    </td>
                    <td>
                      <div className="table-actions">
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={<Pencil size={13} aria-hidden="true" />}
                          onClick={() =>
                            setFormState({ mode: 'edit', route })
                          }
                          disabled={!serviceOptionsReady}
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          icon={<Trash2 size={13} aria-hidden="true" />}
                          onClick={() => setDeleteTarget(route)}
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
        {!routesQuery.isPending &&
        !routesQuery.isError &&
        filtered.length > 0 ? (
          <Pagination pagination={pagination} itemLabel="条路由" />
        ) : null}
      </Card>

      <p className="page-note">
        管理 API 没有单条路由查询接口，编辑使用列表中的数据。保存只写入管理面；需要在发布中心发布后，gateway
        才会加载新路由。相同 Host、路径、匹配类型与方法组合只能存在一条路由。
      </p>

      {formState ? (
        <RouteFormModal
          key={
            formState.mode === 'edit'
              ? `route-${formState.route.id}`
              : 'create-route'
          }
          open
          services={services}
          route={formState.mode === 'edit' ? formState.route : undefined}
          defaultServiceId={
            serviceFilter === 'all' ? undefined : Number(serviceFilter)
          }
          onClose={() => setFormState(null)}
          onSaved={() => setFormState(null)}
        />
      ) : null}

      <ConfirmDialog
        open={deleteTarget !== null}
        title="删除路由"
        description={
          deleteTarget ? (
            <>
              <p>
                确定删除路由 <strong>{deleteTarget.name}</strong>（ID{' '}
                {deleteTarget.id}）吗？
              </p>
              <p className="confirm__warning">
                删除后无法恢复。gateway
                仍使用最近一次发布的快照，直到发布新版本。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除路由"
        tone="danger"
        loading={deleteMutation.isPending}
        error={
          deleteMutation.error
            ? routeDeleteErrorMessage(deleteMutation.error)
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

function routeDeleteErrorMessage(error: unknown): string {
  if (isApiError(error) && error.status && error.status >= 500) {
    return '管理面返回服务端错误，路由可能已被删除。请刷新列表确认。'
  }
  return error instanceof Error ? error.message : '未知错误'
}
