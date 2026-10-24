import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createTenant,
  deleteTenant,
  getTenant,
  listTenants,
  updateTenant,
} from '../../api/tenants'
import type {
  CreateTenantInput,
  UpdateTenantInput,
} from '../../api/contracts/tenant'
import { accessKeys } from './queryKeys'

export function useTenants() {
  return useQuery({
    queryKey: accessKeys.tenantList,
    queryFn: ({ signal }) => listTenants(signal),
  })
}

export function useTenant(id: number | null) {
  return useQuery({
    queryKey: accessKeys.tenant(id ?? 0),
    queryFn: ({ signal }) => {
      if (id === null) {
        throw new Error('缺少租户 ID')
      }
      return getTenant(id, signal)
    },
    enabled: id !== null,
  })
}

export function useCreateTenant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateTenantInput) => createTenant(input),
    onSuccess: (tenant) => {
      queryClient.setQueryData(accessKeys.tenant(tenant.id), tenant)
      void queryClient.invalidateQueries({ queryKey: accessKeys.tenantList })
    },
  })
}

export function useUpdateTenant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: UpdateTenantInput }) =>
      updateTenant(id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: accessKeys.tenants })
    },
  })
}

export function useDeleteTenant() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteTenant(id),
    onSuccess: (_, id) => {
      queryClient.removeQueries({ queryKey: accessKeys.tenant(id) })
      queryClient.removeQueries({ queryKey: accessKeys.apiKeys(id) })
      void queryClient.invalidateQueries({ queryKey: accessKeys.tenantList })
    },
  })
}
