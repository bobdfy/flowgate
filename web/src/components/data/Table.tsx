import type { ReactNode } from 'react'

export interface TableProps {
  children: ReactNode
  minWidth?: number
}

export function Table({ children, minWidth = 760 }: TableProps) {
  return (
    <div className="table-wrap">
      <table className="data-table" style={{ minWidth }}>
        {children}
      </table>
    </div>
  )
}
