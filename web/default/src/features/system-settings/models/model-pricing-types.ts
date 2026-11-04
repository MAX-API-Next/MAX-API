// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { z } from 'zod'
import {
  taskFactContractSchema,
  taskUsageFieldSchema,
} from './task-pricing-contract'

// Wire reference: New API 1424ffdd, features/model-pricing/api.ts.
// MAX's actual Go DTOs additionally require contract/binding guards and distinguish
// configuration preview, total-currency quote, commit and local cache publication.
const token = z.string().min(1)
const amount = z.number().finite().nonnegative()
const quota = amount.int().max(2_147_483_647)
const instant = z.string().datetime({ offset: true })
export const modelPricingNumericKeys = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
] as const
export const modelPricingValuesSchema = z.strictObject({
  ModelPrice: amount.optional(),
  ModelRatio: amount.optional(),
  CompletionRatio: amount.optional(),
  CacheRatio: amount.optional(),
  CreateCacheRatio: amount.optional(),
  ImageRatio: amount.optional(),
  AudioRatio: amount.optional(),
  AudioCompletionRatio: amount.optional(),
  // Retain diagnosed legacy values on reads so an administrator can repair them.
  'billing_setting.billing_mode': z.string().optional(),
  'billing_setting.billing_expr': z.string().optional(),
  'billing_setting.plugin_billing_expr': z.record(token, z.string()).optional(),
})
const pricingWriteSchema = modelPricingValuesSchema.extend({
  'billing_setting.billing_mode': z.enum(['ratio', 'tiered_expr']).optional(),
})
const contextSchema = z.strictObject({
  id: token,
  channel_id: z.number().int().positive().optional(),
  groups: z.array(z.string()).optional(),
  plugin_key: z.string().optional(),
  mapped_model: z.string(),
  usage_schema: z.record(token, taskUsageFieldSchema).optional(),
  contract: taskFactContractSchema.optional(),
  profile: z.string().optional(),
  configured: z.string().optional(),
  effective: z.string().optional(),
  compatible: z.boolean(),
  schema_compatible: z.boolean(),
  available: z.boolean(),
  execution_ready: z.boolean(),
  reason: z.string().optional(),
})
export const modelPricingEntrySchema = z.strictObject({
  model_name: token,
  version: token,
  draft_version: token.optional(),
  configuration_error: z.string().optional(),
  configured: modelPricingValuesSchema,
  effective: modelPricingValuesSchema,
  contract_digest: token,
  binding_version: token,
  contexts: z.array(contextSchema),
  plugin_variants: z.array(contextSchema),
})
export const modelPricingSnapshotSchema = z.strictObject({
  entries: z.array(modelPricingEntrySchema),
  options: z.record(z.string(), z.string()),
  empty_version: token,
  authority: token,
})
export const modelPricingCommitSchema = z.strictObject({
  committed: z.literal(true),
  cache_published: z.boolean(),
  entries: z.array(modelPricingEntrySchema),
  warning: z.string().optional(),
})
export const modelPricingGuards = {
  expected_version: token,
  expected_contract_digest: token,
  expected_binding_version: token,
}
export const guardedPricingChangeSchema = z.strictObject({
  model_name: token,
  ...modelPricingGuards,
  pricing: pricingWriteSchema,
  reset: z.boolean().optional(),
})
const facts = z.record(token, z.union([amount, z.boolean(), z.string()]))
export const modelPricingSampleSchema = z.strictObject({
  group: token,
  submit: facts,
  actual: facts.optional(),
  pricing_at: instant.optional(),
  request_body: z.record(z.string(), z.unknown()).optional(),
  request_headers: z.record(z.string(), z.string()).optional(),
})
export const modelPricingQuoteRequestSchema = modelPricingSampleSchema.extend({
  model_name: token,
  context_id: token,
  ...modelPricingGuards,
  pricing: pricingWriteSchema.optional(),
})
const taskQuoteSchema = z.strictObject({
  profile: z.literal('new-api-task@1'),
  amount,
  quota_before_group: amount,
  quota_after_group: quota,
  matched_tier: z.string(),
  pricing_at: instant,
})
export const modelPricingQuoteSchema = z.strictObject({
  scope: token,
  admission_ready: z.literal(false),
  price_version: token,
  contract_digest: token,
  binding_version: token,
  terms_version: token,
  group: token,
  group_ratio: amount,
  quota_per_unit: amount.positive(),
  reservation_floor: quota,
  currency: z.literal('USD'),
  preview: z.strictObject({
    profile: z.literal('new-api-task@1'),
    source: z.strictObject({
      driver: z.enum(['go_adapter', 'task_plugin']),
      plugin_key: z.string().optional(),
    }),
    selection: z.strictObject({
      expression: z.string(),
      model: token,
      plugin_key: z.string().optional(),
    }),
    contract_digest: token,
    estimate: taskQuoteSchema,
    reserved_quota: quota,
    final: taskQuoteSchema.nullable(),
    final_minus_reserved: z.number().int().nullable(),
  }),
})
export type ModelPricingValues = z.infer<typeof modelPricingValuesSchema>
export type ModelPricingEntry = z.infer<typeof modelPricingEntrySchema>
export type ModelPricingCommit = z.infer<typeof modelPricingCommitSchema>
export type GuardedPricingChange = z.infer<typeof guardedPricingChangeSchema>
export type ModelPricingSample = z.infer<typeof modelPricingSampleSchema>
export type ModelPricingQuoteRequest = z.infer<
  typeof modelPricingQuoteRequestSchema
>
export type ModelPricingQuote = z.infer<typeof modelPricingQuoteSchema>

export function normalizePricing(
  value: ModelPricingValues
): ModelPricingValues {
  const parsed = modelPricingValuesSchema.parse(value)
  // The server projects no overrides as an absent field, not a nested empty map.
  const variants = parsed['billing_setting.plugin_billing_expr']
  if (variants && Object.keys(variants).length === 0)
    delete parsed['billing_setting.plugin_billing_expr']
  return Object.fromEntries(
    Object.entries(parsed).filter(([, v]) => v !== undefined)
  )
}
export function pricingEqual(
  a: ModelPricingValues,
  b: ModelPricingValues
): boolean {
  const signature = (value: ModelPricingValues) =>
    JSON.stringify(
      Object.entries(normalizePricing(value))
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([key, v]) => [
          key,
          typeof v === 'object'
            ? Object.entries(v).sort(([a], [b]) => a.localeCompare(b))
            : v,
        ])
    )
  return signature(a) === signature(b)
}
export function samePricingGuards(
  a: ModelPricingEntry,
  b: ModelPricingEntry
): boolean {
  return (
    a.model_name === b.model_name &&
    a.version === b.version &&
    a.contract_digest === b.contract_digest &&
    a.binding_version === b.binding_version
  )
}
export function pricingChange(
  entry: ModelPricingEntry,
  pricing: ModelPricingValues
): GuardedPricingChange {
  return guardedPricingChangeSchema.parse({
    model_name: entry.model_name,
    expected_version: entry.version,
    expected_contract_digest: entry.contract_digest,
    expected_binding_version: entry.binding_version,
    pricing: normalizePricing(pricing),
  })
}
