export const gatewayKeys = {
  services: ['gateway', 'services'] as const,
  serviceList: ['gateway', 'services', 'list'] as const,
  service: (id: number) => ['gateway', 'services', 'detail', id] as const,
  nodes: (serviceId: number) =>
    ['gateway', 'services', serviceId, 'nodes'] as const,
  routes: ['gateway', 'routes'] as const,
  routeList: ['gateway', 'routes', 'list'] as const,
  versions: ['gateway', 'versions'] as const,
  versionList: ['gateway', 'versions', 'list'] as const,
  version: (id: number) => ['gateway', 'versions', 'detail', id] as const,
}
