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
import { DEFAULT_AUTO_ROUTE_KEY, type AutoGroupRoute } from '@/lib/auto-routes'

export type PricingAutoRouteChain = {
  route: AutoGroupRoute
  groups: string[]
}

function normalizeDisplayGroups(
  groups: string[],
  groupFilter?: (group: string) => boolean
): string[] {
  const seen = new Set<string>()
  const normalized: string[] = []

  for (const rawGroup of groups) {
    const group = rawGroup.trim()
    if (!group || seen.has(group)) continue
    if (groupFilter && !groupFilter(group)) continue
    seen.add(group)
    normalized.push(group)
  }

  return normalized
}

function getFallbackAutoRoute(autoGroups: string[]): AutoGroupRoute | null {
  const groups = normalizeDisplayGroups(autoGroups)
  if (groups.length === 0) return null

  return {
    key: DEFAULT_AUTO_ROUTE_KEY,
    enabled: true,
    user_selectable: true,
    groups,
  }
}

export function getAutoRouteLabelOverride(
  route: AutoGroupRoute
): string | undefined {
  const name = route.name?.trim()
  if (
    route.key === DEFAULT_AUTO_ROUTE_KEY &&
    (!name || name === 'Auto' || name === DEFAULT_AUTO_ROUTE_KEY)
  ) {
    return undefined
  }
  return name || route.key
}

export function getConfiguredAutoRouteChains(options: {
  autoGroups: string[]
  autoRoutes?: AutoGroupRoute[]
  groupFilter?: (group: string) => boolean
}): PricingAutoRouteChain[] {
  const fallbackRoute = getFallbackAutoRoute(options.autoGroups)
  const routes =
    options.autoRoutes !== undefined
      ? options.autoRoutes
      : fallbackRoute
        ? [fallbackRoute]
        : []

  return routes
    .filter((route) => route.enabled)
    .map((route) => ({
      route,
      groups: normalizeDisplayGroups(
        Array.isArray(route.groups) ? route.groups : [],
        options.groupFilter
      ),
    }))
    .filter((item) => item.groups.length > 0)
}
