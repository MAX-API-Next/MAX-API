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
import type { ModelSettings } from '@/features/system-settings/types'
import {
  parseNonNegativeFiniteNumber,
  readPricingConfig,
  shouldReplacePricingConfig,
} from './model-pricing-config'

function createPricingSettings(
  overrides: Partial<ModelSettings>
): ModelSettings {
  return {
    ModelPrice: '{}',
    ModelRatio: '{}',
    CacheRatio: '{}',
    CompletionRatio: '{}',
    ImageRatio: '{}',
    AudioRatio: '{}',
    AudioCompletionRatio: '{}',
    ...overrides,
  } as ModelSettings
}

describe('model drawer pricing', () => {
  test('strictly parses only finite non-negative pricing values', () => {
    assert.equal(parseNonNegativeFiniteNumber('0'), 0)
    assert.equal(parseNonNegativeFiniteNumber('1.25e-3'), 0.00125)
    assert.equal(parseNonNegativeFiniteNumber(''), undefined)
    assert.equal(parseNonNegativeFiniteNumber('-1'), undefined)
    assert.equal(parseNonNegativeFiniteNumber('Infinity'), undefined)
    assert.equal(parseNonNegativeFiniteNumber('1abc'), undefined)
  })

  test('loads existing pricing for a same-name create flow', () => {
    const pricing = readPricingConfig(
      createPricingSettings({
        ModelRatio: '{"existing-model":1.5}',
        CompletionRatio: '{"existing-model":2}',
        CacheRatio: '{"existing-model":0}',
      }),
      'existing-model'
    )

    assert.equal(pricing.mode, 'per-token')
    assert.equal(pricing.fields.ratio, '1.5')
    assert.equal(pricing.promptPrice, '3')
    assert.equal(pricing.completionPrice, '6')
    assert.equal(pricing.fields.cacheRatio, '0')
    assert.equal(pricing.advancedOpen, true)
  })

  test('keeps fixed price authoritative over stale ratio entries', () => {
    const pricing = readPricingConfig(
      createPricingSettings({
        ModelPrice: '{"priced-model":0.25}',
        ModelRatio: '{"priced-model":9}',
      }),
      'priced-model'
    )

    assert.equal(pricing.mode, 'per-request')
    assert.equal(pricing.fields.price, '0.25')
    assert.equal(pricing.fields.ratio, '')
  })

  test('only replaces pricing the form loaded or explicitly supplied', () => {
    assert.equal(shouldReplacePricingConfig('loaded', 'loaded', false), true)
    assert.equal(shouldReplacePricingConfig('renamed', 'loaded', true), true)
    assert.equal(shouldReplacePricingConfig('foreign', 'loaded', false), false)
  })
})
