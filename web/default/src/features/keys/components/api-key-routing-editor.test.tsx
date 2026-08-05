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
import { cleanup, fireEvent, render, within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, afterEach, before, describe, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import { MAX_MANUAL_ROUTING_GROUPS } from '../lib/api-key-form'
import { ApiKeyRoutingEditor } from './api-key-routing-editor'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

afterEach(() => cleanup())

after(() => testEnv.teardown())

const groups = Array.from(
  { length: MAX_MANUAL_ROUTING_GROUPS },
  (_, index) => `group-${index + 1}`
)

describe('ApiKeyRoutingEditor', () => {
  test('keeps the maximum manual selection in one accessible selector', () => {
    let changedGroups: string[] | undefined
    const view = render(
      <I18nextProvider i18n={testEnv.i18n}>
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
          onManualGroupsChange={(nextGroups) => {
            changedGroups = nextGroups
          }}
          onRetryOnFailureChange={() => undefined}
        />
      </I18nextProvider>
    )

    const trigger = view.getByRole('combobox', { name: 'Select groups' })
    assert.equal(trigger.getAttribute('aria-expanded'), 'false')
    for (const group of groups) {
      within(trigger).getByText(group)
    }

    fireEvent.click(trigger)

    assert.equal(trigger.getAttribute('aria-expanded'), 'true')

    const removeButtons = view.getAllByRole('button', {
      name: /^Remove group-/,
    })
    assert.equal(removeButtons.length, MAX_MANUAL_ROUTING_GROUPS)
    for (const removeButton of removeButtons) {
      assert.equal(trigger.contains(removeButton), false)
    }

    fireEvent.click(removeButtons[0])
    assert.deepEqual(changedGroups, groups.slice(1))
  })
})
