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
import type { BillingSettlementReconciliationItem } from '../types'
import {
  getBillingSettlementSelectionPartition,
  isManualSettlementSelectable,
} from './use-billing-settlement-selection'

function item(
  id: number,
  overrides: Partial<BillingSettlementReconciliationItem> = {}
): BillingSettlementReconciliationItem {
  return {
    id,
    revision: 1,
    operation_key: `settlement:${id}`,
    status: 'pending',
    source: 'wallet',
    user_id: 1,
    subscription_id: 0,
    token_id: 0,
    task_id: 0,
    task_quota: 100,
    task_quota_target: 100,
    requires_manual_completion: false,
    funding_delta: 0,
    applied_funding_delta: 0,
    token_delta: 0,
    applied_token_delta: 0,
    attempts: 1,
    last_error: '',
    next_attempt: 0,
    created_at: 1,
    updated_at: 1,
    reconciliation_reviewed_at: 0,
    reconciliation_reviewed_by: 0,
    reconciliation_review_note: '',
    user_blocking_override: null,
    record_blocks_user: false,
    blocks_user: false,
    ...overrides,
  }
}

describe('billing settlement selection partition', () => {
  test('prefers ordinary alerts when both partitions are selectable', () => {
    const ordinary = item(1)
    const zero = item(2, {
      requires_manual_completion: true,
      zero_quota_eligible: true,
    })

    assert.deepEqual(
      getBillingSettlementSelectionPartition([ordinary, zero], true).map(
        (value) => value.id
      ),
      [1]
    )
  })

  test('selects all manual task alerts when they are the only selectable partition', () => {
    const exact = item(1, { requires_manual_completion: true })
    const zero = item(2, {
      requires_manual_completion: true,
      zero_quota_eligible: true,
    })

    assert.equal(isManualSettlementSelectable(exact, true), true)
    assert.equal(isManualSettlementSelectable(exact, false), false)
    assert.equal(isManualSettlementSelectable(item(3), false), true)
    assert.deepEqual(
      getBillingSettlementSelectionPartition([exact, zero], false),
      []
    )
    assert.deepEqual(
      getBillingSettlementSelectionPartition([exact, zero], true).map(
        (value) => value.id
      ),
      [1, 2]
    )
  })
})
