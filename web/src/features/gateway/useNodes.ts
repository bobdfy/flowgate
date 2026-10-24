import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createNode,
  deleteNode,
  listNodes,
  updateNode,
} from '../../api/nodes'
import type { NodeWriteInput } from '../../api/contracts/node'
import { gatewayKeys } from './queryKeys'

export function useNodes(serviceId: number | null) {
  return useQuery({
    queryKey: gatewayKeys.nodes(serviceId ?? 0),
    queryFn: ({ signal }) => {
      if (serviceId === null) {
        throw new Error('缺少服务 ID')
      }
      return listNodes(serviceId, signal)
    },
    enabled: serviceId !== null,
  })
}

export function useCreateNode(serviceId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: NodeWriteInput) => createNode(serviceId, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: gatewayKeys.nodes(serviceId),
      })
    },
  })
}

export function useUpdateNode(serviceId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: NodeWriteInput }) =>
      updateNode(id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: gatewayKeys.nodes(serviceId),
      })
    },
  })
}

export function useDeleteNode(serviceId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteNode(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: gatewayKeys.nodes(serviceId),
      })
    },
  })
}
