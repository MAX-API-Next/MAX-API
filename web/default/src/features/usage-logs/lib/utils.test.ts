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
import { describe, test } from 'node:test'
import { buildApiParams, matchesCommonLogTypeFilter } from './utils'

describe('buildApiParams', () => {
  test('treats mixed retry and numeric type filters as retry filters', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 100,
      searchParams: { type: ['retry', '1'] },
      isAdmin: true,
    })

    assert.equal(params.log_filter, 'retry')
    assert.equal(params.type, undefined)
  })

  test('maps retry subtype filters to log_filter', () => {
    const emptyParams = buildApiParams({
      page: 1,
      pageSize: 100,
      searchParams: { type: ['empty_retry'] },
      isAdmin: true,
    })
    assert.equal(emptyParams.log_filter, 'empty_retry')
    assert.equal(emptyParams.type, undefined)

    const errorParams = buildApiParams({
      page: 1,
      pageSize: 100,
      searchParams: { type: ['error_retry'] },
      isAdmin: true,
    })
    assert.equal(errorParams.log_filter, 'error_retry')
    assert.equal(errorParams.type, undefined)
  })

  test('treats mixed retry and numeric column filters as retry filters', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 100,
      searchParams: {},
      columnFilters: [{ id: 'type', value: ['retry', '1'] }],
      isAdmin: true,
    })

    assert.equal(params.log_filter, 'retry')
    assert.equal(params.type, undefined)
  })

  test('ignores blank type filter array entries', () => {
    const blankParams = buildApiParams({
      page: 1,
      pageSize: 100,
      searchParams: { type: [''] },
      isAdmin: true,
    })
    assert.equal(blankParams.type, undefined)
    assert.equal(blankParams.log_filter, undefined)

    const numericParams = buildApiParams({
      page: 1,
      pageSize: 100,
      searchParams: { type: ['', '2'] },
      isAdmin: true,
    })
    assert.equal(numericParams.type, 2)
    assert.equal(numericParams.log_filter, undefined)
  })

  test('treats all as match-all before retry filters', () => {
    for (const type of [
      ['0', 'retry'],
      ['all', 'retry'],
    ]) {
      const params = buildApiParams({
        page: 1,
        pageSize: 100,
        searchParams: { type },
        isAdmin: true,
      })

      assert.equal(params.type, undefined)
      assert.equal(params.log_filter, undefined)
    }
  })
})

describe('matchesCommonLogTypeFilter', () => {
  test('matches retry filters against row retry markers', () => {
    assert.equal(
      matchesCommonLogTypeFilter({ type: 2, other: '{}', is_retry: true }, [
        'retry',
      ]),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ retry_log: true }) },
        ['retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ empty_retry: true }) },
        ['retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ admin_info: { use_channel: ['1'] } }) },
        ['retry']
      ),
      false
    )
  })

  test('matches retry subtype filters against row retry markers', () => {
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: '{}', is_error_retry: true },
        ['error_retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ retry_log: true }) },
        ['error_retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        {
          type: 2,
          other: JSON.stringify({ retry_log: true, empty_retry: true }),
        },
        ['error_retry']
      ),
      false
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ empty_retry: true }) },
        ['error_retry']
      ),
      false
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: '{}', is_empty_retry: true },
        ['empty_retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ empty_retry: true }) },
        ['empty_retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        {
          type: 2,
          other: JSON.stringify({ retry_log: true, empty_retry: true }),
        },
        ['empty_retry']
      ),
      true
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 2, other: JSON.stringify({ retry_log: true }) },
        ['empty_retry']
      ),
      false
    )
  })

  test('gives retry precedence for mixed retry and numeric filters', () => {
    assert.equal(
      matchesCommonLogTypeFilter({ type: 1, other: '{}' }, ['retry', '1']),
      false
    )
    assert.equal(
      matchesCommonLogTypeFilter(
        { type: 1, other: JSON.stringify({ retry_log: true }) },
        ['retry', '1']
      ),
      true
    )
  })
})
