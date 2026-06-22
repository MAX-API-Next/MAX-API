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
export type HeaderNavAccessConfig = {
  enabled: boolean
  requireAuth: boolean
}

export type HeaderNavModule = 'rankings' | 'pricing'

export type HeaderNavModulesConfig = {
  home: boolean
  console: boolean
  pricing: HeaderNavAccessConfig
  rankings: HeaderNavAccessConfig
  docs: boolean
  about: boolean
  [key: string]: boolean | HeaderNavAccessConfig
}

export const HEADER_NAV_DEFAULT: HeaderNavModulesConfig = {
  home: true,
  console: true,
  pricing: {
    enabled: true,
    requireAuth: false,
  },
  rankings: {
    enabled: true,
    requireAuth: false,
  },
  docs: true,
  about: true,
}

export const HEADER_NAV_MODULE_DEFAULTS: Record<
  HeaderNavModule,
  HeaderNavAccessConfig
> = {
  pricing: HEADER_NAV_DEFAULT.pricing,
  rankings: HEADER_NAV_DEFAULT.rankings,
}

export function parseHeaderNavBoolean(
  raw: unknown,
  fallback: boolean
): boolean {
  if (typeof raw === 'boolean') return raw
  if (typeof raw === 'number') {
    if (raw === 1) return true
    if (raw === 0) return false
    return fallback
  }
  if (typeof raw === 'string') {
    const normalized = raw.trim().toLowerCase()
    if (normalized === 'true' || normalized === '1') return true
    if (normalized === 'false' || normalized === '0') return false
  }
  return fallback
}

function cloneHeaderNavDefault(): HeaderNavModulesConfig {
  return {
    ...HEADER_NAV_DEFAULT,
    pricing: { ...HEADER_NAV_DEFAULT.pricing },
    rankings: { ...HEADER_NAV_DEFAULT.rankings },
  }
}

function parseHeaderNavRecord(raw: unknown): Record<string, unknown> | null {
  if (raw == null) return null
  if (typeof raw === 'string' && raw.trim() === '') return null
  if (raw && typeof raw === 'object') return raw as Record<string, unknown>

  try {
    const parsed = JSON.parse(String(raw)) as unknown
    return parsed && typeof parsed === 'object'
      ? (parsed as Record<string, unknown>)
      : null
  } catch {
    return null
  }
}

export function parseHeaderNavAccess(
  raw: unknown,
  fallback: HeaderNavAccessConfig
): HeaderNavAccessConfig {
  if (
    typeof raw === 'boolean' ||
    typeof raw === 'number' ||
    typeof raw === 'string'
  ) {
    return {
      enabled: parseHeaderNavBoolean(raw, fallback.enabled),
      requireAuth: fallback.requireAuth,
    }
  }
  if (raw && typeof raw === 'object') {
    const record = raw as Record<string, unknown>
    return {
      enabled: parseHeaderNavBoolean(record.enabled, fallback.enabled),
      requireAuth: parseHeaderNavBoolean(
        record.requireAuth,
        fallback.requireAuth
      ),
    }
  }
  return { ...fallback }
}

export function parseHeaderNavAccessOption(
  raw: unknown,
  fallback: HeaderNavAccessConfig
): HeaderNavAccessConfig {
  if (typeof raw === 'string') {
    const trimmed = raw.trim()
    if (trimmed === '') return { ...fallback }

    try {
      return parseHeaderNavAccess(JSON.parse(trimmed) as unknown, fallback)
    } catch {
      return parseHeaderNavAccess(raw, fallback)
    }
  }

  return parseHeaderNavAccess(raw, fallback)
}

export function parseHeaderNavAccessModule(
  value: string | null | undefined,
  fallback: HeaderNavAccessConfig
): HeaderNavAccessConfig {
  return parseHeaderNavAccessOption(value, fallback)
}

export function serializeHeaderNavAccessModule(
  config: HeaderNavAccessConfig
): string {
  return JSON.stringify(config)
}

export function parseHeaderNavModules(raw: unknown): HeaderNavModulesConfig {
  const result = cloneHeaderNavDefault()
  const parsed = parseHeaderNavRecord(raw)
  if (!parsed) return result

  Object.entries(parsed).forEach(([key, value]) => {
    if (key === 'pricing') {
      result.pricing = parseHeaderNavAccess(value, result.pricing)
      return
    }
    if (key === 'rankings') {
      // Legacy fallback only; new admin writes use the standalone RankingsModule option.
      result.rankings = parseHeaderNavAccess(value, result.rankings)
      return
    }

    const fallback = result[key]
    if (
      typeof fallback === 'boolean' ||
      typeof value === 'boolean' ||
      typeof value === 'number' ||
      typeof value === 'string'
    ) {
      result[key] = parseHeaderNavBoolean(
        value,
        typeof fallback === 'boolean' ? fallback : true
      )
    }
  })

  return result
}

export function parseHeaderNavModulesFromStatus(
  status: Record<string, unknown> | null
): HeaderNavModulesConfig {
  const modules = parseHeaderNavModules(status?.HeaderNavModules)
  if (status?.RankingsModule !== undefined) {
    modules.rankings = parseHeaderNavAccessOption(
      status.RankingsModule,
      modules.rankings
    )
  }
  return modules
}

export function serializeHeaderNavModules(
  config: HeaderNavModulesConfig
): string {
  return JSON.stringify(config)
}
