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
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { PricingSidebar, type PricingSidebarProps } from './pricing-sidebar'

const testEnv = createReactTestEnvironment()

const sidebarProps: PricingSidebarProps = {
  quotaTypeFilter: 'all',
  endpointTypeFilter: 'all',
  vendorFilter: 'all',
  groupFilter: 'all',
  tagFilter: 'all',
  onQuotaTypeChange: () => undefined,
  onEndpointTypeChange: () => undefined,
  onVendorChange: () => undefined,
  onGroupChange: () => undefined,
  onTagChange: () => undefined,
  vendors: [],
  groups: ['vip', 'fallback'],
  groupRatios: { vip: 1, fallback: 0.5 },
  autoGroups: [],
  autoRoutes: [
    {
      key: 'auto:fast',
      name: 'Fast lane',
      enabled: true,
      user_selectable: true,
      groups: ['vip', 'fallback'],
    },
  ],
  tags: [],
  models: [],
  hasActiveFilters: false,
  onClearFilters: () => undefined,
}

before(() => testEnv.setup())

after(() => testEnv.teardown())

describe('PricingSidebar auto route chains', () => {
  test('keeps configured auto route details collapsed until requested', async () => {
    const view = await testEnv.render(<PricingSidebar {...sidebarProps} />)

    try {
      const trigger = Array.from(
        view.container.querySelectorAll('button')
      ).find((button) => button.textContent?.includes('Auto route chains'))

      assert.ok(trigger)
      assert.equal(trigger.getAttribute('aria-expanded'), 'false')
      assert.doesNotMatch(view.container.textContent || '', /Fast lane/)

      await view.click(trigger)

      assert.equal(trigger.getAttribute('aria-expanded'), 'true')
      assert.match(view.container.textContent || '', /Fast lane/)
    } finally {
      await view.unmount()
    }
  })
})
