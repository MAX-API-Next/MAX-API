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
import { StatusCodeRiskConfirmationContent } from './status-code-risk-dialog'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

describe('StatusCodeRiskDialog', () => {
  test('keeps risk notices and invokes both available actions', async () => {
    let cancelCount = 0
    let confirmCount = 0
    const view = await testEnv.render(
      <StatusCodeRiskConfirmationContent
        detailItems={['200 -> 500']}
        onCancel={() => {
          cancelCount += 1
        }}
        onConfirm={() => {
          confirmCount += 1
        }}
      />
    )

    try {
      assert.match(view.container.textContent || '', /200 -> 500/)
      for (let index = 1; index <= 4; index += 1) {
        assert.match(
          view.container.textContent || '',
          new RegExp(`High-risk status code retry risk check ${index}`)
        )
      }

      assert.equal(view.container.querySelector('input'), null)
      assert.equal(view.container.querySelector('[type="checkbox"]'), null)

      const buttons = Array.from(view.container.querySelectorAll('button'))
      const cancelButton = buttons.find(
        (button) => button.textContent === 'Cancel'
      )
      const confirmButton = buttons.find(
        (button) => button.textContent === 'I confirm enabling high-risk retry'
      )

      assert.ok(cancelButton)
      assert.ok(confirmButton)
      assert.equal(confirmButton.disabled, false)

      await view.click(cancelButton)
      assert.equal(cancelCount, 1)
      assert.equal(confirmCount, 0)

      await view.click(confirmButton)

      assert.equal(cancelCount, 1)
      assert.equal(confirmCount, 1)
    } finally {
      await view.unmount()
    }
  })
})
