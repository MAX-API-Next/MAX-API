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
import type { TopNavItem, TopNavLink } from '../types'

/**
 * Default top navigation links
 *
 * In practice, navigation links are dynamically fetched from backend.
 * Priority: Backend dynamic links > Provided navLinks > defaultTopNavLinks
 *
 * This is intentionally empty to encourage backend configuration.
 * If you need fallback links, add them here.
 */
export const defaultTopNavLinks: TopNavLink[] = []

/** Built-in and legacy links may have no saved ID. Count only identical keys. */
export function keyedTopNavItems<T extends TopNavItem>(items: T[]) {
  const legacyGroupTitles = new Set<string>()
  const repeatedTitles = new Set<string>()
  for (const item of items) {
    if (item.id || !('children' in item)) continue
    if (legacyGroupTitles.has(item.title)) repeatedTitles.add(item.title)
    legacyGroupTitles.add(item.title)
  }
  const occurrences = new Map<string, number>()
  return items.map((link) => {
    let identity: string
    if (link.id) {
      identity = JSON.stringify(['id', link.id])
    } else if ('href' in link) {
      identity = JSON.stringify(['link', link.title, link.href])
    } else {
      const destinations = repeatedTitles.has(link.title)
        ? link.children
            .map((child) => JSON.stringify([child.title, child.href]))
            .sort()
        : undefined
      identity = JSON.stringify(['group', link.title, destinations])
    }
    const occurrence = occurrences.get(identity) ?? 0
    occurrences.set(identity, occurrence + 1)
    return { key: `${identity}:${occurrence}`, link }
  })
}
