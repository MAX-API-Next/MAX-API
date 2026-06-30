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
import {
  getAvailableOptionValue,
  isOptionValueAvailable,
} from './selection-state'

describe('selection-state', () => {
  test('keeps the current value when it remains available', () => {
    assert.equal(
      getAvailableOptionValue([{ value: 'gpt-4o' }], 'gpt-4o'),
      'gpt-4o'
    )
  })

  test('falls back to the first option when the current value is unavailable', () => {
    assert.equal(
      getAvailableOptionValue([{ value: 'gpt-4o-mini' }], 'gpt-4o'),
      'gpt-4o-mini'
    )
  })

  test('prefers the requested fallback value when it is available', () => {
    assert.equal(
      getAvailableOptionValue(
        [{ value: 'vip' }, { value: 'default' }],
        'stale',
        'default'
      ),
      'default'
    )
  })

  test('clears the value when no options are available', () => {
    assert.equal(getAvailableOptionValue([], 'stale'), '')
  })

  test('reports values unavailable for empty or unloaded options', () => {
    assert.equal(isOptionValueAvailable(undefined, 'gpt-4o'), false)
    assert.equal(isOptionValueAvailable([], 'gpt-4o'), false)
    assert.equal(isOptionValueAvailable([{ value: 'gpt-4o' }], ''), false)
  })

  test('reports values available only when they exist in the options', () => {
    const options = [{ value: 'gpt-4o' }]

    assert.equal(isOptionValueAvailable(options, 'gpt-4o'), true)
    assert.equal(isOptionValueAvailable(options, 'stale'), false)
  })
})
