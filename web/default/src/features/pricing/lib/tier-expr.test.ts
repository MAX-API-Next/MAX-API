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
import { evalExprLocally, type ExtraTokenValues } from './tier-expr'

const emptyExtraTokens: ExtraTokenValues = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

describe('evalExprLocally', () => {
  test('evaluates backend-style ternaries and logical operators', () => {
    const result = evalExprLocally(
      'v1:p <= 100 && c > 0 ? tier("small", p * 2 + c * 3) : tier("large", p * 4)',
      100,
      10,
      emptyExtraTokens
    )

    assert.deepEqual(result, { cost: 230, matchedTier: 'small', error: null })
  })

  test('does not expose browser globals or member access', () => {
    const result = evalExprLocally(
      'globalThis.document ? 999 : 1',
      100,
      10,
      emptyExtraTokens
    )

    assert.equal(result.cost, 0)
    assert.equal(result.matchedTier, '')
    assert.ok(result.error)
  })
})
