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
import { isSafeQuotaAdjustment, isSafeQuotaValue } from './quota-safety'

describe('quota safety', () => {
  test('accepts only JavaScript-safe integer quota values', () => {
    assert.equal(isSafeQuotaValue(Number.MAX_SAFE_INTEGER), true)
    assert.equal(isSafeQuotaValue(Number.MAX_SAFE_INTEGER + 1), false)
    assert.equal(isSafeQuotaValue(1.5), false)
    assert.equal(isSafeQuotaValue(Number.POSITIVE_INFINITY), false)
  })

  test('rejects adjustments whose result leaves the safe integer range', () => {
    assert.equal(
      isSafeQuotaAdjustment(Number.MAX_SAFE_INTEGER - 1, 'add', 1),
      true
    )
    assert.equal(
      isSafeQuotaAdjustment(Number.MAX_SAFE_INTEGER, 'add', 1),
      false
    )
    assert.equal(
      isSafeQuotaAdjustment(-Number.MAX_SAFE_INTEGER + 1, 'subtract', 1),
      true
    )
    assert.equal(
      isSafeQuotaAdjustment(-Number.MAX_SAFE_INTEGER, 'subtract', 1),
      false
    )
    assert.equal(isSafeQuotaAdjustment(0, 'override', 0), true)
    assert.equal(
      isSafeQuotaAdjustment(Number.MAX_SAFE_INTEGER + 1, 'override', 100),
      true
    )
  })
})
