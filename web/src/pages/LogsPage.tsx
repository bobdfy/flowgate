import { useMemo, useState } from 'react'
import { RefreshCw, Search } from 'lucide-react'
import { Badge, type BadgeTone } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Card } from '../components/ui/Card'
import { EmptyState } from '../components/data/EmptyState'
import { ErrorState } from '../components/data/ErrorState'
import { Pagination } from '../components/data/Pagination'
import { Table } from '../components/data/Table'
import { TableSkeleton } from '../components/data/TableSkeleton'
import { PageHeader } from '../components/layout/PageHeader'
import type {
  ErrorKind,
  ErrorLogEntry,
  ErrorLogQuery,
} from '../api/contracts/errorLog'
import { isMockMode } from '../lib/capabilities'
import { formatDateTime } from '../lib/time'
import { useDebouncedValue } from '../lib/useDebouncedValue'
import { usePagination } from '../lib/usePagination'
import { useErrorLogs } from '../features/logs/useErrorLogs'

type RangeKey = '15m' | '1h' | '6h' | 'all'

const RANGE_OPTIONS: { value: RangeKey; label: string }[] = [
  { value: '15m', label: '最近 15 分钟' },
  { value: '1h', label: '最近 1 小时' },
  { value: '6h', label: '最近 6 小时' },
  { value: 'all', label: '窗口内全部' },
]

const KIND_META: Record<ErrorKind, { label: string; tone: BadgeTone }> = {
  upstream_error: { label: '上游错误', tone: 'danger' },
  timeout: { label: '超时', tone: 'warning' },
  overload: { label: '过载 / 限流', tone: 'warning' },
  circuit_open: { label: '熔断中', tone: 'danger' },
  stream_terminated: { label: '流中断', tone: 'danger' },
  auth_failed: { label: '鉴权失败', tone: 'neutral' },
  internal_error: { label: '网关错误', tone: 'danger' },
}

const KIND_OPTIONS: { value: 'all' | ErrorKind; label: string }[] = [
  { value: 'all', label: '全部分类' },
  ...(Object.keys(KIND_META) as ErrorKind[]).map((kind) => ({
    value: kind,
    label: KIND_META[kind].label,
  })),
]

function kindMeta(kind: string): { label: string; tone: BadgeTone } {
  return (
    KIND_META[kind as ErrorKind] ?? { label: kind || '未知', tone: 'neutral' }
  )
}

function statusTone(status: number): BadgeTone {
  if (status >= 500) return 'danger'
  if (status === 429) return 'warning'
  if (status === 401 || status === 403) return 'neutral'
  return 'info'
}

const EMPTY_ENTRIES: ErrorLogEntry[] = []

export function LogsPage() {
  const [range, setRange] = useState<RangeKey>('1h')
  const [kind, setKind] = useState<'all' | ErrorKind>('all')
  const [routeInput, setRouteInput] = useState('')
  const [requestIdInput, setRequestIdInput] = useState('')

  const route = useDebouncedValue(routeInput.trim())
  const requestId = useDebouncedValue(requestIdInput.trim())

  const query = useMemo<ErrorLogQuery>(
    () => ({
      kind: kind === 'all' ? undefined : kind,
      route: route || undefined,
      request_id: requestId || undefined,
      since: range === 'all' ? undefined : range,
    }),
    [kind, route, requestId, range],
  )

  const logsQuery = useErrorLogs(query, !isMockMode)
  const entries = logsQuery.data?.entries ?? EMPTY_ENTRIES

  // 筛选走服务端（since / kind / route / request_id），分页只在已返回的窗口内做。
  const pagination = usePagination(
    entries,
    `${range}\u0000${kind}\u0000${route}\u0000${requestId}`,
  )

  const hasFilter =
    range !== 'all' || kind !== 'all' || route !== '' || requestId !== ''

  const clearFilters = () => {
    setRange('all')
    setKind('all')
    setRouteInput('')
    setRequestIdInput('')
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="运行"
        title="错误请求"
        description="最近失败请求的汇总视图：同一种错误（方法 + 路由 + 状态码 + 分类）合并成一行并累计次数，回答「哪几类请求挂了、挂在哪个环节」；单条详情仍去 gateway 日志按 Request ID 检索。"
        actions={
          <Button
            icon={<RefreshCw size={14} aria-hidden="true" />}
            loading={logsQuery.isFetching}
            onClick={() => void logsQuery.refetch()}
            disabled={isMockMode}
          >
            刷新
          </Button>
        }
      />

      {isMockMode ? (
        <div className="callout callout--info" role="status">
          <span>
            演示模式没有真实 gateway，错误窗口不展示样例数据。切到真实数据后，
            本页读取 gateway 写入 Redis 的最近失败请求。
          </span>
        </div>
      ) : null}

      <Card>
        <div className="table-toolbar">
          <div className="table-toolbar__filters">
            <select
              className="field__input table-filter"
              value={range}
              onChange={(event) => setRange(event.target.value as RangeKey)}
              aria-label="按时间范围筛选"
              disabled={isMockMode}
            >
              {RANGE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <select
              className="field__input table-filter"
              value={kind}
              onChange={(event) =>
                setKind(event.target.value as 'all' | ErrorKind)
              }
              aria-label="按错误分类筛选"
              disabled={isMockMode}
            >
              {KIND_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <div className="table-search">
              <Search
                size={14}
                className="table-search__icon"
                aria-hidden="true"
              />
              <input
                className="field__input table-search__input"
                type="search"
                value={routeInput}
                onChange={(event) => setRouteInput(event.target.value)}
                placeholder="按路由筛选"
                aria-label="按路由筛选"
                disabled={isMockMode}
              />
            </div>
            <div className="table-search">
              <Search
                size={14}
                className="table-search__icon"
                aria-hidden="true"
              />
              <input
                className="field__input table-search__input"
                type="search"
                value={requestIdInput}
                onChange={(event) => setRequestIdInput(event.target.value)}
                placeholder="按 Request ID 精确查找"
                aria-label="按 Request ID 查找"
                disabled={isMockMode}
              />
            </div>
          </div>
          <div className="table-toolbar__meta" aria-live="polite">
            {isMockMode
              ? '演示模式'
              : logsQuery.isPending
                ? '正在加载…'
                : logsQuery.isError
                  ? '加载失败'
                  : hasFilter
                    ? `命中 ${entries.length} 种错误`
                    : `窗口内共 ${entries.length} 种错误 · 上限 ${logsQuery.data?.capacity ?? '—'} 种`}
          </div>
        </div>

        {isMockMode ? (
          <div className="table-state">
            <EmptyState
              title="演示模式不展示错误窗口"
              description="错误看板依赖 gateway 写入的真实 Redis 数据。"
            />
          </div>
        ) : logsQuery.isPending ? (
          <TableSkeleton columns={10} rows={5} minWidth={1150} />
        ) : logsQuery.isError ? (
          <div className="table-state">
            <ErrorState
              error={logsQuery.error}
              onRetry={() => void logsQuery.refetch()}
            />
          </div>
        ) : entries.length === 0 ? (
          <div className="table-state">
            <EmptyState
              title={hasFilter ? '没有匹配的错误请求' : '窗口内没有失败请求'}
              description={
                hasFilter
                  ? '试试放宽时间范围或清除筛选。'
                  : '只记录 5xx、401/403 与 429；404 等客户端错误不计入。'
              }
              actions={
                hasFilter ? (
                  <Button size="sm" onClick={clearFilters}>
                    清除筛选
                  </Button>
                ) : undefined
              }
            />
          </div>
        ) : (
          <Table minWidth={1150}>
            <thead>
              <tr>
                <th>最近发生</th>
                <th>次数</th>
                <th>分类</th>
                <th>状态</th>
                <th>路由</th>
                <th>耗时</th>
                <th>实例</th>
                <th>租户 / Key</th>
                <th>Request ID</th>
                <th>详情</th>
              </tr>
            </thead>
            <tbody>
              {pagination.pageItems.map((entry) => {
                const meta = kindMeta(entry.kind)
                return (
                  <tr key={`${entry.method}-${entry.route}-${entry.status}-${entry.kind}`}>
                    <td className="table-muted">{formatDateTime(entry.ts)}</td>
                    <td className="text-mono">{entry.count}</td>
                    <td>
                      <Badge tone={meta.tone}>{meta.label}</Badge>
                    </td>
                    <td>
                      <Badge tone={statusTone(entry.status)}>
                        {entry.status}
                      </Badge>
                    </td>
                    <td className="text-mono">{entry.route}</td>
                    <td className="text-mono">{entry.duration_ms} ms</td>
                    <td className="table-muted text-mono">{entry.instance}</td>
                    <td className="table-muted text-mono">
                      {entry.tenant_id
                        ? `${entry.tenant_id} / ${entry.key_id ?? '—'}`
                        : '—'}
                    </td>
                    <td>
                      <code
                        className="text-mono"
                        title={`在 gateway 日志中搜索：docker logs flowgate-gateway | grep ${entry.request_id}`}
                      >
                        {entry.request_id || '—'}
                      </code>
                    </td>
                    <td className="table-muted" title={entry.detail}>
                      {entry.detail || '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
        )}

        {!isMockMode &&
        !logsQuery.isPending &&
        !logsQuery.isError &&
        entries.length > 0 ? (
          <Pagination pagination={pagination} itemLabel="种错误" />
        ) : null}
      </Card>

      <p className="page-note">
        每一行是一种错误（方法 + 路由 + 状态码 + 分类），「次数」是窗口内的累计值，
        其余字段取自最近一次发生；按最近发生时间倒序，最久没发生的会被挤出 500 种的窗口。
        这里不是完整日志存储 —— 只记录方法、路由、状态码、耗时、Request ID、租户与
        Key 的数值 ID 和截断后的错误摘要，不记录请求头、请求体与 API Key 原文。
      </p>
    </div>
  )
}
