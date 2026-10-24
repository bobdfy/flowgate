import { Fragment } from 'react'
import { useMatches } from 'react-router-dom'
import { ChevronRight, PanelLeft } from 'lucide-react'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { DataSourceBadges } from '../../features/connection/DataSourceBadges'
import { isMockMode } from '../../lib/capabilities'
import { env } from '../../lib/env'

interface RouteHandle {
  crumbs?: string[]
}

export interface TopBarProps {
  onToggleSidebar: () => void
}

export function TopBar({ onToggleSidebar }: TopBarProps) {
  const matches = useMatches()
  const crumbs = matches.flatMap((match) => {
    const handle = match.handle as RouteHandle | undefined
    return handle?.crumbs ?? []
  })

  return (
    <header className="topbar">
      <div className="topbar__left">
        <Button
          className="topbar__menu-button"
          variant="ghost"
          size="sm"
          icon={<PanelLeft size={16} aria-hidden="true" />}
          aria-label="折叠或展开侧边栏"
          title="折叠或展开侧边栏"
          onClick={onToggleSidebar}
        />
        <nav className="topbar__crumbs" aria-label="面包屑">
          {crumbs.length === 0 ? (
            <span className="topbar__crumb topbar__crumb--current">FlowGate</span>
          ) : (
            crumbs.map((crumb, index) => (
              <Fragment key={`${crumb}-${index}`}>
                {index > 0 ? (
                  <ChevronRight
                    size={12}
                    className="topbar__crumb-separator"
                    aria-hidden="true"
                  />
                ) : null}
                <span
                  className={
                    index === crumbs.length - 1
                      ? 'topbar__crumb topbar__crumb--current'
                      : 'topbar__crumb'
                  }
                  aria-current={index === crumbs.length - 1 ? 'page' : undefined}
                >
                  {crumb}
                </span>
              </Fragment>
            ))
          )}
        </nav>
      </div>

      <div className="topbar__right">
        {isMockMode ? <Badge tone="warning" dot>演示模式</Badge> : null}
        <Badge tone="neutral" className="topbar__env">
          {env.environmentLabel}
        </Badge>
        <DataSourceBadges />
      </div>
    </header>
  )
}
