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
import type { TopNavItem, TopNavLink } from '@/components/layout/types'
import type { CustomNavLink, HeaderNavCustomConfig } from './header-nav-modules'
import { normalizeInternalRedirect } from './safe-redirect'

export function isSupportedNavHref(href: string): boolean {
  if (href.startsWith('/')) return Boolean(normalizeInternalRedirect(href, ''))
  if (!/^https?:\/\//i.test(href)) return false
  try {
    new URL(href)
    return true
  } catch {
    return false
  }
}

function toTopNavLink(link: CustomNavLink): TopNavLink[] {
  if (!link.title || !isSupportedNavHref(link.href)) return []
  return [
    {
      ...link,
      external: !link.href.startsWith('/'),
      literalTitle: true,
    },
  ]
}

export function getCustomNavLinks(custom: HeaderNavCustomConfig): TopNavItem[] {
  if (!custom.enabled) return []
  return custom.items.flatMap((item): TopNavItem[] => {
    if (!item.title) return []
    const parent = toTopNavLink({ title: item.title, href: item.href })
    const children = item.children.flatMap(toTopNavLink)
    if (!children.length) return parent
    return [{ title: item.title, children: [...parent, ...children] }]
  })
}
