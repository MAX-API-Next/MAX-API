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
import { getDashboardQueryData } from './overview-query-utils'

describe('getDashboardQueryData', () => {
  test('returns successful response data', () => {
    assert.deepEqual(
      getDashboardQueryData({ success: true, data: ['model-a', 'model-b'] }),
      ['model-a', 'model-b']
    )
  })

  test('throws the backend message for a business failure', () => {
    assert.throws(
      () =>
        getDashboardQueryData({
          success: false,
          message: 'models unavailable',
        }),
      /models unavailable/
    )
  })

  test('uses the generic message when the backend omits one', () => {
    assert.throws(
      () => getDashboardQueryData({ success: false }),
      /Request failed/
    )
  })
})
