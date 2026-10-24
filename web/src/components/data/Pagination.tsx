import { ChevronLeft } from 'lucide-react'
import { Button } from '../ui/Button'
import {
  PAGE_SIZE_OPTIONS,
  type PageSize,
  type PaginationResult,
} from '../../lib/usePagination'

export interface PaginationProps {
  pagination: PaginationResult<unknown>
  itemLabel: string
}

export function Pagination({ pagination, itemLabel }: PaginationProps) {
  const {
    page,
    pageSize,
    pageCount,
    total,
    rangeStart,
    rangeEnd,
    setPage,
    setPageSize,
  } = pagination

  return (
    <div className="pagination" aria-label="列表分页">
      <div className="pagination__summary" aria-live="polite">
        第 {rangeStart}–{rangeEnd} 条，共 {total} {itemLabel}
      </div>
      <div className="pagination__controls">
        <label className="pagination__size">
          每页
          <select
            className="field__input pagination__select"
            value={pageSize}
            onChange={(event) =>
              setPageSize(Number(event.target.value) as PageSize)
            }
            aria-label="每页条数"
          >
            {PAGE_SIZE_OPTIONS.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
          条
        </label>
        <Button
          size="sm"
          variant="ghost"
          icon={<ChevronLeft size={14} aria-hidden="true" />}
          disabled={page <= 1}
          onClick={() => setPage(page - 1)}
        >
          上一页
        </Button>
        <span className="pagination__page">
          第 {page} / {pageCount} 页
        </span>
        <Button
          size="sm"
          variant="ghost"
          disabled={page >= pageCount}
          onClick={() => setPage(page + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  )
}
