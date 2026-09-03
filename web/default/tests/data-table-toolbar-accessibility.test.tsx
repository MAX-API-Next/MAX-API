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
import { afterAll, afterEach, describe, test } from 'bun:test'
import { I18nextProvider } from 'react-i18next'
import { DataTableToolbar } from '../src/components/data-table/toolbar'
import { createReactTestEnvironment } from '../src/test/react'

const testEnv = createReactTestEnvironment()
await testEnv.setup()
const { cleanup, render, screen } = await import('@testing-library/react')

afterEach(() => cleanup())

afterAll(() => testEnv.teardown())

describe('DataTableToolbar accessibility', () => {
  test('gives the submit-mode search input an accessible name', () => {
    render(
      <I18nextProvider i18n={testEnv.i18n}>
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
      </I18nextProvider>
    )

    assert.ok(screen.getByRole('textbox', { name: 'Search accounts' }))
  })
})
