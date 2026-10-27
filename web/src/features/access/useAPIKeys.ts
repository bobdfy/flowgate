import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createAPIKey,
  deleteAPIKey,
  listAPIKeys,
  updateAPIKey,
} from '../../api/apiKeys'
import type {
  CreateAPIKeyInput,
  UpdateAPIKeyInput,
} from '../../api/contracts/apiKey'
import { accessKeys } from './queryKeys'

export function useAPIKeys(tenantId: number | null) {
  return useQuery({
    queryKey: accessKeys.apiKeys(tenantId ?? 0),
    queryFn: ({ signal }) => {
      if (tenantId === null) {
        throw new Error('缺少租户 ID')
      }
      return listAPIKeys(tenantId, signal)
    },
    enabled: tenantId !== null,
  })
}

export function useCreateAPIKey(tenantId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateAPIKeyInput) => createAPIKey(tenantId, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: accessKeys.apiKeys(tenantId),
      })
    },
  })
}

export function useUpdateAPIKey(tenantId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: UpdateAPIKeyInput }) =>
      updateAPIKey(id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: accessKeys.apiKeys(tenantId),
      })
    },
  })
}

export function useDeleteAPIKey(tenantId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteAPIKey(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: accessKeys.apiKeys(tenantId),
      })
    },
  })
}
