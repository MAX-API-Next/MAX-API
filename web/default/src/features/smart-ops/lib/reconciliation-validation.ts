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
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
} from '../types'

const ITEM_NUMBER_FIELDS = [
  'id',
  'user_id',
  'subscription_id',
  'token_id',
  'task_id',
  'funding_delta',
  'applied_funding_delta',
  'token_delta',
  'applied_token_delta',
  'attempts',
  'next_attempt',
  'created_at',
  'updated_at',
  'reconciliation_reviewed_at',
  'reconciliation_reviewed_by',
] as const

const ITEM_STRING_FIELDS = [
  'operation_key',
  'last_error',
  'reconciliation_review_note',
] as const

const DATA_NUMBER_FIELDS = [
  'total_count',
  'pending_count',
  'manual_count',
  'open_alert_count',
  'reviewed_count',
  'blocking_record_count',
  'blocked_user_count',
  'oldest_created_at',
  'generated_at',
] as const

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasFiniteNumberFields(
  value: Record<string, unknown>,
  fields: readonly string[]
): boolean {
  return fields.every((field) => Number.isFinite(value[field]))
}

function hasStringFields(
  value: Record<string, unknown>,
  fields: readonly string[]
): boolean {
  return fields.every((field) => typeof value[field] === 'string')
}

function isBillingSettlementReconciliationItem(
  value: unknown
): value is BillingSettlementReconciliationItem {
  if (!isRecord(value)) return false

  return (
    hasFiniteNumberFields(value, ITEM_NUMBER_FIELDS) &&
    hasStringFields(value, ITEM_STRING_FIELDS) &&
    (value.status === 'pending' || value.status === 'manual') &&
    (value.source === 'wallet' || value.source === 'subscription') &&
    (value.user_blocking_override === null ||
      typeof value.user_blocking_override === 'boolean') &&
    typeof value.record_blocks_user === 'boolean' &&
    typeof value.blocks_user === 'boolean'
  )
}

export function isBillingSettlementReconciliationData(
  value: unknown
): value is BillingSettlementReconciliationData {
  if (!isRecord(value)) return false

  return (
    hasFiniteNumberFields(value, DATA_NUMBER_FIELDS) &&
    typeof value.block_user_by_default === 'boolean' &&
    typeof value.truncated === 'boolean' &&
    Array.isArray(value.items) &&
    value.items.every(isBillingSettlementReconciliationItem)
  )
}
