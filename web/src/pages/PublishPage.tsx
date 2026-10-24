import { useState } from 'react'
import { RefreshCw, Rocket, RotateCcw } from 'lucide-react'
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
import type { Version } from '../api/contracts/version'
import { getErrorMessage } from '../lib/errorMessage'
import { formatDateTime } from '../lib/time'
import { usePagination } from '../lib/usePagination'
import { VersionDetailModal } from '../features/gateway/VersionDetailModal'
import { versionStatusMeta } from '../features/gateway/status'
import {
  usePublish,
  useRollbackVersion,
  useVersions,
} from '../features/gateway/useVersions'

const EMPTY_VERSIONS: Version[] = []

function shortHash(hash: string | null): string {
  if (!hash) {
    return '—'
  }
  return hash.length > 12 ? `${hash.slice(0, 12)}…` : hash
}

export function PublishPage() {
  const versionsQuery = useVersions()
  const publishMutation = usePublish()
  const rollbackMutation = useRollbackVersion()

  const [publishOpen, setPublishOpen] = useState(false)
  const [rollbackTarget, setRollbackTarget] = useState<Version | null>(null)
  const [detailVersionId, setDetailVersionId] = useState<number | null>(null)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)

  const versions = versionsQuery.data ?? EMPTY_VERSIONS
  // gateway 取 status='published' 且 version 最大的一条；列表按版本号倒序。
  const latest =
    versions.find((version) => version.status === 'published') ?? null
  const pagination = usePagination(versions, 'versions')

  const closePublishDialog = () => {
    if (publishMutation.isPending) {
      return
    }
    setPublishOpen(false)
    publishMutation.reset()
  }

  const closeRollbackDialog = () => {
    if (rollbackMutation.isPending) {
      return
    }
    setRollbackTarget(null)
    rollbackMutation.reset()
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="流量配置"
        title="发布中心"
        description="把当前可编辑配置冻结为版本快照，并查看历史版本与回滚结果。"
        actions={
          <>
            <Button
              icon={<RefreshCw size={14} aria-hidden="true" />}
              loading={versionsQuery.isFetching}
              onClick={() => void versionsQuery.refetch()}
            >
              刷新
            </Button>
            <Button
              variant="primary"
              icon={<Rocket size={14} aria-hidden="true" />}
              onClick={() => {
                setSuccessMessage(null)
                setPublishOpen(true)
              }}
            >
              发布当前配置
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
          <div className="stat-card__label">当前生效版本</div>
          <div className="stat-card__value">
            {latest ? `v${latest.version}` : '尚无版本'}
          </div>
          <div className="stat-card__meta">
            {latest
              ? `发布于 ${formatDateTime(latest.published_at)}`
              : 'gateway 的路由表为空'}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-card__label">版本总数</div>
          <div className="stat-card__value">{versions.length}</div>
          <div className="stat-card__meta">
            所有记录均由发布或回滚产生
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-card__label">最新配置哈希</div>
          <div className="stat-card__value stat-card__value--mono">
            {latest ? shortHash(latest.config_hash) : '—'}
          </div>
          <div className="stat-card__meta">完整值见版本详情</div>
        </div>
      </div>

      <div className="callout callout--info">
        <span>
          发布读取当前可编辑配置并生成不可变快照，只有 enabled
          的服务、节点与路由会进入快照。每条启用路由必须指向启用服务且至少有一个启用节点，否则发布校验失败且不会创建版本。gateway
          每 5 秒检查最新 published 版本，通常数秒内加载；控制台没有运行时加载回执。
        </span>
      </div>

      <Card>
        <div className="table-toolbar">
          <div className="table-toolbar__filters">
            <span className="table-strong">版本历史</span>
            <span className="table-muted">按版本号倒序</span>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {versionsQuery.isPending
              ? '正在加载…'
              : versionsQuery.isError
                ? '版本列表加载失败'
                : `共 ${versions.length} 个版本`}
          </div>
        </div>

        {versionsQuery.isPending ? (
          <TableSkeleton columns={6} rows={5} minWidth={900} />
        ) : versionsQuery.isError ? (
          <div className="table-state">
            <ErrorState
              title="版本列表加载失败"
              error={versionsQuery.error}
              onRetry={() => void versionsQuery.refetch()}
            />
          </div>
        ) : versions.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title="还没有发布版本"
              description="没有 published 版本时，gateway 启动后路由表为空。发布成功后会生成第一条版本记录。"
              actions={
                <Button
                  variant="primary"
                  size="sm"
                  icon={<Rocket size={14} aria-hidden="true" />}
                  onClick={() => setPublishOpen(true)}
                >
                  发布当前配置
                </Button>
              }
            />
          </div>
        ) : (
          <Table minWidth={900}>
            <thead>
              <tr>
                <th>版本</th>
                <th>状态</th>
                <th>配置哈希</th>
                <th>发布时间</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {pagination.pageItems.map((version) => {
                const statusMeta = versionStatusMeta(version.status)
                const isLatest = latest !== null && version.id === latest.id
                return (
                  <tr key={version.id}>
                    <td>
                      <span className="table-strong text-mono">
                        v{version.version}
                      </span>
                      {isLatest ? (
                        <div className="table-muted">当前生效</div>
                      ) : (
                        <div className="table-muted">ID {version.id}</div>
                      )}
                    </td>
                    <td>
                      <Badge tone={statusMeta.tone}>{statusMeta.label}</Badge>
                    </td>
                    <td
                      className="text-mono table-muted"
                      title={version.config_hash ?? undefined}
                    >
                      {shortHash(version.config_hash)}
                    </td>
                    <td className="table-muted">
                      {formatDateTime(version.published_at)}
                    </td>
                    <td className="table-muted">
                      {formatDateTime(version.created_at)}
                    </td>
                    <td>
                      <div className="table-actions">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setDetailVersionId(version.id)}
                        >
                          查看快照
                        </Button>
                        {isLatest ? (
                          <span className="table-muted">当前版本</span>
                        ) : (
                          <Button
                            size="sm"
                            variant="ghost"
                            icon={<RotateCcw size={13} aria-hidden="true" />}
                            onClick={() => {
                              setSuccessMessage(null)
                              setRollbackTarget(version)
                            }}
                          >
                            回滚
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
        )}
        {!versionsQuery.isPending &&
        !versionsQuery.isError &&
        versions.length > 0 ? (
          <Pagination pagination={pagination} itemLabel="个版本" />
        ) : null}
      </Card>

      <p className="page-note">
        回滚采用 copy-forward：以历史版本快照创建一条新的 published
        版本，不会修改当前服务、节点、路由的可编辑配置。后端目前没有草稿态，发布记录统一为
        published。
      </p>

      <ConfirmDialog
        open={publishOpen}
        title="发布当前配置"
        description={
          <>
            <p>
              将当前服务、节点与路由配置冻结为一个新的 published
              版本。此操作会改变 gateway 后续加载的路由表。
            </p>
            <p className="confirm__warning">
              发布前会校验启用路由的上游可用性；校验失败不会创建版本。
            </p>
          </>
        }
        confirmLabel="发布配置"
        loading={publishMutation.isPending}
        error={
          publishMutation.error ? getErrorMessage(publishMutation.error) : null
        }
        onConfirm={() => {
          publishMutation.mutate(undefined, {
            onSuccess: (version) => {
              setPublishOpen(false)
              setSuccessMessage(
                `已创建并发布版本 v${version.version}。gateway 通常会在数秒内加载该版本。`,
              )
            },
          })
        }}
        onClose={closePublishDialog}
      />

      <ConfirmDialog
        open={rollbackTarget !== null}
        title="回滚版本"
        description={
          rollbackTarget ? (
            <>
              <p>
                将以版本 <strong>v{rollbackTarget.version}</strong> 的快照创建一个新的
                published 版本。
              </p>
              <p className="confirm__warning">
                当前服务、节点与路由的可编辑配置不会被改写，但 gateway
                会加载复制出的历史快照。本次操作会生成新的版本号，无法撤销这次回滚记录。
              </p>
            </>
          ) : null
        }
        confirmLabel="回滚到此版本"
        loading={rollbackMutation.isPending}
        error={
          rollbackMutation.error
            ? getErrorMessage(rollbackMutation.error)
            : null
        }
        onConfirm={() => {
          if (!rollbackTarget) {
            return
          }
          rollbackMutation.mutate(rollbackTarget.id, {
            onSuccess: (version) => {
              setRollbackTarget(null)
              setSuccessMessage(
                `已基于历史快照创建版本 v${version.version}。gateway 通常会在数秒内加载该版本。`,
              )
            },
          })
        }}
        onClose={closeRollbackDialog}
      />

      {detailVersionId !== null ? (
        <VersionDetailModal
          key={detailVersionId}
          versionId={detailVersionId}
          onClose={() => setDetailVersionId(null)}
        />
      ) : null}
    </div>
  )
}
