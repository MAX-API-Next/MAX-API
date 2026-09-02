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
import { useCallback, useEffect, useMemo, useState } from 'react'
import type {
  ColumnFiltersState,
  OnChangeFn,
  PaginationState,
} from '@tanstack/react-table'

type SearchRecord = Record<string, unknown>

function areFilterValuesEqual(left: unknown, right: unknown): boolean {
  if (Array.isArray(left) || Array.isArray(right)) {
    if (!Array.isArray(left) || !Array.isArray(right)) return false
    return (
      left.length === right.length &&
      left.every((value, index) => areFilterValuesEqual(value, right[index]))
    )
  }

  return Object.is(left, right)
}

function areColumnFiltersEqual(
  left: ColumnFiltersState,
  right: ColumnFiltersState
): boolean {
  return (
    left.length === right.length &&
    left.every(
      (filter, index) =>
        filter.id === right[index]?.id &&
        areFilterValuesEqual(filter.value, right[index]?.value)
    )
  )
}

export type NavigateFn = (opts: {
  search:
    | true
    | SearchRecord
    | ((prev: SearchRecord) => Partial<SearchRecord> | SearchRecord)
  replace?: boolean
}) => void

type UseTableUrlStateParams = {
  search: SearchRecord
  navigate: NavigateFn
  pagination?: {
    pageKey?: string
    pageSizeKey?: string
    defaultPage?: number
    defaultPageSize?: number
  }
  globalFilter?: {
    enabled?: boolean
    key?: string
    trim?: boolean
    mode?: 'instant' | 'submit'
  }
  columnFilters?: Array<
    | {
        columnId: string
        searchKey: string
        type?: 'string'
        // Optional transformers for custom types
        serialize?: (value: unknown) => unknown
        deserialize?: (value: unknown) => unknown
      }
    | {
        columnId: string
        searchKey: string
        type: 'array'
        serialize?: (value: unknown) => unknown
        deserialize?: (value: unknown) => unknown
      }
  >
}

type UseTableUrlStateReturn = {
  // Global filter
  globalFilter?: string
  onGlobalFilterChange?: OnChangeFn<string>
  globalFilterInput?: string
  onGlobalFilterInputChange?: OnChangeFn<string>
  /** Commit the draft filter and optionally update related search fields. */
  applyGlobalFilter?: (additionalSearch?: SearchRecord) => void
  resetGlobalFilter?: () => void
  // Column filters
  columnFilters: ColumnFiltersState
  onColumnFiltersChange: OnChangeFn<ColumnFiltersState>
  // Pagination
  pagination: PaginationState
  onPaginationChange: OnChangeFn<PaginationState>
  // Helpers
  ensurePageInRange: (
    pageCount: number,
    opts?: { resetTo?: 'first' | 'last' }
  ) => void
}

export function useTableUrlState(
  params: UseTableUrlStateParams
): UseTableUrlStateReturn {
  const {
    search,
    navigate,
    pagination: paginationCfg,
    globalFilter: globalFilterCfg,
    columnFilters: columnFiltersCfg = [],
  } = params

  const pageKey = paginationCfg?.pageKey ?? ('page' as string)
  const pageSizeKey = paginationCfg?.pageSizeKey ?? ('pageSize' as string)
  const defaultPage = paginationCfg?.defaultPage ?? 1
  const defaultPageSize = paginationCfg?.defaultPageSize ?? 20

  const globalFilterKey = globalFilterCfg?.key ?? ('filter' as string)
  const globalFilterEnabled = globalFilterCfg?.enabled ?? true
  const trimGlobal = globalFilterCfg?.trim ?? true
  const globalFilterMode = globalFilterCfg?.mode ?? 'instant'

  // Build initial column filters from the current search params
  const initialColumnFilters: ColumnFiltersState = useMemo(() => {
    const collected: ColumnFiltersState = []
    for (const cfg of columnFiltersCfg) {
      const raw = (search as SearchRecord)[cfg.searchKey]
      const deserialize = cfg.deserialize ?? ((v: unknown) => v)
      if (cfg.type === 'string') {
        const value = (deserialize(raw) as string) ?? ''
        if (typeof value === 'string' && value.trim() !== '') {
          collected.push({ id: cfg.columnId, value })
        }
      } else {
        // default to array type
        const value = (deserialize(raw) as unknown[]) ?? []
        if (Array.isArray(value) && value.length > 0) {
          collected.push({ id: cfg.columnId, value })
        }
      }
    }
    return collected
  }, [columnFiltersCfg, search])

  const [columnFilters, setColumnFilters] =
    useState<ColumnFiltersState>(initialColumnFilters)

  // URL 为单一数据源：仅当 search（URL）变化时同步，避免依赖 initialColumnFilters 造成死循环（config 常为内联引用）
  useEffect(() => {
    // This effect mirrors external URL navigation into the table's controlled state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setColumnFilters((previous) =>
      areColumnFiltersEqual(previous, initialColumnFilters)
        ? previous
        : initialColumnFilters
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search])

  const pagination: PaginationState = useMemo(() => {
    const rawPage = (search as SearchRecord)[pageKey]
    const rawPageSize = (search as SearchRecord)[pageSizeKey]
    const pageNum = typeof rawPage === 'number' ? rawPage : defaultPage
    const pageSizeNum =
      typeof rawPageSize === 'number' ? rawPageSize : defaultPageSize
    return { pageIndex: Math.max(0, pageNum - 1), pageSize: pageSizeNum }
  }, [search, pageKey, pageSizeKey, defaultPage, defaultPageSize])

  const onPaginationChange: OnChangeFn<PaginationState> = (updater) => {
    const next = typeof updater === 'function' ? updater(pagination) : updater
    const nextPage = next.pageIndex + 1
    const nextPageSize = next.pageSize
    navigate({
      search: (prev) => ({
        ...(prev as SearchRecord),
        [pageKey]: nextPage <= defaultPage ? undefined : nextPage,
        [pageSizeKey]: nextPageSize,
      }),
    })
  }

  const urlGlobalFilter = useMemo(() => {
    if (!globalFilterEnabled) return undefined
    const raw = (search as SearchRecord)[globalFilterKey]
    return typeof raw === 'string' ? raw : ''
  }, [globalFilterEnabled, globalFilterKey, search])

  const [globalFilter, setGlobalFilter] = useState<string | undefined>(
    urlGlobalFilter
  )
  const [globalFilterInput, setGlobalFilterInput] = useState<
    string | undefined
  >(urlGlobalFilter)

  // Keep both committed and draft values aligned with external URL changes
  // (for example browser back/forward navigation or a reset from another control).
  useEffect(() => {
    if (!globalFilterEnabled) return
    // This effect mirrors external URL navigation into local input state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setGlobalFilter(urlGlobalFilter ?? '')
    setGlobalFilterInput(urlGlobalFilter ?? '')
  }, [globalFilterEnabled, urlGlobalFilter])

  const commitGlobalFilter = useCallback(
    (next: string, additionalSearch: SearchRecord = {}) => {
      const value = trimGlobal ? next.trim() : next
      setGlobalFilter(value)
      setGlobalFilterInput(value)
      navigate({
        search: (prev) => ({
          ...(prev as SearchRecord),
          ...additionalSearch,
          [pageKey]: undefined,
          [globalFilterKey]: value ? value : undefined,
        }),
      })
    },
    [globalFilterKey, navigate, pageKey, trimGlobal]
  )

  const onGlobalFilterChange: OnChangeFn<string> | undefined =
    globalFilterEnabled
      ? (updater) => {
          const next =
            typeof updater === 'function'
              ? updater(globalFilter ?? '')
              : updater
          commitGlobalFilter(next)
        }
      : undefined

  const onGlobalFilterInputChange: OnChangeFn<string> | undefined =
    globalFilterEnabled
      ? (updater) => {
          const next =
            typeof updater === 'function'
              ? updater(globalFilterInput ?? '')
              : updater
          if (globalFilterMode === 'submit') {
            setGlobalFilterInput(next)
            return
          }
          commitGlobalFilter(next)
        }
      : undefined

  const applyGlobalFilter =
    globalFilterEnabled && globalFilterMode === 'submit'
      ? (additionalSearch?: SearchRecord) =>
          commitGlobalFilter(globalFilterInput ?? '', additionalSearch)
      : undefined

  const resetGlobalFilter = globalFilterEnabled
    ? () => commitGlobalFilter('')
    : undefined

  const onColumnFiltersChange: OnChangeFn<ColumnFiltersState> = (updater) => {
    const next =
      typeof updater === 'function' ? updater(columnFilters) : updater
    setColumnFilters(next)

    const patch: Record<string, unknown> = {}

    for (const cfg of columnFiltersCfg) {
      const found = next.find((f) => f.id === cfg.columnId)
      const serialize = cfg.serialize ?? ((v: unknown) => v)
      if (cfg.type === 'string') {
        const value =
          typeof found?.value === 'string' ? (found.value as string) : ''
        patch[cfg.searchKey] =
          value.trim() !== '' ? serialize(value) : undefined
      } else {
        const value = Array.isArray(found?.value)
          ? (found!.value as unknown[])
          : []
        patch[cfg.searchKey] = value.length > 0 ? serialize(value) : undefined
      }
    }

    navigate({
      search: (prev) => ({
        ...(prev as SearchRecord),
        [pageKey]: undefined,
        ...patch,
      }),
    })
  }

  const ensurePageInRange = (
    pageCount: number,
    opts: { resetTo?: 'first' | 'last' } = { resetTo: 'first' }
  ) => {
    const currentPage = (search as SearchRecord)[pageKey]
    const pageNum = typeof currentPage === 'number' ? currentPage : defaultPage
    if (pageCount > 0 && pageNum > pageCount) {
      navigate({
        replace: true,
        search: (prev) => ({
          ...(prev as SearchRecord),
          [pageKey]: opts.resetTo === 'last' ? pageCount : undefined,
        }),
      })
    }
  }

  return {
    globalFilter: globalFilterEnabled ? (globalFilter ?? '') : undefined,
    onGlobalFilterChange,
    globalFilterInput: globalFilterEnabled
      ? (globalFilterInput ?? '')
      : undefined,
    onGlobalFilterInputChange,
    applyGlobalFilter,
    resetGlobalFilter,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  }
}
