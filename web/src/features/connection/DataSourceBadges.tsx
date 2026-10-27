import { SourceBadge } from '../../components/ui/SourceBadge'
import { isMockMode } from '../../lib/capabilities'
import { connectionStatusMeta } from './status'
import { useConnection } from './useConnection'

export function DataSourceBadges() {
  const { status, isChecking } = useConnection()
  const meta = connectionStatusMeta[status]
  const adminState =
    status === 'connected' && isChecking ? 'checking' : meta.sourceState

  return (
    <div className="topbar__sources">
      <SourceBadge
        label={isMockMode ? '演示数据' : '管理面'}
        state={isMockMode ? 'connected' : adminState}
        title={
          isMockMode
            ? '数据由浏览器内 Mock 适配层提供，未连接真实管理面'
            : status === 'connected' && isChecking
              ? '正在重新检查管理面连接'
              : meta.label
        }
      />
      <SourceBadge
        label="指标"
        state="unavailable"
        title="gateway 指标在 :9090/metrics 暴露，控制台尚未接入 Metrics BFF"
      />
    </div>
  )
}
