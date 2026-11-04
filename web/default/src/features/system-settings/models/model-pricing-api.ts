// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { z } from 'zod'
import { isAxiosError } from 'axios'
import { api } from '@/lib/api'
import {
  guardedPricingChangeSchema,
  modelPricingCommitSchema,
  modelPricingEntrySchema,
  modelPricingQuoteRequestSchema,
  modelPricingQuoteSchema,
  modelPricingSnapshotSchema,
  normalizePricing,
  type GuardedPricingChange,
  type ModelPricingEntry,
  type ModelPricingValues,
  type ModelPricingQuoteRequest,
} from './model-pricing-types'

// API structure references New API 1424ffdd/web/src/features/model-pricing/api.ts;
// implemented against MAX's Root DTOs. No upstream calculator or mutation hooks.
export type PricingFailure =
  | 'rejected'
  | 'conflict'
  | 'unknown-result'
  | 'invalid-response'
  | 'request-failed'
export class ModelPricingError extends Error {
  constructor(
    public readonly kind: PricingFailure,
    message: string,
    public readonly status?: number
  ) {
    super(message)
  }
}
const endpoint = '/api/option/model_pricing'
const options = { skipBusinessError: true, skipErrorHandler: true }
function response<T>(schema: z.ZodType<T>, raw: unknown): T {
  const envelope = z
    .object({
      success: z.boolean(),
      message: z.string().optional(),
      data: z.unknown().optional(),
    })
    .parse(raw)
  if (!envelope.success)
    throw new ModelPricingError(
      'rejected',
      envelope.message || 'Request failed'
    )
  const result = schema.safeParse(envelope.data)
  if (!result.success)
    throw new ModelPricingError(
      'invalid-response',
      'Invalid model pricing response'
    )
  return result.data
}
async function request<T>(run: () => Promise<T>, write = false): Promise<T> {
  try {
    return await run()
  } catch (error) {
    if (error instanceof ModelPricingError) throw error
    if (isAxiosError(error)) {
      const status = error.response?.status
      const message: unknown = error.response?.data?.message
      const rejected =
        status !== undefined &&
        status >= 400 &&
        status < 500 &&
        status !== 408 &&
        status !== 499
      let kind: PricingFailure = 'request-failed'
      if (status === 409) kind = 'conflict'
      else if (rejected) kind = 'rejected'
      else if (write) kind = 'unknown-result'
      throw new ModelPricingError(
        kind,
        typeof message === 'string' ? message : error.message,
        status
      )
    }
    throw new ModelPricingError(
      write ? 'unknown-result' : 'invalid-response',
      'Invalid model pricing response'
    )
  }
}
export async function getModelPricing(
  names: string[] = [],
  signal?: AbortSignal
) {
  const params = new URLSearchParams()
  for (const name of names) params.append('model', name)
  return request(async () =>
    response(
      modelPricingSnapshotSchema,
      (
        await api.get(endpoint, {
          ...options,
          params,
          signal,
          disableDuplicate: true,
        })
      ).data
    )
  )
}
export async function saveModelPricing(changes: GuardedPricingChange[]) {
  const body = z
    .array(guardedPricingChangeSchema)
    .min(1)
    .max(200)
    .parse(changes)
  if (new Set(body.map((item) => item.model_name)).size !== body.length)
    throw new ModelPricingError('rejected', 'Duplicate pricing model')
  return request(
    async () =>
      response(
        modelPricingCommitSchema,
        (await api.patch(endpoint, { changes: body }, options)).data
      ),
    true
  )
}
export async function previewModelPricing(
  model: string,
  pricing: ModelPricingValues,
  signal?: AbortSignal
) {
  const body = { model_name: model, pricing: normalizePricing(pricing) }
  return request(async () => {
    const value = response(
      modelPricingEntrySchema,
      (await api.post(`${endpoint}/preview`, body, { ...options, signal })).data
    )
    if (value.model_name !== model || !value.draft_version)
      throw new ModelPricingError(
        'invalid-response',
        'Mismatched pricing preview'
      )
    return value
  })
}
export async function quoteModelPricing(
  input: ModelPricingQuoteRequest,
  signal?: AbortSignal
) {
  const body = modelPricingQuoteRequestSchema.parse(input)
  return request(async () =>
    response(
      modelPricingQuoteSchema,
      (await api.post(`${endpoint}/quote`, body, { ...options, signal })).data
    )
  )
}
export const modelPricingTransport = {
  async read(model: string, signal?: AbortSignal): Promise<ModelPricingEntry> {
    const snapshot = await getModelPricing([model], signal)
    if (
      snapshot.entries.length !== 1 ||
      snapshot.entries[0].model_name !== model
    )
      throw new ModelPricingError('invalid-response', 'Missing pricing model')
    return snapshot.entries[0]
  },
  save: (change: GuardedPricingChange) => saveModelPricing([change]),
  saveMany: saveModelPricing,
  preview: previewModelPricing,
  quote: quoteModelPricing,
}
export type ModelPricingTransport = Omit<
  typeof modelPricingTransport,
  'saveMany'
> & {
  saveMany?: typeof saveModelPricing
}
