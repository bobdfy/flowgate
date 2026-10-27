import { useMemo, useState } from 'react'

export const PAGE_SIZE_OPTIONS = [10, 20, 50] as const
export type PageSize = (typeof PAGE_SIZE_OPTIONS)[number]

const DEFAULT_PAGE_SIZE: PageSize = 10

export interface PaginationResult<T> {
  page: number
  pageSize: PageSize
  pageCount: number
  total: number
  pageItems: T[]
  rangeStart: number
  rangeEnd: number
  setPage: (page: number) => void
  setPageSize: (pageSize: PageSize) => void
}

interface PaginationState {
  resetKey: string
  page: number
  pageSize: PageSize
}

/**
 * 管理 API 的列表接口没有分页参数，这里在已加载的完整列表上做客户端分页。
 * resetKey 变化（筛选条件改变）时回到第一页；数据变少时在渲染阶段收敛到最后一页。
 */
export function usePagination<T>(
  items: readonly T[],
  resetKey: string,
  initialPageSize: PageSize = DEFAULT_PAGE_SIZE,
): PaginationResult<T> {
  const [state, setState] = useState<PaginationState>(() => ({
    resetKey,
    page: 1,
    pageSize: initialPageSize,
  }))

  // 筛选条件变化时在渲染阶段重置，避免先渲染旧页码再触发 effect。
  if (state.resetKey !== resetKey) {
    setState({ resetKey, page: 1, pageSize: state.pageSize })
  }

  const total = items.length
  const pageCount = Math.max(1, Math.ceil(total / state.pageSize))
  const page = Math.min(state.page, pageCount)
  const startIndex = (page - 1) * state.pageSize

  const pageItems = useMemo(
    () => items.slice(startIndex, startIndex + state.pageSize),
    [items, startIndex, state.pageSize],
  )

  const setPage = (nextPage: number) => {
    const clamped = Math.min(Math.max(1, Math.trunc(nextPage)), pageCount)
    setState((current) => ({ ...current, page: clamped }))
  }

  const setPageSize = (nextPageSize: PageSize) => {
    setState((current) => ({
      ...current,
      pageSize: nextPageSize,
      page: 1,
    }))
  }

  return {
    page,
    pageSize: state.pageSize,
    pageCount,
    total,
    pageItems,
    rangeStart: total === 0 ? 0 : startIndex + 1,
    rangeEnd: Math.min(startIndex + state.pageSize, total),
    setPage,
    setPageSize,
  }
}
