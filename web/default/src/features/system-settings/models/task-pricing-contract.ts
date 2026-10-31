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
import { z } from 'zod'

// New API a2defd2a: model/model_pricing_config.go and pkg/jsplugin/registry.go.
// Boundary types only; no page, independent price store or quote engine.
const localizedText = z.record(z.string(), z.string())
const displayFields = {
  unitLabel: localizedText.optional(),
  description: localizedText.optional(),
  enumLabels: z.record(z.string(), localizedText).optional(),
}

export const taskUsageFieldSchema = z.union([
  z.strictObject({
    type: z.literal('number'),
    unit: z.enum(['second', 'count', 'token', 'credit']),
    ...displayFields,
  }),
  z.strictObject({ type: z.literal('boolean'), ...displayFields }),
  z.strictObject({
    type: z.literal('enum').optional(),
    enum: z
      .array(z.string().refine((v) => v.trim().length > 0))
      .min(1)
      .refine((v) => new Set(v).size === v.length),
    ...displayFields,
  }),
])

const factPolicySchema = z.strictObject({
  phase: z.enum(['request', 'actual']),
  required: z.boolean(),
  min: z.number().finite().nonnegative().optional(),
  max: z.number().finite().nonnegative().optional(),
  integer: z.boolean().optional(),
})

// This parses the transport shape. Host phase/facts validation and semantic
// digest remain authoritative in Go; a successful parse is not a valid quote.
export const taskFactContractSchema = z.strictObject({
  version: z.literal(1),
  usage_schema: z.record(z.string().min(1), taskUsageFieldSchema),
  field_policy: z.record(z.string().min(1), factPolicySchema),
})

export const taskPricingMapsSchema = z.strictObject({
  billing_mode: z.record(z.string(), z.string()),
  billing_expr: z.record(z.string(), z.string()),
  plugin_billing_expr: z.record(z.string(), z.string()),
})

export const modelPricingChangeSchema = z.strictObject({
  model_name: z.string().min(1),
  expected_version: z.string().min(1),
  pricing: z.record(z.string(), z.unknown()),
  reset: z.boolean().optional(),
  expected_contract_digest: z.string().min(1).optional(),
  expected_binding_version: z.string().min(1).optional(),
})

export type TaskUsageField = z.infer<typeof taskUsageFieldSchema>
export type TaskFactContract = z.infer<typeof taskFactContractSchema>
export type TaskPricingMaps = z.infer<typeof taskPricingMapsSchema>
export type ModelPricingChange = z.infer<typeof modelPricingChangeSchema>
export type TaskPricingFacts = Record<string, number | boolean | string>
export type TaskPricingProfile =
  | 'legacy-rate-card@1'
  | 'legacy-minimax@1'
  | 'legacy-token@1'
  | 'new-api-task@1'
