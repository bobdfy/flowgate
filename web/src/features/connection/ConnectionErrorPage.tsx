import { RefreshCw, Unplug } from 'lucide-react'
import { Badge } from '../../components/ui/Badge'
import { Button } from '../../components/ui/Button'
import { env } from '../../lib/env'
import { getErrorMessage } from '../../lib/errorMessage'
import { formatDateTime } from '../../lib/time'
import { useConnection } from './useConnection'

export function ConnectionErrorPage() {
  const { error, isChecking, lastCheckedAt, retry } = useConnection()

  return (
    <div className="error-page">
      <div className="error-page__panel">
        <div className="row-space-between">
          <div className="gate__brand" style={{ marginBottom: 0 }}>
            <span className="gate__mark">
              <Unplug size={18} strokeWidth={1.9} aria-hidden="true" />
            </span>
            <div>
              <div className="gate__brand-name">FlowGate Console</div>
              <div className="gate__brand-subtitle">API 网关管理控制台</div>
            </div>
          </div>
          <Badge tone="danger" dot>
            管理面不可达
          </Badge>
        </div>

        <h1 className="error-page__title">无法连接管理面</h1>
        <p className="error-page__description">
          {error
            ? getErrorMessage(error)
            : '控制台无法访问管理 API，请确认服务状态后重试。'}
        </p>

        <dl className="kv-list" style={{ marginTop: 20 }}>
          <dt>接入地址</dt>
          <dd>
            <code>{env.adminBaseUrl}</code>
          </dd>
          <dt>最近检查</dt>
          <dd>{formatDateTime(lastCheckedAt)}</dd>
        </dl>

        <div className="callout callout--warning" style={{ marginTop: 20 }}>
          <span>
            请确认 admin 已监听 <code>127.0.0.1:8092</code>，并检查{' '}
            <code>VITE_ADMIN_BASE_URL</code> 与 Vite 代理配置。管理面未启动时，控制台不会展示缓存或演示数据。
          </span>
        </div>

        <div className="error-page__actions">
          <Button
            variant="primary"
            icon={<RefreshCw size={14} aria-hidden="true" />}
            loading={isChecking}
            onClick={() => void retry()}
          >
            重新连接
          </Button>
        </div>
      </div>
    </div>
  )
}
