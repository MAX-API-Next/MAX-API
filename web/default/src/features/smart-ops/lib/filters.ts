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
import type { ChannelPerformanceQuery, ModelPerformanceQuery } from '../types'

export type FilterDraft = {
  hours: string
  channelId: string
  model: string
  group: string
}

export const DEFAULT_PERFORMANCE_HOURS = 1
export const MAX_PERFORMANCE_HOURS = 168
export const MAX_PERFORMANCE_LIMIT = 200

export const DEFAULT_FILTERS: FilterDraft = {
  hours: String(DEFAULT_PERFORMANCE_HOURS),
  channelId: '',
  model: '',
  group: '',
}

export function normalizePerformanceHours(value: string): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return DEFAULT_PERFORMANCE_HOURS
  return Math.min(
    Math.max(Math.trunc(parsed), DEFAULT_PERFORMANCE_HOURS),
    MAX_PERFORMANCE_HOURS
  )
}

export function normalizeFilters<T extends { hours: string }>(filters: T): T {
  return {
    ...filters,
    hours: String(normalizePerformanceHours(filters.hours)),
  } as T
}

export function toQuery(filters: FilterDraft): ChannelPerformanceQuery {
  const channelId = Number(filters.channelId)
  return {
    hours: normalizePerformanceHours(filters.hours),
    channelId:
      Number.isFinite(channelId) && channelId > 0 ? channelId : undefined,
    model: filters.model.trim() || undefined,
    group: filters.group.trim() || undefined,
  }
}

export type ModelFilterDraft = {
  hours: string
  model: string
  group: string
}

export const DEFAULT_MODEL_FILTERS: ModelFilterDraft = {
  hours: String(DEFAULT_PERFORMANCE_HOURS),
  model: '',
  group: '',
}

export function toModelQuery(filters: ModelFilterDraft): ModelPerformanceQuery {
  return {
    hours: normalizePerformanceHours(filters.hours),
    model: filters.model.trim() || undefined,
    group: filters.group.trim() || undefined,
  }
}
