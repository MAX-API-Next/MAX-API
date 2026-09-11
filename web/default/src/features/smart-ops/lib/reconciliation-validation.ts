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
import { z } from 'zod'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
} from '../types'

const billingSettlementReconciliationItemSchema: z.ZodType<BillingSettlementReconciliationItem> =
  z.object({
    id: z.number().int().safe().positive(),
    revision: z.number().int().safe().positive(),
    operation_key: z.string(),
    status: z.enum(['pending', 'manual']),
    source: z.enum(['wallet', 'subscription']),
    user_id: z.number().int().safe(),
    subscription_id: z.number().int().safe(),
    token_id: z.number().int().safe(),
    task_id: z.number().int().safe(),
    task_quota: z.number().int().safe().nonnegative(),
    task_quota_target: z.number().int().safe().nonnegative(),
    requires_manual_completion: z.boolean(),
    zero_quota_eligible: z.boolean().optional(),
    funding_delta: z.number().int().safe(),
    applied_funding_delta: z.number().int().safe(),
    token_delta: z.number().int().safe(),
    applied_token_delta: z.number().int().safe(),
    attempts: z.number().int().safe().nonnegative(),
    last_error: z.string(),
    next_attempt: z.number(),
    created_at: z.number(),
    updated_at: z.number(),
    reconciliation_reviewed_at: z.number(),
    reconciliation_reviewed_by: z.number(),
    reconciliation_review_note: z.string(),
    user_blocking_override: z.boolean().nullable(),
    record_blocks_user: z.boolean(),
    blocks_user: z.boolean(),
  })

const billingSettlementReconciliationDataSchema: z.ZodType<BillingSettlementReconciliationData> =
  z.object({
    total_count: z.number(),
    pending_count: z.number(),
    manual_count: z.number(),
    open_alert_count: z.number(),
    blocking_record_count: z.number(),
    blocked_user_count: z.number(),
    block_user_by_default: z.boolean(),
    oldest_created_at: z.number(),
    truncated: z.boolean(),
    generated_at: z.number(),
    items: z.array(billingSettlementReconciliationItemSchema),
  })

export type BillingSettlementReviewSelection =
  | 'empty'
  | 'ordinary'
  | 'zero_quota'
  | 'mixed'
  | 'exact_quota_required'

export function classifyBillingSettlementReviewSelection(
  items: BillingSettlementReconciliationItem[]
): BillingSettlementReviewSelection {
  const exactQuotaOnly = items.some(
    (item) =>
      item.requires_manual_completion && item.zero_quota_eligible !== true
  )
  if (exactQuotaOnly) return 'exact_quota_required'

  const hasZeroQuotaTasks = items.some(
    (item) => item.zero_quota_eligible === true
  )
  const hasOrdinaryAlerts = items.some(
    (item) => !item.requires_manual_completion
  )
  if (hasZeroQuotaTasks && hasOrdinaryAlerts) return 'mixed'
  if (hasZeroQuotaTasks) return 'zero_quota'
  if (hasOrdinaryAlerts) return 'ordinary'
  return 'empty'
}

export function isBillingSettlementReconciliationData(
  value: unknown
): value is BillingSettlementReconciliationData {
  return billingSettlementReconciliationDataSchema.safeParse(value).success
}
