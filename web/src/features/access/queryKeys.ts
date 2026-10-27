export const accessKeys = {
  tenants: ['tenants'] as const,
  tenantList: ['tenants', 'list'] as const,
  tenant: (id: number) => ['tenants', 'detail', id] as const,
  apiKeys: (tenantId: number) => ['tenants', 'keys', tenantId] as const,
}
