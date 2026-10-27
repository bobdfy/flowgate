import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createAIProvider,
  deleteAIProvider,
  listAIProviders,
  updateAIProvider,
} from '../../api/aiConfig'
import type { AIProviderWriteInput } from '../../api/contracts/aiConfig'
import { aiKeys } from './queryKeys'

export function useAIProviders() {
  return useQuery({
    queryKey: aiKeys.providerList,
    queryFn: ({ signal }) => listAIProviders(signal),
  })
}

export function useCreateAIProvider() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: AIProviderWriteInput) => createAIProvider(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: aiKeys.providerList })
    },
  })
}

export function useUpdateAIProvider() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: AIProviderWriteInput }) =>
      updateAIProvider(id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: aiKeys.providerList })
      // provider 改名会通过 ON UPDATE CASCADE 改掉模型的 provider_name。
      void queryClient.invalidateQueries({ queryKey: aiKeys.modelList })
    },
  })
}

export function useDeleteAIProvider() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteAIProvider(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: aiKeys.providerList })
      // provider 删除会级联删除其下模型。
      void queryClient.invalidateQueries({ queryKey: aiKeys.modelList })
    },
  })
}
