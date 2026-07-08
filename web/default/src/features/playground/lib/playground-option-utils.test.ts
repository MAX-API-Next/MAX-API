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
  getGroupFallback,
  getModelFallback,
  shouldClearModelForGroup,
} from './playground-option-utils'

describe('playground-option-utils', () => {
  test('keeps the current model when it is available in the selected group', () => {
    assert.equal(
      getModelFallback([{ label: 'gpt-4o', value: 'gpt-4o' }], 'gpt-4o'),
      null
    )
  })

  test('falls back to the first model when the current model is not in the selected group', () => {
    assert.equal(
      getModelFallback(
        [{ label: 'gpt-4o-mini', value: 'gpt-4o-mini' }],
        'gpt-4o'
      ),
      'gpt-4o-mini'
    )
  })

  test('does not choose a model fallback from an empty model list', () => {
    assert.equal(getModelFallback([], 'gpt-4o'), null)
    assert.equal(shouldClearModelForGroup([], 'gpt-4o'), true)
  })

  test('prefers the default group when the current group is unavailable', () => {
    assert.equal(
      getGroupFallback(
        [
          { label: 'vip', value: 'vip', ratio: 2 },
          { label: 'default', value: 'default', ratio: 1 },
        ],
        'stale'
      ),
      'default'
    )
  })

  test('falls back to the first group when default is unavailable', () => {
    assert.equal(
      getGroupFallback([{ label: 'vip', value: 'vip', ratio: 2 }], 'stale'),
      'vip'
    )
  })
})
