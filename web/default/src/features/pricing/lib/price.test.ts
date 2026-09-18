/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import { parseTiersFromExpr } from './billing-expr'
import { getDynamicPriceEntries } from './dynamic-price'
import {
  formatFixedPrice,
  formatGroupPrice,
  formatTaskRateCardRange,
} from './price'

function modelWithRateCard(
  min: number | undefined,
  max: number | undefined
): PricingModel {
  return {
    id: 1,
    model_name: 'example-task',
    quota_type: 1,
    model_ratio: 0,
    completion_ratio: 0,
    enable_groups: [],
    task_rate_card: {
      unit: 'second',
      min_unit_price: min as number,
      max_unit_price: max as number,
      rows: [],
    },
  }
}

describe('task rate-card display prices', () => {
  test('dynamic summaries keep configured zero prices and omit absent prices', () => {
    const [tier] = parseTiersFromExpr('tier("free", p * 0 + c * 2 + cr * 0)')
    const entries = getDynamicPriceEntries(tier, { tokenUnit: 'M' })
    assert.deepEqual(
      entries.map(({ key, value }) => [key, value]),
      [
        ['p', 0],
        ['c', 2],
        ['cr', 0],
      ]
    )
    assert.match(entries[0].formatted, /0/)
    assert.match(entries[2].formatted, /0/)
  })

  test('keeps an explicit zero price visible', () => {
    assert.match(formatTaskRateCardRange(modelWithRateCard(0, 0)), /0/)
  })

  test('does not turn an omitted price into a misleading zero', () => {
    assert.equal(
      formatTaskRateCardRange(modelWithRateCard(undefined, undefined)),
      '-'
    )
  })

  test('keeps explicit zero group ratios for token and per-request prices', () => {
    const model = { ...modelWithRateCard(1, 1), model_price: 4 }
    const free = { free: 0 }
    const fixed = formatFixedPrice(model, 'free', false, 1, 1, free)
    const token = formatGroupPrice(
      { ...model, quota_type: 0, model_ratio: 3 },
      'free',
      'input',
      'M',
      false,
      1,
      1,
      free
    )
    assert.equal(
      fixed,
      formatFixedPrice({ ...model, model_price: 0 }, 'base', false, 1, 1, {})
    )
    assert.equal(
      token,
      formatGroupPrice(
        { ...model, quota_type: 0, model_ratio: 0 },
        'base',
        'input',
        'M',
        false,
        1,
        1,
        {}
      )
    )
  })
})
