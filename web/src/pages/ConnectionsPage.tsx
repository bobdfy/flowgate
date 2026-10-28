import { RefreshCw, Trash2 } from 'lucide-react'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Card, CardBody, CardHeader } from '../components/ui/Card'
import { PageHeader } from '../components/layout/PageHeader'
import { SourceBadge, type SourceState } from '../components/ui/SourceBadge'
import {
  capabilities,
  isMockMode,
  type CapabilityState,
} from '../lib/capabilities'
import { env } from '../lib/env'
import { getErrorMessage } from '../lib/errorMessage'
import { formatDateTime } from '../lib/time'
import { connectionStatusMeta } from '../features/connection/status'
import { useConnection } from '../features/connection/useConnection'
import { useRuntimeAI, useRuntimeNodes } from '../features/runtime/useRuntime'

function capabilitySourceState(state: CapabilityState): SourceState {
  return state === 'available' ? 'connected' : 'unavailable'
}

function capabilityLabel(state: CapabilityState): string {
  if (state === 'available') return '已接入'
  if (state === 'pending') return '待后端接口'
  return '未接入'
}

export function ConnectionsPage() {
  const {
    status,
    isChecking,
    error,
    health,
    lastCheckedAt,
    keyConfigured,
    rememberKey,
    retry,
    clearKey,
  } = useConnection()
  const meta = connectionStatusMeta[status]

  const runtimeNodesQuery = useRuntimeNodes(!isMockMode)
  const runtimeAIQuery = useRuntimeAI(!isMockMode)
  const runtimeNodes = runtimeNodesQuery.data?.nodes ?? []
  const healthyNodes = runtimeNodes.filter((node) => node.healthy).length
  const openNodes = runtimeNodes.filter((node) => node.state === 'open').length
  const runtimeUnavailable = !isMockMode &&
    (runtimeNodesQuery.isError || runtimeAIQuery.isError)
  const runtimeMeta = runtimeAIQuery.data ?? runtimeNodesQuery.data
  const runtimeStale = runtimeMeta?.stale === true

  const handleClearKey = () => {
    const confirmed = window.confirm(
      '清除后需要重新输入管理密钥才能访问管理面，是否继续？',
    )
    if (confirmed) {
      clearKey()
    }
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="系统"
        title="接入状态"
        description={
          isMockMode
            ? '演示模式：所有健康检查与管理操作都由浏览器内 Mock 适配层响应。'
            : '检查管理面连接、密钥存储方式与各数据源的接入边界。'
        }
        actions={
          <Button
            icon={<RefreshCw size={14} aria-hidden="true" />}
            loading={isChecking}
            onClick={() => void retry()}
          >
            重新检测
          </Button>
        }
      />

      {isMockMode ? (
        <div className="callout callout--warning" role="status">
          <span>
            当前为演示模式。健康检查、服务、路由、租户、API Key 与 AI
            配置全部由浏览器内 Mock 适配层返回，不会访问真实管理面或 gateway；
            刷新页面后数据重置。
          </span>
        </div>
      ) : null}

      <div className="connections-grid">
        <Card>
          <CardHeader
            title={isMockMode ? '管理面（演示）' : '管理面'}
            description={
              isMockMode
                ? 'Mock 适配层 · 配置 API 模拟'
                : 'admin 控制面 · 配置 API'
            }
            actions={
              <Badge tone={isMockMode ? 'warning' : meta.badgeTone} dot>
                {isMockMode ? '演示模式' : meta.label}
              </Badge>
            }
          />
          <CardBody>
            <dl className="kv-list">
              <dt>{isMockMode ? '配置地址（未使用）' : '接入地址'}</dt>
              <dd>
                <code>{env.adminBaseUrl}</code>
              </dd>
              <dt>{isMockMode ? 'Mock 健康响应' : '服务端健康'}</dt>
              <dd>{health?.status ?? '—'}</dd>
              <dt>最近检查</dt>
              <dd>{formatDateTime(lastCheckedAt)}</dd>
              <dt>管理密钥</dt>
              <dd>
                {isMockMode
                  ? '不适用（Mock 不校验）'
                  : keyConfigured
                    ? '已配置'
                    : '未配置'}
              </dd>
              <dt>密钥存储</dt>
              <dd>
                {isMockMode
                  ? '不适用（不读取密钥）'
                  : keyConfigured
                    ? rememberKey
                      ? '当前标签页 sessionStorage'
                      : '仅当前页面内存'
                    : '无'}
              </dd>
            </dl>

            {error ? (
              <div className="callout callout--warning" style={{ marginTop: 16 }}>
                <span>{getErrorMessage(error)}</span>
              </div>
            ) : null}

            <div className="connection-actions">
              {keyConfigured ? (
                <Button
                  variant="danger"
                  size="sm"
                  icon={<Trash2 size={14} aria-hidden="true" />}
                  onClick={handleClearKey}
                  disabled={isChecking}
                >
                  清除本地密钥
                </Button>
              ) : null}
            </div>
          </CardBody>
        </Card>

        <Card>
          <CardHeader
            title="数据源能力"
            description="已实现、未实现与待接入的能力区分"
          />
          <CardBody>
            <div className="capability-list">
              {capabilities.map((capability) => (
                <div className="capability-row" key={capability.key}>
                  <div className="capability-row__copy">
                    <div className="capability-row__name">{capability.label}</div>
                    <div className="capability-row__note">{capability.detail}</div>
                  </div>
                  <SourceBadge
                    label={capabilityLabel(capability.state)}
                    state={capabilitySourceState(capability.state)}
                    title={capability.detail}
                  />
                </div>
              ))}
            </div>
          </CardBody>
        </Card>
      </div>

      <Card>
        <CardHeader
          title="gateway 运行状态"
          description="节点健康与 AI 配置加载结果由 gateway 每 5 秒写入 Redis，admin 只读转发。"
          actions={
            <Badge
              tone={
                isMockMode
                  ? 'neutral'
                  : runtimeUnavailable
                    ? 'neutral'
                    : runtimeStale
                      ? 'warning'
                      : 'success'
              }
              dot
            >
              {isMockMode
                ? '演示模式'
                : runtimeUnavailable
                  ? '未上报'
                  : runtimeStale
                    ? '上报已过期'
                    : '在线'}
            </Badge>
          }
        />
        <CardBody>
          {isMockMode ? (
            <div className="callout callout--info">
              <span>
                演示模式没有真实 gateway，节点健康与 AI 加载状态不展示样例数据。
              </span>
            </div>
          ) : runtimeUnavailable ? (
            <div className="callout callout--warning">
              <span>
                未获取到 gateway 上报：{getErrorMessage(
                  runtimeNodesQuery.error ?? runtimeAIQuery.error,
                )}。确认 gateway 已启动、admin 配了 REDIS_ADDR。
              </span>
            </div>
          ) : (
            <dl className="kv-list">
              <dt>上报实例</dt>
              <dd className="text-mono">{runtimeMeta?.instance ?? '—'}</dd>
              <dt>最近上报</dt>
              <dd>
                {runtimeMeta
                  ? `${formatDateTime(runtimeMeta.published_at)} · ${runtimeMeta.age_seconds} 秒前`
                  : '—'}
              </dd>
              <dt>节点健康</dt>
              <dd className="text-mono">
                {healthyNodes} / {runtimeNodes.length} 健康
                {openNodes > 0 ? ` · ${openNodes} 个熔断中` : ''}
              </dd>
              <dt>AI 配置版本</dt>
              <dd className="text-mono">
                {runtimeAIQuery.data
                  ? `v${runtimeAIQuery.data.ai.version}`
                  : '—'}
              </dd>
              <dt>AI 模型 / 提供商</dt>
              <dd className="text-mono">
                {runtimeAIQuery.data
                  ? `${runtimeAIQuery.data.ai.model_count} 个模型 · ${runtimeAIQuery.data.ai.provider_count} 个提供商`
                  : '—'}
              </dd>
              <dt>AI 最近加载</dt>
              <dd>
                {runtimeAIQuery.data?.ai.loaded_at
                  ? formatDateTime(runtimeAIQuery.data.ai.loaded_at)
                  : '—'}
              </dd>
              <dt>AI 最近错误</dt>
              <dd>
                {runtimeAIQuery.data?.ai.last_error
                  ? runtimeAIQuery.data.ai.last_error
                  : '无'}
              </dd>
            </dl>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader
          title="接入约定"
          description="控制台直连管理 API 的边界条件"
        />
        <CardBody>
          <dl className="kv-list">
            <dt>管理 API</dt>
            <dd>
              <code>{env.adminBaseUrl}</code> · 43 个已注册路由
            </dd>
            <dt>鉴权</dt>
            <dd>
              <code>X-Admin-Key</code> 请求头；是否启用由 admin 的{' '}
              <code>-admin-key</code> 启动参数决定
            </dd>
            <dt>开发代理</dt>
            <dd>
              Vite 将 <code>/api</code> 转发到 <code>http://127.0.0.1:8092</code>
            </dd>
            <dt>指标端点</dt>
            <dd>
              {isMockMode ? (
                '演示模式没有真实 gateway，不提供指标入口'
              ) : (
                <>
                  <a href={env.metricsUrl} target="_blank" rel="noreferrer">
                    {env.metricsUrl}
                  </a>{' '}
                  · Prometheus 文本格式，新标签打开；侧边栏也有「运行指标」入口
                </>
              )}
            </dd>
            <dt>错误请求</dt>
            <dd>
              gateway 把失败请求写入 Redis 窗口（最多 500 条），
              经 <code>/api/v1/logs/errors</code> 查询；全量 access log 仍只输出到
              stdout
            </dd>
          </dl>
          <div className="callout callout--info" style={{ marginTop: 16 }}>
            <span>
              管理密钥只保存在内存或当前标签页的 sessionStorage，不使用
              localStorage，也不会拼接到 URL 或输出到控制台日志。
            </span>
          </div>
        </CardBody>
      </Card>
    </div>
  )
}
