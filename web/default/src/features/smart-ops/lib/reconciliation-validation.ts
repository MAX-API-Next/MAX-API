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
    id: z.number(),
    revision: z.number(),
    operation_key: z.string(),
    status: z.enum(['pending', 'manual']),
    source: z.enum(['wallet', 'subscription']),
    user_id: z.number(),
    subscription_id: z.number(),
    token_id: z.number(),
    task_id: z.number(),
    funding_delta: z.number(),
    applied_funding_delta: z.number(),
    token_delta: z.number(),
    applied_token_delta: z.number(),
    attempts: z.number(),
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

export function isBillingSettlementReconciliationData(
  value: unknown
): value is BillingSettlementReconciliationData {
  return billingSettlementReconciliationDataSchema.safeParse(value).success
}
