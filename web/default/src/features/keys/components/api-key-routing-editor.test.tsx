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
import { MAX_MANUAL_ROUTING_GROUPS } from '../lib/api-key-form'
import { ApiKeyRoutingEditor } from './api-key-routing-editor'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

const groups = Array.from(
  { length: MAX_MANUAL_ROUTING_GROUPS },
  (_, index) => `group-${index + 1}`
)

describe('ApiKeyRoutingEditor', () => {
  test('keeps the maximum manual selection inside one full-width trigger', async () => {
    const view = await testEnv.render(
      <ApiKeyRoutingEditor
        mode='manual'
        route='auto'
        manualGroups={groups}
        retryOnFailure
        autoRouteOptions={[]}
        realGroupOptions={groups.map((group) => ({
          value: group,
          label: group,
          desc: group,
          ratio: 1,
        }))}
        defaultManualGroups={groups}
        onModeChange={() => undefined}
        onRouteChange={() => undefined}
        onManualGroupsChange={() => undefined}
        onRetryOnFailureChange={() => undefined}
      />
    )

    try {
      const trigger =
        view.container.querySelector<HTMLElement>('[role="combobox"]')
      assert.ok(trigger)
      assert.match(trigger.className, /\bw-full\b/)
      assert.ok(trigger.querySelector('.lucide-chevrons-up-down'))
      assert.equal(view.container.querySelector('.lucide-x'), null)
      for (const group of groups) {
        assert.match(trigger.textContent || '', new RegExp(group))
        const removeButton = view.container.querySelector(
          `button[aria-label="Remove ${group}"]`
        )
        assert.ok(removeButton)
        assert.ok(removeButton.querySelector('.lucide-trash-2'))
        assert.equal(trigger.contains(removeButton), false)
      }
    } finally {
      await view.unmount()
    }
  })
})
