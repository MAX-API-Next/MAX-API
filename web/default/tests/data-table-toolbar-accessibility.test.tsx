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
import assert from 'node:assert/strict'
import { afterAll, beforeAll, describe, test } from 'bun:test'
import { DataTableToolbar } from '../src/components/data-table/toolbar'
import { createReactTestEnvironment } from '../src/test/react'

const testEnv = createReactTestEnvironment()

beforeAll(async () => {
  await testEnv.setup()
})

afterAll(() => testEnv.teardown())

describe('DataTableToolbar accessibility', () => {
  test('gives the submit-mode search input an accessible name', async () => {
    const view = await testEnv.render(
      <DataTableToolbar
        table={{
          getState: () => ({ columnFilters: [], globalFilter: '' }),
        } as never}
        onSearch={() => undefined}
        searchPlaceholder='Search accounts'
        searchValue=''
        onSearchValueChange={() => undefined}
        hideViewOptions
      />
    )

    const input = view.container.querySelector('input')
    assert.ok(input)
    assert.equal(input.getAttribute('aria-label'), 'Search accounts')

    await view.unmount()
  })
})
