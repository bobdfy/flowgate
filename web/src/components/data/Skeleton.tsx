import { cx } from '../../lib/cx'

export interface SkeletonProps {
  width?: string | number
  height?: number
  className?: string
}

export function Skeleton({ width = '100%', height = 12, className }: SkeletonProps) {
  return (
    <div
      className={cx('skeleton', className)}
      style={{ width, height }}
      aria-hidden="true"
    />
  )
}
