import { lazy } from 'react'

/**
 * 页面按路由懒加载：首屏只下载应用壳与当前页面，避免所有业务页面进入同一个 bundle。
 * 这些声明单独放在这里，避免路由配置文件同时导出组件与非组件而影响 Fast Refresh。
 */
export const OverviewPage = lazy(() =>
  import('../pages/OverviewPage').then((module) => ({
    default: module.OverviewPage,
  })),
)

export const LogsPage = lazy(() =>
  import('../pages/plannedPages').then((module) => ({
    default: module.LogsPage,
  })),
)

export const ServicesPage = lazy(() =>
  import('../pages/ServicesPage').then((module) => ({
    default: module.ServicesPage,
  })),
)

export const ServiceDetailPage = lazy(() =>
  import('../pages/ServiceDetailPage').then((module) => ({
    default: module.ServiceDetailPage,
  })),
)

export const RoutesPage = lazy(() =>
  import('../pages/RoutesPage').then((module) => ({
    default: module.RoutesPage,
  })),
)

export const PublishPage = lazy(() =>
  import('../pages/PublishPage').then((module) => ({
    default: module.PublishPage,
  })),
)

export const AIConfigPage = lazy(() =>
  import('../pages/AIConfigPage').then((module) => ({
    default: module.AIConfigPage,
  })),
)

export const TenantsPage = lazy(() =>
  import('../pages/TenantsPage').then((module) => ({
    default: module.TenantsPage,
  })),
)

export const TenantDetailPage = lazy(() =>
  import('../pages/TenantDetailPage').then((module) => ({
    default: module.TenantDetailPage,
  })),
)

export const ConnectionsPage = lazy(() =>
  import('../pages/ConnectionsPage').then((module) => ({
    default: module.ConnectionsPage,
  })),
)
