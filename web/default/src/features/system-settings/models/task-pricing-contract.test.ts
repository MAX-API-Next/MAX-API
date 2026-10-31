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
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'
import {
  modelPricingChangeSchema,
  taskFactContractSchema,
  taskPricingMapsSchema,
  taskUsageFieldSchema,
  type TaskUsageField,
} from './task-pricing-contract'

function enumValues(field: TaskUsageField) {
  assert.ok('enum' in field)
  return field.enum
}

function unit(field: TaskUsageField) {
  assert.equal(field.type, 'number')
  assert.ok('unit' in field)
  return field.unit
}

// Shared with Go. Prices are synthetic, not a browser calculator/production API.
const facts = z.record(
  z.string(),
  z.union([z.number().finite(), z.boolean(), z.string()])
)
const fixture = z
  .object({
    fixture_version: z.literal(1),
    profiles: z.array(
      z.object({
        id: z.string(),
        models: z.array(z.string()),
        plugin_key: z.string(),
        contract: taskFactContractSchema,
        submit: facts,
        actual: facts,
        expected: z.object({
          target: z.number(),
          reserved: z.number(),
          refund: z.number(),
        }),
      })
    ),
    patch_cases: z.array(modelPricingChangeSchema),
  })
  .parse(
    JSON.parse(
      readFileSync(
        new URL(
          '../../../../../../pkg/taskpricing/testdata/compat-v1.json',
          import.meta.url
        ),
        'utf8'
      )
    )
  )

describe('task pricing shared contract boundary', () => {
  test('three families retain distinct units and per-model schema', () => {
    assert.equal(fixture.profiles.length, 4)
    const [h3, kling, seed15, seed2] = fixture.profiles
    assert.deepEqual(enumValues(h3.contract.usage_schema.resolution), [
      '768P',
      '2K',
    ])
    assert.equal(unit(kling.contract.usage_schema.units), 'credit')
    assert.deepEqual(kling.models, [
      'kling-v1',
      'kling-v1-6',
      'kling-v2-master',
    ])
    assert.equal(kling.models.includes('kling-v3'), false)
    assert.equal(seed15.contract.usage_schema.generate_audio.type, 'boolean')
    assert.equal(
      Object.hasOwn(seed15.contract.usage_schema, 'video_input'),
      false
    )
    assert.equal(
      Object.hasOwn(seed2.contract.usage_schema, 'generate_audio'),
      false
    )
    assert.deepEqual(enumValues(seed2.contract.usage_schema.video_input), [
      'none',
      'video',
    ])
    assert.equal(unit(seed2.contract.usage_schema.tokens), 'token')
  })

  test('wire preserves false, explicit zero expression and omitted reset', () => {
    const [explicit, absent] = fixture.patch_cases
    assert.equal(explicit.reset, false)
    assert.equal(Object.hasOwn(absent, 'reset'), false)
    assert.deepEqual(explicit.pricing['billing_setting.plugin_billing_expr'], {
      kling: '0',
    })
    assert.equal(explicit.pricing.ModelPrice, 0)
    assert.equal(Object.hasOwn(absent.pricing, 'ModelPrice'), false)
    assert.deepEqual(
      modelPricingChangeSchema.parse(JSON.parse(JSON.stringify(explicit))),
      explicit
    )
    assert.equal(fixture.profiles[2].submit.generate_audio, false)
    const prices = taskPricingMapsSchema.parse({
      billing_mode: { 'kling-v1': 'tiered_expr' },
      billing_expr: { 'kling-v1': '1' },
      plugin_billing_expr: { 'kling::kling-v1': '0' },
    })
    assert.equal(prices.plugin_billing_expr['kling::kling-v1'], '0')
    assert.equal(
      Object.hasOwn(prices.plugin_billing_expr, 'other::kling-v1'),
      false
    )
  })

  test('malformed fields and unknown PATCH fields are rejected', () => {
    for (const field of [
      { type: 'number', unit: 'millisecond' },
      { type: 'boolean', unit: 'count' },
      { enum: [] },
      { enum: ['a', 'a'] },
      { type: 'number', unit: 'credit', enum: ['a'] },
    ])
      assert.equal(taskUsageFieldSchema.safeParse(field).success, false)
    assert.equal(
      modelPricingChangeSchema.safeParse({
        ...fixture.patch_cases[0],
        accidental_price: 1,
      }).success,
      false
    )
    assert.equal(
      modelPricingChangeSchema.safeParse({
        ...fixture.patch_cases[0],
        reset: null,
      }).success,
      false
    )
  })

  test('fixed expected amounts use independent integer arithmetic', () => {
    // cents -> quota at QPU=1000. Never evaluates the candidate expressions.
    const expected = [
      { target: (40 + 30 + 2) * 10, reserved: 1200, refund: 480 },
      { target: 6 * 10, reserved: 100, refund: 40 },
      { target: (2000000 / 1000000) * 2 * 1000, reserved: 8000, refund: 4000 },
      {
        target: (2000000 / 1000000) * 2 * 1000 * 2,
        reserved: 12000,
        refund: 4000,
      },
    ]
    fixture.profiles.forEach((profile, i) =>
      assert.deepEqual(profile.expected, expected[i])
    )
  })
})
