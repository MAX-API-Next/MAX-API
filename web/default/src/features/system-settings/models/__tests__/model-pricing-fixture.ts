// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import type {
  ModelPricingCommit,
  ModelPricingEntry,
  ModelPricingQuote,
  ModelPricingQuoteRequest,
  ModelPricingValues,
} from '../model-pricing-types'

export function entry(
  model = 'kling-v1',
  version = 'price-v1',
  pricing?: ModelPricingValues
): ModelPricingEntry {
  const configured = pricing ?? {
    ModelPrice: 7,
    CacheRatio: 1.5,
    'billing_setting.billing_mode': 'tiered_expr',
    'billing_setting.billing_expr': 'u("units") * 0.1',
    'billing_setting.plugin_billing_expr': { kling: '0' },
  }
  return {
    model_name: model,
    version,
    contract_digest: 'contracts-v1',
    binding_version: 'binding-v1',
    configured: structuredClone(configured),
    effective: structuredClone(configured),
    contexts: [
      {
        id: 'channel:1',
        channel_id: 1,
        groups: ['default'],
        mapped_model: model,
        usage_schema: { units: { type: 'number', unit: 'credit' } },
        contract: {
          version: 1,
          usage_schema: { units: { type: 'number', unit: 'credit' } },
          field_policy: { units: { phase: 'actual', required: true } },
        },
        compatible: true,
        schema_compatible: true,
        available: true,
        execution_ready: false,
      },
    ],
    plugin_variants: [],
  }
}
export function receipt(
  value: ModelPricingEntry,
  published = true
): ModelPricingCommit {
  return {
    committed: true,
    cache_published: published,
    entries: [structuredClone(value)],
    ...(published
      ? {}
      : { warning: 'Committed; local cache publication failed' }),
  }
}
export function quoted(input: ModelPricingQuoteRequest): ModelPricingQuote {
  // Fixed wire fixture: 0.6 credit * USD0.1 * 1000 = 60; floor500 on reserve.
  // No production expression/quote function is used to generate expected values.
  const at = input.pricing_at ?? '2026-10-31T00:00:00Z'
  const identity = {
    profile: 'new-api-task@1' as const,
    matched_tier: '',
    pricing_at: at,
  }
  const finalAmount = input.actual?.units === 0 ? 0 : 0.06
  const finalQuota = input.actual?.units === 0 ? 0 : 60
  const estimateAmount = input.submit.units === 2 ? 0.2 : 0.1
  const estimateQuota = input.submit.units === 2 ? 200 : 100
  return {
    scope: 'configuration_sample',
    admission_ready: false,
    price_version: input.expected_version,
    contract_digest: input.expected_contract_digest,
    binding_version: input.expected_binding_version,
    terms_version: 'terms-v1',
    group: input.group,
    group_ratio: 1,
    quota_per_unit: 1000,
    reservation_floor: 500,
    currency: 'USD',
    preview: {
      profile: 'new-api-task@1',
      source: { driver: 'go_adapter' },
      selection: { expression: 'u("units") * 0.1', model: input.model_name },
      contract_digest: 'single-contract-v1',
      estimate: {
        ...identity,
        amount: estimateAmount,
        quota_before_group: estimateQuota,
        quota_after_group: estimateQuota,
      },
      reserved_quota: 500,
      final: {
        ...identity,
        amount: finalAmount,
        quota_before_group: finalQuota,
        quota_after_group: finalQuota,
      },
      final_minus_reserved: input.actual?.units === 0 ? -500 : -440,
    },
  }
}
export function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
