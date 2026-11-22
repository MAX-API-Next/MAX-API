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
import * as z from 'zod'
import { isSupportedNavHref } from '@/lib/custom-nav-links'
import type { CustomNavLink } from '@/lib/header-nav-modules'

const navLink = z.object({
  id: z.string().optional(),
  title: z.string().trim(),
  href: z.string().trim(),
})

export const headerNavSchema = z
  .object({
    home: z.boolean(),
    console: z.boolean(),
    pricingEnabled: z.boolean(),
    pricingRequireAuth: z.boolean(),
    rankingsEnabled: z.boolean(),
    rankingsRequireAuth: z.boolean(),
    docs: z.boolean(),
    about: z.boolean(),
    customEnabled: z.boolean(),
    customItems: z.array(navLink.extend({ children: z.array(navLink) })),
  })
  .superRefine((values, ctx) => {
    if (!values.customEnabled) return
    function validateLink(
      link: CustomNavLink,
      path: (number | string)[],
      hrefOptional: boolean
    ) {
      if (!link.title) {
        ctx.addIssue({
          code: 'custom',
          path: [...path, 'title'],
          message: 'Required',
        })
      }
      if (!(hrefOptional && !link.href) && !isSupportedNavHref(link.href)) {
        ctx.addIssue({
          code: 'custom',
          path: [...path, 'href'],
          message: 'Enter a site path or an HTTP(S) URL.',
        })
      }
    }
    values.customItems.forEach((item, index) => {
      const path = ['customItems', index]
      validateLink(item, path, item.children.length > 0)
      item.children.forEach((child, childIndex) => {
        validateLink(child, [...path, 'children', childIndex], false)
      })
    })
  })

export type HeaderNavFormValues = z.infer<typeof headerNavSchema>
