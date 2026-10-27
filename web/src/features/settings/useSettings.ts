import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getTPMWindowSettings,
  updateTPMWindowSettings,
} from '../../api/settings'
import { usageKeys } from '../usage/queryKeys'

export const settingsKeys = {
  tpmWindow: ['settings', 'tpm-window'] as const,
}

/** 每 5 秒刷新，让“已保存但 gateway 还没切换”的状态能自动收敛。 */
export function useTPMWindowSettings(enabled = true) {
  return useQuery({
    queryKey: settingsKeys.tpmWindow,
    queryFn: ({ signal }) => getTPMWindowSettings(signal),
    enabled,
    refetchInterval: 5_000,
  })
}

export function useUpdateTPMWindow() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (windowSeconds: number) =>
      updateTPMWindowSettings(windowSeconds),
    onSuccess: (data) => {
      queryClient.setQueryData(settingsKeys.tpmWindow, data)
      // 窗口变了，用量接口的“当前窗口”也跟着变，直接刷新。
      void queryClient.invalidateQueries({ queryKey: usageKeys.all })
    },
  })
}
