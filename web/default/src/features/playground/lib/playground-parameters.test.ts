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
  getParameterControlValueText,
  normalizeParameterNumberValue,
} from './playground-parameters'

describe('playground parameter controls', () => {
  test('clamps decimal and integer values to supported ranges', () => {
    assert.equal(normalizeParameterNumberValue('temperature', 2.8), 2)
    assert.equal(normalizeParameterNumberValue('top_p', -1), 0)
    assert.equal(normalizeParameterNumberValue('frequency_penalty', 0.26), 0.3)
    assert.equal(normalizeParameterNumberValue('max_tokens', 200000.9), 200000)
    assert.equal(normalizeParameterNumberValue('seed', 2147483648), 2147483647)
  })

  test('preserves the optional seed empty state', () => {
    assert.equal(normalizeParameterNumberValue('seed', ''), null)
    assert.equal(getParameterControlValueText('seed', null), 'Not Set')
    assert.equal(normalizeParameterNumberValue('temperature', ''), 0)
  })

  test('falls back to the control minimum for empty or invalid values', () => {
    assert.equal(normalizeParameterNumberValue('max_tokens', ''), 1)
    assert.equal(normalizeParameterNumberValue('max_tokens', 'invalid'), 1)
    assert.equal(
      normalizeParameterNumberValue('frequency_penalty', 'invalid'),
      -2
    )
    assert.equal(normalizeParameterNumberValue('seed', 'invalid'), null)
  })
})
