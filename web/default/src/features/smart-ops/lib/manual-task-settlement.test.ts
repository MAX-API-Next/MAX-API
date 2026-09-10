/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import type { TFunction } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getManualTaskSettlementSchema } from './manual-task-settlement'

const t = ((value: string) => value) as unknown as TFunction

describe('manual task settlement schema', () => {
  const schema = getManualTaskSettlementSchema(t, 100)

  test('accepts an exact bounded quota and trims the audit note', () => {
    const result = schema.safeParse({
      actualQuota: '40',
      note: '  Verified provider usage.  ',
    })

    assert.equal(result.success, true)
    if (result.success) {
      assert.deepEqual(result.data, {
        actualQuota: '40',
        note: 'Verified provider usage.',
      })
    }
  })

  test('accepts the exact reservation cap', () => {
    const result = schema.safeParse({
      actualQuota: '100',
      note: 'Verified full reservation usage.',
    })

    assert.equal(result.success, true)
  })

  test('rejects empty quota', () => {
    assert.equal(
      schema.safeParse({ actualQuota: '', note: 'valid note' }).success,
      false
    )
  })

  test('rejects fractional quota', () => {
    assert.equal(
      schema.safeParse({ actualQuota: '1.5', note: 'valid note' }).success,
      false
    )
  })

  test('rejects negative quota', () => {
    assert.equal(
      schema.safeParse({ actualQuota: '-1', note: 'valid note' }).success,
      false
    )
  })

  test('rejects over-reservation quota', () => {
    assert.equal(
      schema.safeParse({ actualQuota: '101', note: 'valid note' }).success,
      false
    )
  })

  test('rejects a short audit note', () => {
    assert.equal(
      schema.safeParse({ actualQuota: '0', note: 'x' }).success,
      false
    )
  })

  test('rejects an unsafe integer below the cap', () => {
    const largeSchema = getManualTaskSettlementSchema(t, Number.MAX_VALUE)

    assert.equal(
      largeSchema.safeParse({
        actualQuota: '9007199254740992',
        note: 'valid note',
      }).success,
      false
    )
  })

  test('rejects non-decimal quota representations', () => {
    for (const actualQuota of ['1e1', '0x10', '+5']) {
      assert.equal(
        schema.safeParse({ actualQuota, note: 'valid note' }).success,
        false
      )
    }
  })

  test('preserves an explicit zero quota', () => {
    const result = schema.safeParse({
      actualQuota: '0',
      note: 'Verified zero usage.',
    })

    assert.equal(result.success, true)
  })
})
