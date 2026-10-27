import { Skeleton } from './Skeleton'
import { Table } from './Table'

export interface TableSkeletonProps {
  columns?: number
  rows?: number
  minWidth?: number
}

const widths = ['42%', '64%', '34%', '52%', '28%', '46%']

export function TableSkeleton({
  columns = 5,
  rows = 5,
  minWidth = 760,
}: TableSkeletonProps) {
  return (
    <Table minWidth={minWidth}>
      <tbody>
        {Array.from({ length: rows }, (_, rowIndex) => (
          <tr key={rowIndex}>
            {Array.from({ length: columns }, (_, columnIndex) => (
              <td key={columnIndex}>
                <Skeleton
                  width={widths[columnIndex % widths.length]}
                  height={12}
                />
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </Table>
  )
}
