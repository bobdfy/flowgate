import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createRoute,
  deleteRoute,
  listRoutes,
  updateRoute,
} from '../../api/routes'
import type { RouteWriteInput } from '../../api/contracts/route'
import { gatewayKeys } from './queryKeys'

export function useRoutes() {
  return useQuery({
    queryKey: gatewayKeys.routeList,
    queryFn: ({ signal }) => listRoutes(signal),
  })
}

export function useCreateRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: RouteWriteInput) => createRoute(input),
    onSuccess: () => {
      // 创建响应不包含权威的时间戳，统一重新拉取列表。
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.routeList })
    },
  })
}

export function useUpdateRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: RouteWriteInput }) =>
      updateRoute(id, input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.routeList })
    },
  })
}

export function useDeleteRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteRoute(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.routeList })
    },
  })
}
