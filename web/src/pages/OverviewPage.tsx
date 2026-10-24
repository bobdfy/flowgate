import { Link } from 'react-router-dom'
import { ArrowRight, Info, RefreshCw } from 'lucide-react'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Card, CardBody, CardHeader } from '../components/ui/Card'
import { PageHeader } from '../components/layout/PageHeader'
import { SourceBadge, type SourceState } from '../components/ui/SourceBadge'
import { UnavailableState } from '../components/data/UnavailableState'
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
            : '确认控制台的数据源接入状态。运行时指标仅在真实查询通道就绪后展示。'
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
                <code>:9090/metrics</code>
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
          title="运行时指标尚未接入"
          description="没有可用的 Metrics BFF，因此不展示请求量、QPS、延迟分位或错误率"
        />
        <CardBody>
          <UnavailableState
            title="不展示推测数据"
            description="gateway 已暴露 Prometheus 指标，但控制台尚无聚合查询接口。在接入前，总览不会使用静态示例或随机数填充图表。"
            requirements={[
              'GET :9090/metrics — Prometheus 文本格式',
              '只读 Metrics BFF — 时间范围、步长、路由维度聚合',
            ]}
            actions={
              <span className="row text-sm visually-muted">
                <Info size={14} aria-hidden="true" />
                这是真实的能力缺口，不是加载失败
              </span>
            }
          />
        </CardBody>
      </Card>
    </div>
  )
}
