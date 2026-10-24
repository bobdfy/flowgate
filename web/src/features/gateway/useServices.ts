import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createService,
  deleteService,
  getService,
  listServices,
  updateService,
} from '../../api/services'
import type { ServiceWriteInput } from '../../api/contracts/service'
import { gatewayKeys } from './queryKeys'

export function useServices() {
  return useQuery({
    queryKey: gatewayKeys.serviceList,
    queryFn: ({ signal }) => listServices(signal),
  })
}

export function useService(id: number | null) {
  return useQuery({
    queryKey: gatewayKeys.service(id ?? 0),
    queryFn: ({ signal }) => {
      if (id === null) {
        throw new Error('缺少服务 ID')
      }
      return getService(id, signal)
    },
    enabled: id !== null,
  })
}

export function useCreateService() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: ServiceWriteInput) => createService(input),
    onSuccess: () => {
      // 创建响应不包含权威的时间戳，统一重新拉取列表。
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.serviceList })
    },
  })
}

export function useUpdateService() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ServiceWriteInput }) =>
      updateService(id, input),
    onSuccess: (_, { id }) => {
      void queryClient.invalidateQueries({
        queryKey: gatewayKeys.service(id),
      })
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.serviceList })
      // 路由列表展示服务名，改名后需要同步刷新。
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.routeList })
    },
  })
}

export function useDeleteService() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteService(id),
    onSuccess: (_, id) => {
      queryClient.removeQueries({ queryKey: gatewayKeys.service(id) })
      queryClient.removeQueries({ queryKey: gatewayKeys.nodes(id) })
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.serviceList })
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.routeList })
    },
  })
}
