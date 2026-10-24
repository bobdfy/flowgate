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
import type { Node } from '../api/contracts/node'
import { isApiError } from '../api/errors'
import { isMockMode } from '../lib/capabilities'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'
import { NodeFormModal } from '../features/gateway/NodeFormModal'
import { ServiceFormModal } from '../features/gateway/ServiceFormModal'
import { serviceDeleteErrorMessage } from '../features/gateway/errorMessages'
import {
  enabledStatusMeta,
  nodeHealthStatusMeta,
  overloadStrategyLabel,
  protocolLabel,
  runtimeNodeStateMeta,
} from '../features/gateway/status'
import { useDeleteNode, useNodes } from '../features/gateway/useNodes'
import { useRuntimeNodes } from '../features/runtime/useRuntime'
import {
  useDeleteService,
  useService,
  useServices,
} from '../features/gateway/useServices'

type NodeFormState =
  | { mode: 'create' }
  | { mode: 'edit'; node: Node }
  | null

function parseServiceId(raw: string | undefined): number | null {
  if (!raw || !/^[1-9]\d*$/.test(raw)) {
    return null
  }
  const parsed = Number(raw)
  return Number.isSafeInteger(parsed) ? parsed : null
}

const EMPTY_NODES: Node[] = []

export function ServiceDetailPage() {
  const params = useParams()
  const navigate = useNavigate()
  const serviceId = parseServiceId(params.id)

  const serviceQuery = useService(serviceId)
  const nodesQuery = useNodes(serviceId)
  const servicesQuery = useServices()
  // gateway 实时健康：只读快照，演示模式没有真实 gateway。
  const runtimeQuery = useRuntimeNodes(!isMockMode)
  const deleteNodeMutation = useDeleteNode(serviceId ?? 0)
  const deleteServiceMutation = useDeleteService()

  const [editServiceOpen, setEditServiceOpen] = useState(false)
  const [nodeFormState, setNodeFormState] = useState<NodeFormState>(null)
  const [deleteNodeTarget, setDeleteNodeTarget] = useState<Node | null>(null)
  const [deleteServiceOpen, setDeleteServiceOpen] = useState(false)

  const nodes = nodesQuery.data ?? EMPTY_NODES
  const nodePagination = usePagination(nodes, 'nodes')

  const runtimeByNodeId = new Map(
    (runtimeQuery.data?.nodes ?? []).map((node) => [node.node_id, node]),
  )

  const backLink = (
    <Link className="back-link" to="/gateway/services">
      <ArrowLeft size={14} aria-hidden="true" />
      返回服务列表
    </Link>
  )

  if (serviceId === null) {
    return (
      <div className="page">
        {backLink}
        <PageHeader
          eyebrow="流量配置 / 服务治理"
          title="服务详情"
          description="无法加载服务配置。"
        />
        <ErrorState
          title="服务 ID 无效"
          error={new Error('URL 中的服务 ID 必须是正整数。')}
          actions={
            <Link className="button button--secondary" to="/gateway/services">
              返回服务列表
            </Link>
          }
        />
      </div>
    )
  }

  if (serviceQuery.isPending) {
    return (
      <div className="page">
        {backLink}
        <PageHeader
          eyebrow="流量配置 / 服务治理"
          title="服务详情"
          description="正在加载服务配置与节点…"
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

  if (serviceQuery.isError || !serviceQuery.data) {
    const notFound =
      isApiError(serviceQuery.error) && serviceQuery.error.kind === 'not-found'
    return (
      <div className="page">
        {backLink}
        <PageHeader
          eyebrow="流量配置 / 服务治理"
          title="服务详情"
          description="无法加载服务配置。"
        />
        <ErrorState
          title={notFound ? '服务不存在' : '服务加载失败'}
          error={
            serviceQuery.error ?? new Error('管理面没有返回服务数据。')
          }
          actions={
            <div className="row">
              <Button
                size="sm"
                onClick={() => void serviceQuery.refetch()}
                loading={serviceQuery.isFetching}
              >
                重试
              </Button>
              <Link
                className="button button--ghost button--sm"
                to="/gateway/services"
              >
                返回列表
              </Link>
            </div>
          }
        />
      </div>
    )
  }

  const service = serviceQuery.data
  const statusMeta = enabledStatusMeta(service.enabled)
  const enabledNodes = nodes.filter((node) => node.enabled).length
  const refreshing = serviceQuery.isFetching || nodesQuery.isFetching

  const refresh = () => {
    void serviceQuery.refetch()
    void nodesQuery.refetch()
    void runtimeQuery.refetch()
  }

  const closeDeleteNodeDialog = () => {
    if (deleteNodeMutation.isPending) {
      return
    }
    setDeleteNodeTarget(null)
    deleteNodeMutation.reset()
  }

  const closeDeleteServiceDialog = () => {
    if (deleteServiceMutation.isPending) {
      return
    }
    setDeleteServiceOpen(false)
    deleteServiceMutation.reset()
  }

  return (
    <div className="page">
      {backLink}

      <PageHeader
        eyebrow="流量配置 / 服务治理"
        title={service.name}
        description={`服务 ID ${service.id} · 创建于 ${formatDateTime(service.created_at)}`}
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={refreshing}
              onClick={refresh}
            >
              刷新
            </Button>
            <Link
              className="button button--secondary"
              to={`/gateway/routes?service=${service.id}`}
            >
              查看关联路由
            </Link>
            <Button
              icon={<Pencil size={14} aria-hidden="true" />}
              onClick={() => setEditServiceOpen(true)}
            >
              编辑服务
            </Button>
            <Button
              variant="danger-ghost"
              icon={<Trash2 size={14} aria-hidden="true" />}
              onClick={() => setDeleteServiceOpen(true)}
            >
              删除服务
            </Button>
          </>
        }
      />

      {!service.enabled ? (
        <div className="callout callout--warning">
          <span>
            该服务已停用，不会进入发布快照；启用中的路由若指向它会发布失败。
            重新启用后仍需在发布中心发布新版本。
          </span>
        </div>
      ) : null}

      <Card>
        <CardHeader
          title="服务配置"
          description="配置值写入管理面后不会自动生效，需要在发布中心发布新版本。"
          actions={<Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>}
        />
        <CardBody>
          <div className="section-title">基础信息</div>
          <dl className="kv-list">
            <dt>服务 ID</dt>
            <dd className="text-mono">{service.id}</dd>
            <dt>协议</dt>
            <dd>{protocolLabel(service.protocol)}</dd>
            <dt>建连超时</dt>
            <dd className="text-mono">{service.connect_timeout_ms} ms</dd>
            <dt>请求超时</dt>
            <dd className="text-mono">{service.request_timeout_ms} ms</dd>
            <dt>响应头超时</dt>
            <dd className="text-mono">{service.response_header_timeout_ms} ms</dd>
            <dt>最近更新</dt>
            <dd>{formatDateTime(service.updated_at)}</dd>
          </dl>

          <hr className="divider" />

          <div className="section-title">重试</div>
          <dl className="kv-list">
            <dt>最大重试次数</dt>
            <dd className="text-mono">{service.max_retries}</dd>
            <dt>重试状态码</dt>
            <dd className="text-mono">
              {service.retry_on_status || '仅传输错误'}
            </dd>
            <dt>重试退避</dt>
            <dd className="text-mono">{service.retry_backoff_ms} ms</dd>
          </dl>

          <hr className="divider" />

          <div className="section-title">熔断</div>
          <dl className="kv-list">
            <dt>失败阈值</dt>
            <dd className="text-mono">{service.cb_failure_threshold}</dd>
            <dt>冷却时间</dt>
            <dd className="text-mono">{service.cb_cooldown_ms} ms</dd>
            <dt>半开探测上限</dt>
            <dd className="text-mono">{service.cb_half_open_limit}</dd>
          </dl>

          <hr className="divider" />

          <div className="section-title">并发保护</div>
          <dl className="kv-list">
            <dt>服务级最大并发</dt>
            <dd className="text-mono">
              {service.max_concurrency > 0
                ? service.max_concurrency
                : '不限制'}
            </dd>
            <dt>排队超时</dt>
            <dd className="text-mono">{service.queue_timeout_ms} ms</dd>
            <dt>过载策略</dt>
            <dd>{overloadStrategyLabel(service.overload_strategy)}</dd>
          </dl>
        </CardBody>
      </Card>

      <Card>
        <CardHeader
          title="上游节点"
          description="节点地址、权重与启用状态会进入发布快照。"
          actions={
            <Button
              variant="primary"
              size="sm"
              icon={<Plus size={14} aria-hidden="true" />}
              onClick={() => setNodeFormState({ mode: 'create' })}
            >
              新增节点
            </Button>
          }
        />

        {nodesQuery.isPending ? (
          <TableSkeleton columns={7} rows={4} minWidth={1050} />
        ) : nodesQuery.isError ? (
          <div className="table-state">
            <ErrorState
              title="节点加载失败"
              error={nodesQuery.error}
              onRetry={() => void nodesQuery.refetch()}
            />
          </div>
        ) : nodes.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有节点"
              description="没有启用节点时，指向该服务的启用路由无法发布。"
              actions={
                <Button
                  variant="primary"
                  size="sm"
                  icon={<Plus size={14} aria-hidden="true" />}
                  onClick={() => setNodeFormState({ mode: 'create' })}
                >
                  新增节点
                </Button>
              }
            />
          </div>
        ) : (
          <>
            <Table minWidth={1050}>
              <thead>
                <tr>
                  <th>地址</th>
                  <th>权重</th>
                  <th>状态</th>
                  <th>实时健康</th>
                  <th>health_status（持久化）</th>
                  <th>更新时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {nodePagination.pageItems.map((node) => {
                  const healthMeta = nodeHealthStatusMeta(node.health_status)
                  const runtime = runtimeByNodeId.get(node.id)
                  const runtimeMeta = runtime
                    ? runtimeNodeStateMeta(runtime.state)
                    : null
                  return (
                    <tr key={node.id}>
                      <td>
                        <span className="text-mono table-strong">
                          {node.address}
                        </span>
                        <div className="table-muted">ID {node.id}</div>
                      </td>
                      <td className="text-mono">{node.weight}</td>
                      <td>
                        <Badge tone={enabledStatusMeta(node.enabled).tone}>
                          {enabledStatusMeta(node.enabled).label}
                        </Badge>
                      </td>
                      <td>
                        {isMockMode ? (
                          <span className="table-muted">—</span>
                        ) : runtimeQuery.isPending ? (
                          <span className="table-muted">…</span>
                        ) : runtimeMeta ? (
                          <span title={runtimeMeta.description}>
                            <Badge tone={runtimeMeta.tone}>
                              {runtimeMeta.label}
                            </Badge>
                          </span>
                        ) : (
                          <span className="table-muted" title="gateway 尚未上报该节点的状态">
                            未上报
                          </span>
                        )}
                      </td>
                      <td>
                        <span title={healthMeta.description}>
                          <Badge tone={healthMeta.tone}>
                            {healthMeta.label}
                          </Badge>
                        </span>
                      </td>
                      <td className="table-muted">
                        {formatDateTime(node.updated_at)}
                      </td>
                      <td>
                        <div className="table-actions">
                          <Button
                            size="sm"
                            variant="ghost"
                            icon={<Pencil size={13} aria-hidden="true" />}
                            onClick={() =>
                              setNodeFormState({ mode: 'edit', node })
                            }
                          >
                            编辑
                          </Button>
                          <Button
                            size="sm"
                            variant="danger-ghost"
                            icon={<Trash2 size={13} aria-hidden="true" />}
                            onClick={() => setDeleteNodeTarget(node)}
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
            <Pagination pagination={nodePagination} itemLabel="个节点" />
            <p className="table-footnote">
              共 {nodes.length} 个节点，其中启用 {enabledNodes} 个。实时健康来自
              gateway 每 5 秒上报的快照
              {runtimeQuery.data
                ? `（实例 ${runtimeQuery.data.instance}，${runtimeQuery.data.age_seconds} 秒前）`
                : ''}
              ；health_status 只是持久化字段。gateway 未上报时显示「未上报」。
            </p>
          </>
        )}
      </Card>

      <p className="page-note">
        删除节点会立即写入管理面，但 gateway
        仍使用最近一次发布的快照，直到在发布中心发布新版本。删除最后一个启用节点后，指向该服务的启用路由将无法发布。
      </p>

      {editServiceOpen ? (
        <ServiceFormModal
          open
          service={service}
          existingNames={(servicesQuery.data ?? []).map(
            (item) => item.name,
          )}
          onClose={() => setEditServiceOpen(false)}
          onSaved={() => setEditServiceOpen(false)}
        />
      ) : null}

      {nodeFormState ? (
        <NodeFormModal
          key={
            nodeFormState.mode === 'edit'
              ? `node-${nodeFormState.node.id}`
              : 'create-node'
          }
          open
          serviceId={service.id}
          serviceName={service.name}
          node={
            nodeFormState.mode === 'edit' ? nodeFormState.node : undefined
          }
          onClose={() => setNodeFormState(null)}
          onSaved={() => setNodeFormState(null)}
        />
      ) : null}

      <ConfirmDialog
        open={deleteNodeTarget !== null}
        title="删除节点"
        description={
          deleteNodeTarget ? (
            <>
              <p>
                确定删除节点{' '}
                <strong className="text-mono">{deleteNodeTarget.address}</strong>
                （ID {deleteNodeTarget.id}）吗？
              </p>
              <p className="confirm__warning">
                删除后无法恢复。gateway
                会在下一次发布新版本并刷新路由表后才不再使用该节点。
              </p>
            </>
          ) : null
        }
        confirmLabel="删除节点"
        tone="danger"
        loading={deleteNodeMutation.isPending}
        error={
          deleteNodeMutation.error
            ? `节点删除失败：${getDeleteNodeError(deleteNodeMutation.error)}`
            : null
        }
        onConfirm={() => {
          if (!deleteNodeTarget) {
            return
          }
          deleteNodeMutation.mutate(deleteNodeTarget.id, {
            onSuccess: () => setDeleteNodeTarget(null),
          })
        }}
        onClose={closeDeleteNodeDialog}
      />

      <ConfirmDialog
        open={deleteServiceOpen}
        title="删除服务"
        description={
          <>
            <p>
              确定删除服务 <strong>{service.name}</strong>（ID {service.id}）
              吗？
            </p>
            <p className="confirm__warning">
              该服务下的 {nodes.length} 个节点会被级联删除。若仍有路由引用该服务，管理面会拒绝删除。此操作无法撤销。
            </p>
          </>
        }
        confirmLabel="删除服务"
        tone="danger"
        loading={deleteServiceMutation.isPending}
        error={
          deleteServiceMutation.error
            ? serviceDeleteErrorMessage(deleteServiceMutation.error)
            : null
        }
        onConfirm={() => {
          deleteServiceMutation.mutate(service.id, {
            onSuccess: () => {
              navigate('/gateway/services', { replace: true })
            },
          })
        }}
        onClose={closeDeleteServiceDialog}
      />
    </div>
  )
}

function getDeleteNodeError(error: unknown): string {
  if (isApiError(error) && error.status && error.status >= 500) {
    return '管理面返回服务端错误，节点可能已被删除。请刷新列表确认。'
  }
  return error instanceof Error ? error.message : '未知错误'
}
