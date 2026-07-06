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
import i18next from 'i18next'

export const DEFAULT_AUTO_ROUTE_KEY = 'auto'

export type AutoGroupRoute = {
  key: string
  name?: string
  enabled: boolean
  user_selectable: boolean
  groups: string[]
}

export type AutoGroupRoutesConfig = {
  version: number
  default_route: string
  routes: AutoGroupRoute[]
}

const AUTO_ROUTE_KEY_PATTERN = /^auto(?::[A-Za-z0-9][A-Za-z0-9._-]{0,58})?$/

type ValidationMessageValues = Record<string, number | string>

function formatValidationMessage(
  template: string,
  values: ValidationMessageValues = {}
) {
  return template.replace(/{{\s*([A-Za-z0-9_]+)\s*}}/g, (match, key) => {
    const value = values[key]
    return value === undefined ? match : String(value)
  })
}

function autoRouteValidationMessage(
  key: string,
  values: ValidationMessageValues = {}
) {
  const fallback = formatValidationMessage(key, values)
  if (!i18next.isInitialized) return fallback
  const translated = i18next.t(key, values)
  return typeof translated === 'string' && translated ? translated : fallback
}

function autoRouteValidationError(
  key: string,
  values?: ValidationMessageValues
) {
  return new Error(autoRouteValidationMessage(key, values))
}

function normalizeRouteName(route: AutoGroupRoute) {
  const name = typeof route.name === 'string' ? route.name.trim() : ''
  if (name) return name
  return route.key === DEFAULT_AUTO_ROUTE_KEY ? undefined : route.key
}

function normalizeGroupList(groups: unknown): string[] {
  if (!Array.isArray(groups)) return []
  const seen = new Set<string>()
  const normalized: string[] = []
  for (const item of groups) {
    if (typeof item !== 'string') continue
    const group = item.trim()
    if (!group || isAutoRouteKey(group) || seen.has(group)) continue
    seen.add(group)
    normalized.push(group)
  }
  return normalized
}

function normalizeGroupListStrict(groups: unknown, context: string): string[] {
  if (!Array.isArray(groups)) {
    throw autoRouteValidationError(
      '{{routeContext}} groups must be an array',
      { routeContext: context }
    )
  }
  if (groups.length === 0) {
    throw autoRouteValidationError(
      '{{routeContext}} groups must not be empty',
      { routeContext: context }
    )
  }
  if (groups.length > 64) {
    throw autoRouteValidationError(
      '{{routeContext}} groups must not exceed 64 entries',
      { routeContext: context }
    )
  }
  const seen = new Set<string>()
  const normalized: string[] = []
  for (const item of groups) {
    if (typeof item !== 'string') {
      throw autoRouteValidationError(
        '{{routeContext}} groups must contain strings only',
        { routeContext: context }
      )
    }
    const group = item.trim()
    if (!group) continue
    if (isAutoRouteKey(group)) {
      throw autoRouteValidationError(
        '{{routeContext}} groups must contain real groups only',
        { routeContext: context }
      )
    }
    if (seen.has(group)) continue
    seen.add(group)
    normalized.push(group)
  }
  if (normalized.length === 0) {
    throw autoRouteValidationError(
      '{{routeContext}} groups must contain at least one real group',
      { routeContext: context }
    )
  }
  return normalized
}

export function isAutoRouteKey(value?: string | null) {
  const group = value?.trim() ?? ''
  return group === DEFAULT_AUTO_ROUTE_KEY || group.startsWith('auto:')
}

export function isValidAutoRouteKey(value: string) {
  return AUTO_ROUTE_KEY_PATTERN.test(value.trim())
}

export function createLegacyAutoRouteConfig(
  groups: string[]
): AutoGroupRoutesConfig {
  const normalizedGroups = normalizeGroupList(groups)
  return {
    version: 1,
    default_route: DEFAULT_AUTO_ROUTE_KEY,
    routes: [
      {
        key: DEFAULT_AUTO_ROUTE_KEY,
        enabled: true,
        user_selectable: true,
        groups: normalizedGroups.length > 0 ? normalizedGroups : ['default'],
      },
    ],
  }
}

export function normalizeAutoGroupRoutesConfig(
  config: Partial<AutoGroupRoutesConfig>
): AutoGroupRoutesConfig {
  const defaultRoute =
    typeof config.default_route === 'string' &&
    isValidAutoRouteKey(config.default_route)
      ? config.default_route.trim()
      : DEFAULT_AUTO_ROUTE_KEY
  const seen = new Set<string>()
  const routes: AutoGroupRoute[] = []

  for (const route of Array.isArray(config.routes) ? config.routes : []) {
    if (!route || typeof route.key !== 'string') continue
    const key = route.key.trim()
    if (!isValidAutoRouteKey(key) || seen.has(key)) continue
    const groups = normalizeGroupList(route.groups)
    if (groups.length === 0) continue
    const name = normalizeRouteName({ ...route, key })
    seen.add(key)
    routes.push({
      key,
      ...(name ? { name } : {}),
      enabled: route.enabled !== false,
      user_selectable: route.user_selectable !== false,
      groups,
    })
  }

  if (!seen.has(defaultRoute)) {
    const name =
      defaultRoute === DEFAULT_AUTO_ROUTE_KEY ? undefined : defaultRoute
    routes.unshift({
      key: defaultRoute,
      ...(name ? { name } : {}),
      enabled: true,
      user_selectable: true,
      groups: ['default'],
    })
  }

  return {
    version: 1,
    default_route: defaultRoute,
    routes,
  }
}

export function normalizeAutoGroupRoutesConfigStrict(
  config: unknown
): AutoGroupRoutesConfig {
  if (Array.isArray(config)) {
    const groups = normalizeGroupListStrict(config, 'auto')
    return createLegacyAutoRouteConfig(groups)
  }
  if (!config || typeof config !== 'object') {
    throw autoRouteValidationError(
      'auto group routes config must be an object'
    )
  }
  const rawConfig = config as Record<string, unknown>
  const version =
    typeof rawConfig.version === 'number' ? rawConfig.version : 1
  if (version !== 1) {
    throw autoRouteValidationError(
      'unsupported auto group routes config version: {{version}}',
      { version }
    )
  }
  const defaultRoute =
    typeof rawConfig.default_route === 'string'
      ? rawConfig.default_route.trim()
      : DEFAULT_AUTO_ROUTE_KEY
  if (!isValidAutoRouteKey(defaultRoute)) {
    throw autoRouteValidationError('invalid default auto route key: {{key}}', {
      key: defaultRoute,
    })
  }
  if (!Array.isArray(rawConfig.routes)) {
    throw autoRouteValidationError('auto group routes must be an array')
  }
  if (rawConfig.routes.length === 0) {
    throw autoRouteValidationError('auto group routes must not be empty')
  }
  if (rawConfig.routes.length > 32) {
    throw autoRouteValidationError(
      'auto group routes must not exceed 32 entries'
    )
  }

  const seen = new Set<string>()
  const routes: AutoGroupRoute[] = []
  let hasDefault = false
  let defaultEnabled = false
  for (const item of rawConfig.routes) {
    if (!item || typeof item !== 'object') {
      throw autoRouteValidationError('auto route must be an object')
    }
    const rawRoute = item as Record<string, unknown>
    const key = typeof rawRoute.key === 'string' ? rawRoute.key.trim() : ''
    if (!isValidAutoRouteKey(key)) {
      throw autoRouteValidationError('invalid auto route key: {{key}}', {
        key,
      })
    }
    if (seen.has(key)) {
      throw autoRouteValidationError('duplicate auto route key: {{key}}', {
        key,
      })
    }
    seen.add(key)
    const enabled = rawRoute.enabled !== false
    if (key === defaultRoute) {
      hasDefault = true
      defaultEnabled = enabled
    }

    const name = typeof rawRoute.name === 'string' ? rawRoute.name.trim() : ''
    if ([...name].length > 64) {
      throw autoRouteValidationError(
        'auto route {{key}} name must not exceed 64 characters',
        { key }
      )
    }
    const normalizedName =
      name || (key === DEFAULT_AUTO_ROUTE_KEY ? undefined : key)
    routes.push({
      key,
      ...(normalizedName ? { name: normalizedName } : {}),
      enabled,
      user_selectable: rawRoute.user_selectable !== false,
      groups: normalizeGroupListStrict(rawRoute.groups, `auto route ${key}`),
    })
  }
  if (!hasDefault) {
    throw autoRouteValidationError(
      'default auto route {{key}} is not defined',
      { key: defaultRoute }
    )
  }
  if (!defaultEnabled) {
    throw autoRouteValidationError(
      'default auto route {{key}} must be enabled',
      { key: defaultRoute }
    )
  }
  return {
    version: 1,
    default_route: defaultRoute,
    routes,
  }
}

export function parseAutoGroupRoutesConfig(
  value: string | undefined | null,
  legacyAutoGroups?: string | undefined | null
): AutoGroupRoutesConfig {
  const raw = value?.trim()
  if (raw) {
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        return createLegacyAutoRouteConfig(normalizeGroupList(parsed))
      }
      if (parsed && typeof parsed === 'object') {
        return normalizeAutoGroupRoutesConfig(
          parsed as Partial<AutoGroupRoutesConfig>
        )
      }
    } catch {
      // fall through to legacy value
    }
  }

  if (legacyAutoGroups?.trim()) {
    try {
      const parsed = JSON.parse(legacyAutoGroups)
      return createLegacyAutoRouteConfig(normalizeGroupList(parsed))
    } catch {
      // fall through to built-in default
    }
  }

  return createLegacyAutoRouteConfig(['default'])
}

export function parseAutoGroupRoutesConfigStrict(
  value: string | undefined | null
): AutoGroupRoutesConfig {
  const raw = value?.trim()
  if (!raw) {
    throw autoRouteValidationError('auto group routes config is empty')
  }
  try {
    return normalizeAutoGroupRoutesConfigStrict(JSON.parse(raw))
  } catch (error) {
    if (error instanceof SyntaxError) {
      throw autoRouteValidationError('Invalid auto route config')
    }
    throw error
  }
}

export function validateAutoGroupRoutesConfigString(
  value: string | undefined | null
) {
  try {
    parseAutoGroupRoutesConfigStrict(value)
    return { valid: true, message: '' }
  } catch (error) {
    return {
      valid: false,
      message:
        error instanceof Error
          ? error.message
          : autoRouteValidationMessage('Invalid auto route config'),
    }
  }
}

export function stringifyAutoGroupRoutesConfig(
  config: AutoGroupRoutesConfig,
  space?: number
) {
  return JSON.stringify(normalizeAutoGroupRoutesConfig(config), null, space)
}

export function getDefaultAutoRouteGroups(config: AutoGroupRoutesConfig) {
  return (
    config.routes.find((route) => route.key === config.default_route)?.groups ??
    []
  )
}
