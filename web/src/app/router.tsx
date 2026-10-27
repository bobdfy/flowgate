import { Navigate, createBrowserRouter } from 'react-router-dom'
import { AppShell } from '../components/layout/AppShell'
import { ConnectionBoundary } from '../features/connection/ConnectionBoundary'
import { NotFoundPage } from '../pages/NotFoundPage'
import { RouteErrorPage } from '../pages/RouteErrorPage'
import {
  AIConfigPage,
  ConnectionsPage,
  LogsPage,
  OverviewPage,
  PublishPage,
  RoutesPage,
  ServiceDetailPage,
  ServicesPage,
  TenantDetailPage,
  TenantsPage,
} from './lazyPages'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <ConnectionBoundary />,
    errorElement: <RouteErrorPage />,
    children: [
      {
        element: <AppShell />,
        children: [
          { index: true, element: <Navigate to="/overview" replace /> },
          {
            path: 'overview',
            element: <OverviewPage />,
            handle: { crumbs: ['运行', '运行总览'] },
          },
          {
            path: 'observability/logs',
            element: <LogsPage />,
            handle: { crumbs: ['运行', '请求日志'] },
          },
          {
            path: 'gateway/services',
            element: <ServicesPage />,
            handle: { crumbs: ['流量配置', '服务治理'] },
          },
          {
            path: 'gateway/services/:id',
            element: <ServiceDetailPage />,
            handle: { crumbs: ['流量配置', '服务治理', '服务详情'] },
          },
          {
            path: 'gateway/routes',
            element: <RoutesPage />,
            handle: { crumbs: ['流量配置', 'HTTP 路由'] },
          },
          {
            path: 'gateway/publish',
            element: <PublishPage />,
            handle: { crumbs: ['流量配置', '发布中心'] },
          },
          {
            path: 'ai/routing',
            element: <AIConfigPage />,
            handle: { crumbs: ['AI', '模型路由'] },
          },
          {
            path: 'access/tenants',
            element: <TenantsPage />,
            handle: { crumbs: ['访问控制', '租户与 API Key'] },
          },
          {
            path: 'access/tenants/:id',
            element: <TenantDetailPage />,
            handle: { crumbs: ['访问控制', '租户与 API Key', '租户详情'] },
          },
          {
            path: 'system/connections',
            element: <ConnectionsPage />,
            handle: { crumbs: ['系统', '接入状态'] },
          },
          { path: '*', element: <NotFoundPage /> },
        ],
      },
    ],
  },
])
