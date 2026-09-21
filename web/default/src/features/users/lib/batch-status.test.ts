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
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'
import type { BatchUserStatusResult, User } from '../types'
import {
  canBatchChangeUserStatus,
  normalizeBatchStatusResults,
} from './batch-status'

const user = { id: 3, username: 'normal', role: 1, status: 1 } as User

describe('User status batch safety', () => {
  test('protects self, Root, peers, higher roles and deleted accounts', () => {
    const admin = { id: 2, role: 10 }
    assert.equal(canBatchChangeUserStatus(user, admin), true)
    assert.equal(canBatchChangeUserStatus({ ...user, status: 2 }, admin), true)
    assert.equal(canBatchChangeUserStatus(user, null), false)
    assert.equal(canBatchChangeUserStatus(user, { id: 4, role: 1 }), false)
    assert.equal(canBatchChangeUserStatus(user, { id: 3, role: 100 }), false)
    for (const role of [10, 100])
      assert.equal(canBatchChangeUserStatus({ ...user, role }, admin), false)
    assert.equal(
      canBatchChangeUserStatus({ ...user, role: 100 }, { id: 2, role: 100 }),
      false
    )
    assert.equal(
      canBatchChangeUserStatus({ ...user, role: 10 }, { id: 2, role: 100 }),
      true
    )
    assert.equal(
      canBatchChangeUserStatus({ ...user, DeletedAt: '2026-01-01' }, admin),
      false
    )
    assert.equal(
      canBatchChangeUserStatus({ ...user, status: -1 }, admin),
      false
    )
  })

  test('matches partial results by ID and marks missing results unknown', () => {
    assert.deepEqual(
      normalizeBatchStatusResults(
        [3, 4, 5],
        [
          { id: 4, outcome: 'rejected', code: 'forbidden' },
          { id: 3, outcome: 'updated', status: 2 },
        ]
      ),
      [
        { id: 3, outcome: 'updated', status: 2 },
        { id: 4, outcome: 'rejected', code: 'forbidden' },
        { id: 5, outcome: 'unknown', code: 'unknown' },
      ]
    )
    assert.equal(
      normalizeBatchStatusResults([3], undefined)[0].outcome,
      'unknown'
    )
  })

  test('does not trust duplicate or malformed success entries', () => {
    assert.equal(
      normalizeBatchStatusResults(
        [3],
        [
          { id: 3, outcome: 'updated' },
          { id: 3, outcome: 'unchanged' },
        ]
      )[0].outcome,
      'unknown'
    )
    assert.equal(
      normalizeBatchStatusResults(
        [3],
        [{ id: 3, outcome: 'success' } as unknown as BatchUserStatusResult]
      )[0].outcome,
      'unknown'
    )
  })

  for (const locale of ['en', 'zh', 'fr', 'ja', 'ru', 'vi']) {
    test(`provides complete batch management translations in ${locale}`, () => {
      const translations = JSON.parse(
        readFileSync(
          new URL(`../../../i18n/locales/${locale}.json`, import.meta.url),
          'utf8'
        )
      ).translation
      for (const key of [
        'Batch disable',
        'Batch enable',
        'Updated',
        'Selected users',
        'No change needed',
        'Result unconfirmed. Refresh and verify before retrying.',
        'Protected account or insufficient permissions',
        'User not found or deleted',
        'Account status changed. Refresh and select again.',
        'Operation rejected',
        'Review the {{count}} selected accounts before confirming.',
        'Disabling blocks new sign-ins and API requests. Existing upstream tasks are not cancelled. Balances and records are kept.',
        'Enabling restores account access, including existing valid API keys. Balances and records are unchanged.',
        'The user list changed. Close this dialog and select accounts again.',
        '{{confirmed}} confirmed, {{remaining}} rejected or unconfirmed.',
        'Select at most {{count}} users per batch.',
      ]) {
        assert.ok(translations[key]?.trim(), `${locale}: ${key}`)
        assert.doesNotMatch(translations[key], /\?{2,}/)
        if (locale !== 'en') assert.notEqual(translations[key], key)
        for (const variable of key.match(/\{\{\w+\}\}/g) ?? [])
          assert.ok(translations[key].includes(variable))
      }
    })
  }
})
