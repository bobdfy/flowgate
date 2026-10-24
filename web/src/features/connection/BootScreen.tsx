import { Network } from 'lucide-react'
import { env } from '../../lib/env'

export function BootScreen() {
  return (
    <div className="boot">
      <div className="boot__panel">
        <div className="boot__brand">
          <span className="gate__mark">
            <Network size={18} strokeWidth={1.9} aria-hidden="true" />
          </span>
          FlowGate Console
        </div>
        <span className="spinner spinner--lg" aria-hidden="true" />
        <p className="boot__text" role="status">
          正在检查管理面连接…
        </p>
        <p className="boot__text">
          <code>{env.adminBaseUrl}</code>
        </p>
      </div>
    </div>
  )
}
