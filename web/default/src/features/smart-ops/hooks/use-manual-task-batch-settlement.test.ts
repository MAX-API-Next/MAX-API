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
import type {
  ManualTaskBillingBatchCompletionData,
  ManualTaskBillingBatchFailure,
} from '../types'
import { mergeManualTaskBatchFailures } from './use-manual-task-batch-settlement'

function failure(
  settlement_id: number,
  code: string
): ManualTaskBillingBatchFailure {
  return { settlement_id, code, message: code }
}

describe('manual task batch failure state', () => {
  test('keeps unresolved failures and replaces resolved entries', () => {
    const current = [
      failure(94, 'record_conflict'),
      failure(95, 'manual_review_required'),
    ]
    const data: ManualTaskBillingBatchCompletionData = {
      completed_count: 1,
      failed_count: 1,
      settlement_ids: [95],
      failed: [failure(94, 'invalid_settlement_request')],
    }

    assert.deepEqual(mergeManualTaskBatchFailures(current, data), [
      failure(94, 'invalid_settlement_request'),
    ])
  })
})
