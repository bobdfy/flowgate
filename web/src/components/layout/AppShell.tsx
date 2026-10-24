import { Suspense, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { isMockMode } from '../../lib/capabilities'
import { DemoModeBanner } from './DemoModeBanner'
import { RouteLoading } from './RouteLoading'
import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'

export function AppShell() {
  const [collapsed, setCollapsed] = useState(false)

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        跳到主要内容
      </a>
      <Sidebar collapsed={collapsed} />
      <div className="app-main">
        <TopBar onToggleSidebar={() => setCollapsed((current) => !current)} />
        {isMockMode ? <DemoModeBanner /> : null}
        <main className="content" id="main-content" tabIndex={-1}>
          <Suspense fallback={<RouteLoading />}>
            <Outlet />
          </Suspense>
        </main>
      </div>
    </div>
  )
}
