import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getVersion,
  listVersions,
  publishConfig,
  rollbackVersion,
} from '../../api/versions'
import { gatewayKeys } from './queryKeys'

export function useVersions() {
  return useQuery({
    queryKey: gatewayKeys.versionList,
    queryFn: ({ signal }) => listVersions(signal),
  })
}

export function useVersion(id: number | null) {
  return useQuery({
    queryKey: gatewayKeys.version(id ?? 0),
    queryFn: ({ signal }) => {
      if (id === null) {
        throw new Error('缺少版本 ID')
      }
      return getVersion(id, signal)
    },
    enabled: id !== null,
  })
}

export function usePublish() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => publishConfig(),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.versionList })
    },
  })
}

export function useRollbackVersion() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => rollbackVersion(id),
    onSuccess: () => {
      // 回滚会 copy-forward 出一个新版本，历史版本本身不变。
      void queryClient.invalidateQueries({ queryKey: gatewayKeys.versionList })
    },
  })
}
