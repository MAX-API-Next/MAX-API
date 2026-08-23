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

  test('evaluates time-based pricing expressions with backend time functions', () => {
    const result = evalExprLocally(
      'hour("Asia/Shanghai") < 9 || (hour("Asia/Shanghai") >= 12 && hour("Asia/Shanghai") < 14) || hour("Asia/Shanghai") >= 18 ? tier("平常时段 0-9/12-14/18-24", p * 4.5 + c * 13.5 + cr * 0.15) : tier("高峰期 9-12/14-18", p * 9 + c * 27 + cr * 0.3)',
      100,
      10,
      emptyExtraTokens
    )

    assert.equal(result.error, null)
    assert.ok(
      [
        {
          cost: 585,
          matchedTier: '平常时段 0-9/12-14/18-24',
        },
        {
          cost: 1170,
          matchedTier: '高峰期 9-12/14-18',
        },
      ].some(
        (expected) =>
          expected.cost === result.cost &&
          expected.matchedTier === result.matchedTier
      )
    )
  })

  test('keeps time helpers in backend ranges and falls back to UTC', () => {
    const utcFallbackChecks = ['hour', 'minute', 'weekday', 'month', 'day']
      .flatMap((helper) => [
        `${helper}("Invalid/Zone") == ${helper}("UTC")`,
        `${helper}("") == ${helper}("UTC")`,
      ])
      .join(' && ')
    const utcRangeChecks = [
      'hour("UTC") >= 0 && hour("UTC") <= 23',
      'minute("UTC") >= 0 && minute("UTC") <= 59',
      'weekday("UTC") >= 0 && weekday("UTC") <= 6',
      'month("UTC") >= 1 && month("UTC") <= 12',
      'day("UTC") >= 1 && day("UTC") <= 31',
    ].join(' && ')
    const result = evalExprLocally(
      `${utcFallbackChecks} && ${utcRangeChecks} ? tier("valid", p) : tier("invalid", 999)`,
      42,
      0,
      emptyExtraTokens
    )

    assert.deepEqual(result, { cost: 42, matchedTier: 'valid', error: null })
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
