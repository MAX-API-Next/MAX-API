import type { ModelPerformanceItem } from '../types'

export type ModelPerformanceSortKey =
  | 'model_name'
  | 'channel_count'
  | 'observed_count'
  | 'error_log_count'
  | 'consumed_quota'
  | 'observed_success_rate'
  | 'avg_logged_latency_ms'
  | 'avg_tps'
  | 'retry_log_count'

export type ModelPerformanceSortState = {
  key: ModelPerformanceSortKey
  direction: 'asc' | 'desc'
} | null

function sortableValue(
  item: ModelPerformanceItem,
  key: ModelPerformanceSortKey
): number | string | null {
  const value = item[key]
  if (key === 'model_name') return typeof value === 'string' ? value : null
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export function sortModelPerformanceItems(
  items: readonly ModelPerformanceItem[],
  sortState: ModelPerformanceSortState,
  locale?: string
): readonly ModelPerformanceItem[] {
  if (!sortState) return items

  return items
    .map((item, originalIndex) => ({ item, originalIndex }))
    .sort((left, right) => {
      const leftValue = sortableValue(left.item, sortState.key)
      const rightValue = sortableValue(right.item, sortState.key)

      if (leftValue == null && rightValue == null) {
        return left.originalIndex - right.originalIndex
      }
      if (leftValue == null) return 1
      if (rightValue == null) return -1

      const comparison =
        typeof leftValue === 'string' && typeof rightValue === 'string'
          ? leftValue.localeCompare(rightValue, locale)
          : Number(leftValue) - Number(rightValue)
      if (comparison === 0) return left.originalIndex - right.originalIndex
      return sortState.direction === 'asc' ? comparison : -comparison
    })
    .map(({ item }) => item)
}
