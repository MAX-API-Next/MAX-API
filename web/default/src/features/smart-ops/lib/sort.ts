/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import type { ChannelPerformanceItem } from '../types'

export type ChannelPerformanceSortKey =
  | 'observed_count'
  | 'error_log_count'
  | 'consumed_quota'
  | 'observed_success_rate'
  | 'avg_logged_latency_ms'
  | 'retry_log_count'
  | 'probe_latency_ms'

export type ChannelPerformanceSortState = {
  key: ChannelPerformanceSortKey
  direction: 'asc' | 'desc'
} | null

function sortableMetric(
  item: ChannelPerformanceItem,
  key: ChannelPerformanceSortKey
): number | null {
  const value = item[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export function sortChannelPerformanceItems(
  items: readonly ChannelPerformanceItem[],
  sortState: ChannelPerformanceSortState
): readonly ChannelPerformanceItem[] {
  if (!sortState) return items

  return items
    .map((item, originalIndex) => ({ item, originalIndex }))
    .sort((left, right) => {
      const leftValue = sortableMetric(left.item, sortState.key)
      const rightValue = sortableMetric(right.item, sortState.key)

      if (leftValue == null && rightValue == null) {
        return left.originalIndex - right.originalIndex
      }
      if (leftValue == null) return 1
      if (rightValue == null) return -1

      const comparison = leftValue - rightValue
      if (comparison === 0) return left.originalIndex - right.originalIndex
      return sortState.direction === 'asc' ? comparison : -comparison
    })
    .map(({ item }) => item)
}
