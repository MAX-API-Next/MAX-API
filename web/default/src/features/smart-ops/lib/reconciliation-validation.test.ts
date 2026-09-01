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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { BillingSettlementReconciliationData } from '../types'
import { isBillingSettlementReconciliationData } from './reconciliation-validation'

function validData(): BillingSettlementReconciliationData {
  return {
    total_count: 1,
    pending_count: 1,
    manual_count: 0,
    open_alert_count: 1,
    blocking_record_count: 1,
    blocked_user_count: 1,
    block_user_by_default: true,
    oldest_created_at: 1,
    truncated: false,
    generated_at: 2,
    items: [
      {
        id: 3,
        revision: 1,
        operation_key: 'request:validation:finalize',
        status: 'pending',
        source: 'wallet',
        user_id: 4,
        subscription_id: 0,
        token_id: 5,
        task_id: 0,
        funding_delta: 10,
        applied_funding_delta: 2,
        token_delta: 10,
        applied_token_delta: 2,
        attempts: 1,
        last_error: '',
        next_attempt: 0,
        created_at: 1,
        updated_at: 2,
        reconciliation_reviewed_at: 0,
        reconciliation_reviewed_by: 0,
        reconciliation_review_note: '',
        user_blocking_override: null,
        record_blocks_user: true,
        blocks_user: true,
      },
    ],
  }
}

describe('isBillingSettlementReconciliationData', () => {
  test('accepts the complete reconciliation payload', (): void => {
    assert.equal(isBillingSettlementReconciliationData(validData()), true)
  })

  test('rejects null and incomplete reconciliation items', (): void => {
    const nullItemData = { ...validData(), items: [null] }
    const incompleteItem = {
      ...validData(),
      items: [{ ...validData().items[0], funding_delta: undefined }],
    }

    assert.equal(isBillingSettlementReconciliationData(nullItemData), false)
    assert.equal(isBillingSettlementReconciliationData(incompleteItem), false)
  })

  test('preserves enum, nullable boolean, and finite-number boundaries', (): void => {
    const item = validData().items[0]

    assert.equal(
      isBillingSettlementReconciliationData({
        ...validData(),
        items: [{ ...item, status: 'applied' }],
      }),
      false
    )
    assert.equal(
      isBillingSettlementReconciliationData({
        ...validData(),
        items: [{ ...item, user_blocking_override: 'false' }],
      }),
      false
    )
    assert.equal(
      isBillingSettlementReconciliationData({
        ...validData(),
        generated_at: Number.POSITIVE_INFINITY,
      }),
      false
    )
    assert.equal(
      isBillingSettlementReconciliationData({
        ...validData(),
        items: [{ ...item, funding_delta: Number.NaN }],
      }),
      false
    )
  })
})
