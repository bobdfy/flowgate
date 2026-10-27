import { useMemo } from 'react'
import { Badge } from '../../components/ui/Badge'
import { Button } from '../../components/ui/Button'
import { Modal } from '../../components/ui/Modal'
import { ErrorState } from '../../components/data/ErrorState'
import { Skeleton } from '../../components/data/Skeleton'
import { Table } from '../../components/data/Table'
import type { Node } from '../../api/contracts/node'
import type { Route } from '../../api/contracts/route'
import type { Service } from '../../api/contracts/service'
import type { VersionItem } from '../../api/contracts/version'
import { formatDateTime } from '../../lib/time'
import { decodeVersionItem } from './snapshot'
import {
  enabledStatusMeta,
  matchTypeLabel,
  methodsLabel,
  nodeHealthStatusMeta,
  overloadStrategyLabel,
  protocolLabel,
  versionStatusMeta,
} from './status'
import { useVersion } from './useVersions'

export interface VersionDetailModalProps {
  versionId: number
  onClose: () => void
}

const EMPTY_ITEMS: VersionItem[] = []

interface DecodedItem {
  item: VersionItem
  service?: Service
  node?: Node
  route?: Route
  error?: string
}

export function VersionDetailModal({
  versionId,
  onClose,
}: VersionDetailModalProps) {
  const versionQuery = useVersion(versionId)
  const detail = versionQuery.data
  const items = detail?.items ?? EMPTY_ITEMS

  const decodedItems = useMemo<DecodedItem[]>(() => {
    return items.map((item) => {
      try {
        const decoded = decodeVersionItem(item)
        if (decoded.type === 'service') {
          return { item, service: decoded.data }
        }
        if (decoded.type === 'node') {
          return { item, node: decoded.data }
        }
        if (decoded.type === 'route') {
          return { item, route: decoded.data }
        }
        return { item }
      } catch {
        return { item, error: '快照解析失败' }
      }
    })
  }, [items])

  const services = decodedItems.filter(
    (entry): entry is DecodedItem & { service: Service } =>
      entry.service !== undefined,
  )
  const nodes = decodedItems.filter(
    (entry): entry is DecodedItem & { node: Node } => entry.node !== undefined,
  )
  const routes = decodedItems.filter(
    (entry): entry is DecodedItem & { route: Route } =>
      entry.route !== undefined,
  )
  const decodeErrors = decodedItems.filter((entry) => entry.error)
  const unknownCount = decodedItems.filter(
    (entry) =>
      !entry.error &&
      entry.service === undefined &&
      entry.node === undefined &&
      entry.route === undefined,
  ).length

  return (
    <Modal
      open
      size="lg"
      title={`版本快照详情 #${versionId}`}
      description="版本快照只读；回滚会复制该快照生成一个新版本。"
      onClose={onClose}
      footer={
        <Button onClick={onClose}>关闭</Button>
      }
    >
      {versionQuery.isPending ? (
        <div className="stack">
          <Skeleton height={18} width="34%" />
          <Skeleton height={12} width="52%" />
          <Skeleton height={12} width="46%" />
          <Skeleton height={160} />
        </div>
      ) : versionQuery.isError || !detail ? (
        <ErrorState
          title="版本快照加载失败"
          error={versionQuery.error ?? new Error('管理面没有返回版本数据。')}
          actions={
            <Button
              size="sm"
              onClick={() => void versionQuery.refetch()}
              loading={versionQuery.isFetching}
            >
              重试
            </Button>
          }
        />
      ) : (
        <div className="stack">
          <dl className="kv-list">
            <dt>版本号</dt>
            <dd className="text-mono">v{detail.version.version}</dd>
            <dt>状态</dt>
            <dd>
              <Badge tone={versionStatusMeta(detail.version.status).tone}>
                {versionStatusMeta(detail.version.status).label}
              </Badge>
            </dd>
            <dt>配置哈希</dt>
            <dd className="text-mono">
              {detail.version.config_hash ?? '—'}
            </dd>
            <dt>发布时间</dt>
            <dd>{formatDateTime(detail.version.published_at)}</dd>
            <dt>创建时间</dt>
            <dd>{formatDateTime(detail.version.created_at)}</dd>
            <dt>快照条目</dt>
            <dd>
              {detail.items.length} 条（服务 {services.length} · 节点{' '}
              {nodes.length} · 路由 {routes.length}）
            </dd>
          </dl>

          {decodeErrors.length > 0 ? (
            <div className="callout callout--danger" role="alert">
              <span>
                {decodeErrors.length} 条快照无法解析，已在下方按资源类型跳过。
              </span>
            </div>
          ) : null}

          {unknownCount > 0 ? (
            <div className="callout callout--info">
              <span>
                另有 {unknownCount} 条未知资源类型的快照未展示；控制台当前只认识
                service / node / route。
              </span>
            </div>
          ) : null}

          <section>
            <h3 className="section-title">服务（{services.length}）</h3>
            {services.length === 0 ? (
              <p className="page-note">该版本快照不包含服务。</p>
            ) : (
              <Table minWidth={760}>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>名称</th>
                    <th>协议</th>
                    <th>状态</th>
                    <th>超时（建连 / 请求）</th>
                    <th>过载策略</th>
                    <th>服务级并发</th>
                  </tr>
                </thead>
                <tbody>
                  {services.map(({ item, service }) => (
                    <tr key={item.id}>
                      <td className="text-mono">{service.id}</td>
                      <td className="table-strong">{service.name}</td>
                      <td>{protocolLabel(service.protocol)}</td>
                      <td>
                        <Badge
                          tone={enabledStatusMeta(service.enabled).tone}
                        >
                          {enabledStatusMeta(service.enabled).label}
                        </Badge>
                      </td>
                      <td className="text-mono">
                        {service.connect_timeout_ms} /{' '}
                        {service.request_timeout_ms} ms
                      </td>
                      <td>
                        {overloadStrategyLabel(service.overload_strategy)}
                      </td>
                      <td className="text-mono">
                        {service.max_concurrency > 0
                          ? service.max_concurrency
                          : '不限制'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </section>

          <section>
            <h3 className="section-title">节点（{nodes.length}）</h3>
            {nodes.length === 0 ? (
              <p className="page-note">该版本快照不包含节点。</p>
            ) : (
              <Table minWidth={700}>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>服务 ID</th>
                    <th>地址</th>
                    <th>权重</th>
                    <th>状态</th>
                    <th>health_status</th>
                  </tr>
                </thead>
                <tbody>
                  {nodes.map(({ item, node }) => (
                    <tr key={item.id}>
                      <td className="text-mono">{node.id}</td>
                      <td className="text-mono">{node.service_id}</td>
                      <td className="text-mono">{node.address}</td>
                      <td className="text-mono">{node.weight}</td>
                      <td>
                        <Badge tone={enabledStatusMeta(node.enabled).tone}>
                          {enabledStatusMeta(node.enabled).label}
                        </Badge>
                      </td>
                      <td>
                        <Badge tone={nodeHealthStatusMeta(node.health_status).tone}>
                          {nodeHealthStatusMeta(node.health_status).label}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </section>

          <section>
            <h3 className="section-title">路由（{routes.length}）</h3>
            {routes.length === 0 ? (
              <p className="page-note">该版本快照不包含路由。</p>
            ) : (
              <Table minWidth={860}>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>名称</th>
                    <th>服务 ID</th>
                    <th>Host</th>
                    <th>路径 / 匹配</th>
                    <th>方法</th>
                    <th>状态</th>
                    <th>路由级并发</th>
                  </tr>
                </thead>
                <tbody>
                  {routes.map(({ item, route }) => (
                    <tr key={item.id}>
                      <td className="text-mono">{route.id}</td>
                      <td className="table-strong">{route.name}</td>
                      <td className="text-mono">{route.service_id}</td>
                      <td className="text-mono">{route.host || '任意'}</td>
                      <td>
                        <span className="text-mono">
                          {route.path_pattern}
                        </span>
                        <div className="table-muted">
                          {matchTypeLabel(route.path_match_type)}
                        </div>
                      </td>
                      <td className="text-mono">
                        {methodsLabel(route.methods)}
                      </td>
                      <td>
                        <Badge tone={enabledStatusMeta(route.enabled).tone}>
                          {enabledStatusMeta(route.enabled).label}
                        </Badge>
                      </td>
                      <td className="text-mono">
                        {route.max_concurrency > 0
                          ? route.max_concurrency
                          : '不限制'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </section>

          <p className="table-footnote">
            条目 {items.length} 条；快照来自发布时的 enabled 资源，后续修改管理面配置不会改变历史版本。
          </p>
        </div>
      )}
    </Modal>
  )
}
