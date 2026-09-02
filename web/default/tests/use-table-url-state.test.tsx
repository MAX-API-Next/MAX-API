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
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, describe, mock, test } from 'bun:test'
import { createReactTestEnvironment } from '../src/test/react'
import {
  useTableUrlState,
  type NavigateFn,
} from '../src/hooks/use-table-url-state'

const testEnv = createReactTestEnvironment()

beforeAll(async () => {
  await testEnv.setup()
})

afterEach(() => cleanup())

afterAll(() => testEnv.teardown())

describe('useTableUrlState submit-mode global filter', () => {
  test('keeps typing local and commits only when explicitly applied', () => {
    const navigate = mock<NavigateFn>(() => undefined)
    const { result } = renderHook(() =>
      useTableUrlState({
        search: { filter: 'alice', page: 3 },
        navigate,
        globalFilter: { enabled: true, key: 'filter', mode: 'submit' },
      })
    )

    act(() => result.current.onGlobalFilterInputChange?.('alice-admin'))

    assert.equal(result.current.globalFilter, 'alice')
    assert.equal(result.current.globalFilterInput, 'alice-admin')
    assert.equal(navigate.mock.calls.length, 0)

    act(() => result.current.applyGlobalFilter?.())

    assert.equal(result.current.globalFilter, 'alice-admin')
    assert.equal(result.current.globalFilterInput, 'alice-admin')
    assert.equal(navigate.mock.calls.length, 1)

    const search = navigate.mock.calls[0]?.[0].search
    assert.equal(typeof search, 'function')
    if (typeof search === 'function') {
      const next = search({ filter: 'alice', page: 3 })
      assert.equal(next.filter, 'alice-admin')
      assert.equal(next.page, undefined)
    }
  })

  test('merges additional submitted filters into one URL update', () => {
    const navigate = mock<NavigateFn>(() => undefined)
    const { result } = renderHook(() =>
      useTableUrlState({
        search: { filter: '', page: 2, model: 'old' },
        navigate,
        globalFilter: { enabled: true, key: 'filter', mode: 'submit' },
      })
    )

    act(() => result.current.onGlobalFilterInputChange?.('model name'))
    act(() => result.current.applyGlobalFilter?.({ model: 'gpt-5' }))

    assert.equal(navigate.mock.calls.length, 1)
    const search = navigate.mock.calls[0]?.[0].search
    assert.equal(typeof search, 'function')
    if (typeof search === 'function') {
      const next = search({ filter: '', page: 2, model: 'old' })
      assert.equal(next.filter, 'model name')
      assert.equal(next.model, 'gpt-5')
      assert.equal(next.page, undefined)
    }
  })

  test('synchronizes committed and draft values after external URL changes', async () => {
    const navigate = mock<NavigateFn>(() => undefined)
    const { result, rerender } = renderHook(
      ({ search }: { search: Record<string, unknown> }) =>
        useTableUrlState({
          search,
          navigate,
          globalFilter: { enabled: true, key: 'filter', mode: 'submit' },
        }),
      { initialProps: { search: { filter: 'before' } } }
    )

    act(() => result.current.onGlobalFilterInputChange?.('draft'))
    rerender({ search: { filter: 'after' } })

    await waitFor(() => {
      assert.equal(result.current.globalFilter, 'after')
      assert.equal(result.current.globalFilterInput, 'after')
    })
  })

  test('clears related submitted filters when resetting the global filter', () => {
    const navigate = mock<NavigateFn>(() => undefined)
    const { result } = renderHook(() =>
      useTableUrlState({
        search: { filter: 'alice', token: 'secret', page: 2 },
        navigate,
        globalFilter: { enabled: true, key: 'filter', mode: 'submit' },
      })
    )

    act(() => result.current.resetGlobalFilter?.({ token: undefined }))

    assert.equal(navigate.mock.calls.length, 1)
    const search = navigate.mock.calls[0]?.[0].search
    assert.equal(typeof search, 'function')
    if (typeof search === 'function') {
      const next = search({ filter: 'alice', token: 'secret', page: 2 })
      assert.equal(next.filter, undefined)
      assert.equal(next.token, undefined)
      assert.equal(next.page, undefined)
    }
  })
})
