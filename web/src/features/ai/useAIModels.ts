import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createAIModel,
  deleteAIModel,
  listAIModels,
  updateAIModel,
} from '../../api/aiConfig'
import type { AIModelWriteInput } from '../../api/contracts/aiConfig'
import { aiKeys } from './queryKeys'

export function useAIModels() {
  return useQuery({
    queryKey: aiKeys.modelList,
    queryFn: ({ signal }) => listAIModels(signal),
  })
}

export function useCreateAIModel() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: AIModelWriteInput) => createAIModel(input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: aiKeys.modelList })
    },
  })
}

export function useUpdateAIModel() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ name, input }: { name: string; input: AIModelWriteInput }) =>
      updateAIModel(name, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: aiKeys.modelList })
    },
  })
}

export function useDeleteAIModel() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => deleteAIModel(name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: aiKeys.modelList })
    },
  })
}
