export type TenantStatus = 'active' | 'disabled'

export interface Tenant {
  id: number
  name: string
  status: TenantStatus
  qps_limit: number
  tpm_limit: number
  created_at: string
}

export interface TenantWriteInput {
  name: string
  status: TenantStatus
  qps_limit: number
  tpm_limit: number
}

export type CreateTenantInput = TenantWriteInput
export type UpdateTenantInput = TenantWriteInput
