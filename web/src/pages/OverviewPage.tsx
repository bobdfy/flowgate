import { Link } from 'react-router-dom'
import { ArrowRight, Info, RefreshCw } from 'lucide-react'
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
import { formatDateTime } from '../lib/time'
import { connectionStatusMeta } from '../features/connection/status'
import { useConnection } from '../features/connection/useConnection'

function sourceStateForCapability(state: CapabilityState): SourceState {
  return state === 'available' ? 'connected' : 'unavailable'
}

function sourceLabelForCapability(state: CapabilityState): string {
  if (state === 'available') return '已接入'
  if (state === 'pending') return '待后端接口'
  return '未接入'
}

export function OverviewPage() {
  const {
    status,
    isChecking,
    health,
    lastCheckedAt,
    keyConfigured,
    retry,
  } = useConnection()
  const meta = connectionStatusMeta[status]

  return (
    <div className="page">
      <PageHeader
        eyebrow="运行"
        title="运行总览"
        description={
          isMockMode
            ? '演示模式：管理配置来自浏览器内 Mock 适配层，请勿据此判断真实运行状态。'
            : '确认控制台的数据源接入状态。运行时指标以只读外链打开，控制台不展示聚合图表。'
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

      <div className="overview-grid">
        <Card>
          <CardHeader
            title={isMockMode ? '演示数据源' : '管理面连接'}
            description={
              isMockMode
                ? 'Mock 适配层的健康响应，不代表真实管理面'
                : '控制面配置 API 的实时可用性'
            }
            actions={
              <Badge tone={isMockMode ? 'warning' : meta.badgeTone} dot>
                {isMockMode ? '演示模式' : meta.label}
              </Badge>
            }
          />
          <CardBody>
            <dl className="kv-list">
              <dt>{isMockMode ? 'Mock 健康响应' : '服务端健康'}</dt>
              <dd>{health?.status ?? '—'}</dd>
              <dt>{isMockMode ? '配置地址（未使用）' : '接入地址'}</dt>
              <dd>
                <code>{env.adminBaseUrl}</code>
              </dd>
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
            </dl>
            <div className="connection-actions">
              <Link
                to="/system/connections"
                className="button button--ghost button--sm"
              >
                接入详情
                <ArrowRight size={14} aria-hidden="true" />
              </Link>
            </div>
          </CardBody>
        </Card>

        <Card>
          <CardHeader
            title="控制台环境"
            description="由构建时环境变量决定，不在运行时修改"
          />
          <CardBody>
            <dl className="kv-list">
              <dt>环境标识</dt>
              <dd>{env.environmentLabel}</dd>
              <dt>数据模式</dt>
              <dd>
                {isMockMode
                  ? '演示（浏览器内 Mock 适配层）'
                  : '实时（真实管理面）'}
              </dd>
              <dt>指标端点</dt>
              <dd>
                {isMockMode ? (
                  <code>演示模式不适用</code>
                ) : (
                  <a href={env.metricsUrl} target="_blank" rel="noreferrer">
                    {env.metricsUrl}
                  </a>
                )}
              </dd>
              <dt>控制台版本</dt>
              <dd>0.1.0 · 阶段 4</dd>
            </dl>
          </CardBody>
        </Card>
      </div>

      <Card>
        <CardHeader
          title="数据源接入状态"
          description="能力清单来自当前后端实现，不代表历史可用性"
        />
        <CardBody>
          <div className="source-grid">
            {capabilities.map((capability) => (
              <div className="source-card" key={capability.key}>
                <div className="source-card__top">
                  <span className="source-card__name">{capability.label}</span>
                  <SourceBadge
                    label={sourceLabelForCapability(capability.state)}
                    state={sourceStateForCapability(capability.state)}
                    title={capability.detail}
                  />
                </div>
                <p className="source-card__description">{capability.detail}</p>
              </div>
            ))}
          </div>
        </CardBody>
      </Card>

      <Card>
        <CardHeader
          title="运行时指标"
          description="以只读外链打开 gateway 的 Prometheus 端点，控制台不解析、不聚合，也不展示推测数据"
        />
        <CardBody>
          {isMockMode ? (
            <div className="callout callout--info">
              <span>
                演示模式没有真实 gateway，指标端点不存在，因此不提供外链。
              </span>
            </div>
          ) : (
            <div className="row row--wrap">
              <a
                className="button button--secondary"
                href={env.metricsUrl}
                target="_blank"
                rel="noreferrer"
              >
                <span className="button__icon">
                  <Info size={14} aria-hidden="true" />
                </span>
                <span>打开指标页（新标签）</span>
              </a>
              <span className="text-sm visually-muted">
                原始 Prometheus 文本格式；需要 QPS、延迟分位等聚合视图时，
                仍需另接 Metrics BFF 或现有监控系统。
              </span>
            </div>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
