import { Link, NavLink } from 'react-router-dom'
import {
  Cable,
  KeyRound,
  LayoutDashboard,
  Network,
  Rocket,
  Route as RouteIcon,
  ScrollText,
  Server,
  Sparkles,
  type LucideIcon,
} from 'lucide-react'
import { cx } from '../../lib/cx'
import { isMockMode } from '../../lib/capabilities'
import { connectionStatusMeta } from '../../features/connection/status'
import { useConnection } from '../../features/connection/useConnection'

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
}

interface NavGroup {
  label: string
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    label: '运行',
    items: [
      { to: '/overview', label: '运行总览', icon: LayoutDashboard, end: true },
      { to: '/observability/logs', label: '请求日志', icon: ScrollText },
    ],
  },
  {
    label: '流量配置',
    items: [
      { to: '/gateway/services', label: '服务治理', icon: Server },
      { to: '/gateway/routes', label: 'HTTP 路由', icon: RouteIcon },
      { to: '/gateway/publish', label: '发布中心', icon: Rocket },
    ],
  },
  {
    label: 'AI',
    items: [{ to: '/ai/routing', label: '模型路由', icon: Sparkles }],
  },
  {
    label: '访问控制',
    items: [
      { to: '/access/tenants', label: '租户与 API Key', icon: KeyRound },
    ],
  },
  {
    label: '系统',
    items: [{ to: '/system/connections', label: '接入状态', icon: Cable }],
  },
]

export function Sidebar({ collapsed }: { collapsed: boolean }) {
  const { status, isChecking } = useConnection()
  const meta = connectionStatusMeta[status]
  const checking = status === 'connected' && isChecking
  const footerLabel = isMockMode
    ? '演示模式 · Mock 数据'
    : checking
      ? '正在检测管理面'
      : meta.footerLabel
  const dotState = isMockMode
    ? 'degraded'
    : checking
      ? 'degraded'
      : meta.sidebarDot

  return (
    <aside
      className={cx('sidebar', collapsed && 'sidebar--collapsed')}
      aria-label="侧边栏"
    >
      <Link
        to="/overview"
        className="sidebar__brand"
        aria-label="FlowGate 控制台"
        title={collapsed ? 'FlowGate Console' : undefined}
      >
        <span className="sidebar__mark">
          <Network size={17} strokeWidth={2} aria-hidden="true" />
        </span>
        <span className="sidebar__brand-text">
          <span className="sidebar__brand-name">FlowGate</span>
          <span className="sidebar__brand-subtitle">Gateway Console</span>
        </span>
      </Link>

      <nav className="sidebar__nav" aria-label="主导航">
        {navGroups.map((group) => (
          <div className="nav-group" key={group.label}>
            <div className="nav-group__label">{group.label}</div>
            {group.items.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  aria-label={item.label}
                  title={collapsed ? item.label : undefined}
                  className={({ isActive }) =>
                    cx('nav-link', isActive && 'nav-link--active')
                  }
                >
                  <span className="nav-link__icon">
                    <Icon size={17} strokeWidth={1.8} aria-hidden="true" />
                  </span>
                  <span className="nav-link__label">{item.label}</span>
                </NavLink>
              )
            })}
          </div>
        ))}
      </nav>

      <Link
        to="/system/connections"
        className="sidebar__footer"
        aria-label={footerLabel}
        title={footerLabel}
      >
        <span
          className={cx('sidebar__status-dot', `sidebar__status-dot--${dotState}`)}
          aria-hidden="true"
        />
        <span className="sidebar__footer-text">{footerLabel}</span>
      </Link>
    </aside>
  )
}
