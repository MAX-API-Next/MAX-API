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
/**
 * Utility functions for usage logs feature
 */
import {
  getAllLogs,
  getUserLogs,
  getAllMidjourneyLogs,
  getUserMidjourneyLogs,
  getAllTaskLogs,
  getUserTaskLogs,
} from '../api'
import {
  LOG_TYPE_ALL_VALUE,
  LOG_TYPES,
  DISPLAYABLE_LOG_TYPES,
  TIMING_LOG_TYPES,
  LOG_TYPE_RETRY_VALUE,
  LOG_TYPE_ERROR_RETRY_VALUE,
  LOG_TYPE_EMPTY_RETRY_VALUE,
  QUOTA_FILTER_ALL_VALUE,
} from '../constants'
import type {
  GetLogsParams,
  GetLogsResponse,
  FetchLogsConfig,
  GetMidjourneyLogsParams,
  GetTaskLogsParams,
} from '../types'
import { parseLogOther } from './format'

const RETRY_LOG_FILTER_VALUES = [
  LOG_TYPE_RETRY_VALUE,
  LOG_TYPE_ERROR_RETRY_VALUE,
  LOG_TYPE_EMPTY_RETRY_VALUE,
] as const

type RetryLogFilterValue = (typeof RETRY_LOG_FILTER_VALUES)[number]

function isRetryLogFilterValue(value: string): value is RetryLogFilterValue {
  return (RETRY_LOG_FILTER_VALUES as readonly string[]).includes(value)
}

// ============================================================================
// Type Checkers & Utilities
// ============================================================================

/**
 * Check if log type is displayable (has detailed info)
 */
export function isDisplayableLogType(type: number): boolean {
  return (DISPLAYABLE_LOG_TYPES as readonly number[]).includes(type)
}

/**
 * Check if log type shows timing info
 */
export function isTimingLogType(type: number): boolean {
  return (TIMING_LOG_TYPES as readonly number[]).includes(type)
}

/**
 * Get log type configuration by type number
 */
export function getLogTypeConfig(type: number) {
  return LOG_TYPES.find((t) => t.value === type) || LOG_TYPES[0]
}

/**
 * Check if log uses per-call billing
 */
export function isPerCallBilling(modelPrice?: number): boolean {
  return (modelPrice ?? 0) > 0
}

const HOUR_IN_MS = 60 * 60 * 1000

/**
 * Get default time range (one hour before now to one hour after now)
 */
export function getDefaultTimeRange(): { start: Date; end: Date } {
  const now = Date.now()
  const start = new Date(now - HOUR_IN_MS)
  const end = new Date(now + HOUR_IN_MS)

  return { start, end }
}

/**
 * Convert milliseconds timestamp to seconds for API
 */
function timestampToSeconds(ms: number): number {
  return Math.floor(ms / 1000)
}

export function matchesCommonLogTypeFilter(
  log: {
    type: number
    other: string
    is_retry?: boolean
    is_error_retry?: boolean
    is_empty_retry?: boolean
  },
  value: unknown
): boolean {
  if (!Array.isArray(value) || value.length === 0) return true

  const filterValues = value
    .map((item) => String(item).trim())
    .filter((item) => item !== '')

  if (filterValues.length === 0) return true
  if (filterValues.includes(LOG_TYPE_ALL_VALUE)) return true
  if (
    filterValues.includes(LOG_TYPE_RETRY_VALUE) ||
    filterValues.includes(LOG_TYPE_ERROR_RETRY_VALUE) ||
    filterValues.includes(LOG_TYPE_EMPTY_RETRY_VALUE)
  ) {
    if (filterValues.includes(LOG_TYPE_ERROR_RETRY_VALUE)) {
      if (log.is_error_retry === true) return true
      if (log.is_empty_retry === true) return false
      const other = parseLogOther(log.other)
      if (other?.empty_retry === true) return false
      return other?.retry_log === true
    }
    if (filterValues.includes(LOG_TYPE_EMPTY_RETRY_VALUE)) {
      if (log.is_empty_retry === true) return true
      const other = parseLogOther(log.other)
      return other?.empty_retry === true
    }
    if (log.is_retry === true) {
      return true
    }
    const other = parseLogOther(log.other)
    return other?.retry_log === true || other?.empty_retry === true
  }
  return filterValues.includes(String(log.type))
}

/**
 * Build query parameters from filters
 */
export function buildQueryParams(
  params: Record<string, unknown>
): URLSearchParams {
  const queryParams = new URLSearchParams()

  Object.entries(params).forEach(([key, value]) => {
    // Keep 0 as a valid value, only filter out undefined, null, and empty string
    if (value !== undefined && value !== null && value !== '') {
      queryParams.append(key, String(value))
    }
  })

  return queryParams
}

/**
 * Build time range parameters with default values
 * Shared logic for all log types
 */
function buildTimeRangeParams(
  searchParams: Record<string, unknown>,
  useMilliseconds: boolean
): { start_timestamp?: number; end_timestamp?: number } {
  const hasTimeParams = searchParams.startTime ?? searchParams.endTime
  const defaultTimeRange = !hasTimeParams ? getDefaultTimeRange() : null

  const convertTimestamp = (timestamp: number) =>
    useMilliseconds ? timestamp : timestampToSeconds(timestamp)

  const getTimestamp = (paramTime?: unknown, defaultTime?: Date) => {
    const time = (paramTime as number) || defaultTime?.getTime()
    return time ? convertTimestamp(time) : undefined
  }

  return {
    start_timestamp: getTimestamp(
      searchParams.startTime,
      defaultTimeRange?.start
    ),
    end_timestamp: getTimestamp(searchParams.endTime, defaultTimeRange?.end),
  }
}

/**
 * Build base parameters with time range (for drawing and task logs)
 * @param useMilliseconds - Whether to use millisecond timestamps (true for drawing logs, false for task logs)
 */
export function buildBaseParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
  useMilliseconds?: boolean
}): {
  p: number
  page_size: number
  channel_id?: string
  start_timestamp?: number
  end_timestamp?: number
  quota_filter?: string
} {
  const { page, pageSize, searchParams, useMilliseconds = false } = config

  return {
    p: page,
    page_size: pageSize,
    ...(searchParams.channel
      ? {
          channel_id: String(searchParams.channel),
        }
      : {}),
    ...(searchParams.quotaFilter &&
    searchParams.quotaFilter !== QUOTA_FILTER_ALL_VALUE
      ? { quota_filter: String(searchParams.quotaFilter) }
      : {}),
    ...buildTimeRangeParams(searchParams, useMilliseconds),
  }
}

/**
 * Build API params from search params and column filters (for common logs)
 */
export function buildApiParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
  columnFilters?: Array<{ id: string; value: unknown }>
  isAdmin: boolean
}): GetLogsParams {
  const { page, pageSize, searchParams, columnFilters = [], isAdmin } = config

  const applyTypeFilter = (params: GetLogsParams, value: unknown) => {
    const typeValues = (Array.isArray(value) ? value : [value])
      .map((item) => String(item).trim())
      .filter((item) => item !== '')

    if (typeValues.length === 0) {
      params.type = undefined
      params.log_filter = undefined
      return
    }

    if (typeValues.includes(LOG_TYPE_ALL_VALUE) || typeValues.includes('all')) {
      params.type = undefined
      params.log_filter = undefined
      return
    }

    const retryFilter = typeValues.find(isRetryLogFilterValue)
    if (retryFilter) {
      params.log_filter = retryFilter
      params.type = undefined
      return
    }
    params.type = processType(typeValues)
    params.log_filter = undefined
  }

  // Helper to process type parameter (single value from array)
  const processType = (value: unknown): number | undefined => {
    const parseType = (raw: unknown): number | undefined => {
      const type = Number(raw)
      return Number.isFinite(type) ? type : undefined
    }

    if (Array.isArray(value) && value.length === 1) {
      return parseType(value[0])
    }
    if (typeof value === 'string' && value !== '') {
      return parseType(value)
    }
    return undefined
  }

  // Build base params from search params
  const params: GetLogsParams = {
    p: page,
    page_size: pageSize,
    ...(searchParams.model ? { model_name: String(searchParams.model) } : {}),
    ...(searchParams.token ? { token_name: String(searchParams.token) } : {}),
    ...(searchParams.group ? { group: String(searchParams.group) } : {}),
    ...(isAdmin && searchParams.channel
      ? { channel: Number(searchParams.channel) || 0 }
      : {}),
    ...(isAdmin && searchParams.username
      ? { username: String(searchParams.username) }
      : {}),
    ...(searchParams.requestId
      ? { request_id: String(searchParams.requestId) }
      : {}),
    ...(searchParams.upstreamRequestId
      ? { upstream_request_id: String(searchParams.upstreamRequestId) }
      : {}),
    ...(searchParams.quotaFilter &&
    searchParams.quotaFilter !== QUOTA_FILTER_ALL_VALUE
      ? { quota_filter: String(searchParams.quotaFilter) }
      : {}),
    ...buildTimeRangeParams(searchParams, false),
  }
  if (searchParams.type) {
    applyTypeFilter(params, searchParams.type)
  }

  // Override with column filters if present
  if (columnFilters.length > 0) {
    columnFilters.forEach(({ id, value }) => {
      if (value === undefined || value === null || value === '') return

      switch (id) {
        case 'type':
          applyTypeFilter(params, value)
          break
        case 'model_name':
          params.model_name = String(value)
          break
        case 'token_name':
          params.token_name = String(value)
          break
        case 'group':
          params.group = String(value)
          break
        case 'channel':
          if (isAdmin) params.channel = Number(value) || 0
          break
        case 'username':
          if (isAdmin) params.username = String(value)
          break
      }
    })
  }

  return params
}

// ============================================================================
// Data Fetching
// ============================================================================

/**
 * Fetch logs based on category type
 */
export async function fetchLogsByCategory(
  config: FetchLogsConfig
): Promise<GetLogsResponse> {
  const { logCategory, isAdmin, page, pageSize, searchParams, columnFilters } =
    config

  if (logCategory === 'common') {
    const params = buildApiParams({
      page,
      pageSize,
      searchParams,
      columnFilters,
      isAdmin,
    })
    return isAdmin ? await getAllLogs(params) : await getUserLogs(params)
  }

  // For drawing and task logs
  const baseParams = buildBaseParams({
    page,
    pageSize,
    searchParams,
    useMilliseconds: logCategory === 'drawing',
  })

  const paramsWithFilter = {
    ...baseParams,
    ...(logCategory === 'drawing'
      ? { mj_id: searchParams.filter as string | undefined }
      : {}),
    ...(logCategory === 'task'
      ? { task_id: searchParams.filter as string | undefined }
      : {}),
  }

  if (logCategory === 'drawing') {
    return isAdmin
      ? await getAllMidjourneyLogs(paramsWithFilter as GetMidjourneyLogsParams)
      : await getUserMidjourneyLogs(paramsWithFilter as GetMidjourneyLogsParams)
  }

  // task logs
  return isAdmin
    ? await getAllTaskLogs(paramsWithFilter as GetTaskLogsParams)
    : await getUserTaskLogs(paramsWithFilter as GetTaskLogsParams)
}
