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
import type {
  TaskRateCardPricing,
  TaskRateCardPricingComponent,
} from '../types'
import {
  getTaskRateCardComponentLabelKey,
  hasStructuredTaskRateCard,
} from './task-rate-card'

describe('task rate card helpers', () => {
  test('recognizes only cards with a billing type and components', () => {
    const baseCard: TaskRateCardPricing = {
      billing_type: 'minimax',
      min_unit_price: 0,
      max_unit_price: 0,
      rows: [],
    }

    assert.equal(hasStructuredTaskRateCard(null), false)
    assert.equal(hasStructuredTaskRateCard(undefined), false)
    assert.equal(hasStructuredTaskRateCard(baseCard), false)
    assert.equal(
      hasStructuredTaskRateCard({ ...baseCard, billing_type: undefined }),
      false
    )
    assert.equal(
      hasStructuredTaskRateCard({ ...baseCard, components: [] }),
      false
    )
    assert.equal(
      hasStructuredTaskRateCard({
        ...baseCard,
        components: [{ key: 'input_image', unit: 'image', unit_price: '0.04' }],
      }),
      true
    )
  })

  test('maps every supported component key and variant to its label key', () => {
    const cases: Array<[TaskRateCardPricingComponent, string]> = [
      [
        {
          key: 'output_video',
          variant: '768P',
          unit: 'second',
          unit_price: '0.08',
        },
        'Output video / 768P',
      ],
      [
        {
          key: 'output_video',
          variant: '2K',
          unit: 'second',
          unit_price: '0.13',
        },
        'Output video / 2K',
      ],
      [
        {
          key: 'input_video',
          variant: '768P',
          unit: 'second',
          unit_price: '0.08',
        },
        'Input video / 768P',
      ],
      [
        {
          key: 'input_video',
          variant: '2K',
          unit: 'second',
          unit_price: '0.13',
        },
        'Input video / 2K',
      ],
      [
        { key: 'input_image', unit: 'image', unit_price: '0.04' },
        'Extra input image price',
      ],
      [
        { key: 'input_audio', unit: 'second', unit_price: '0.01' },
        'Input audio price',
      ],
    ]

    for (const [component, expectedLabel] of cases) {
      assert.equal(getTaskRateCardComponentLabelKey(component), expectedLabel)
    }
  })

  test('returns null for unsupported key and variant combinations', () => {
    const unknownComponents: TaskRateCardPricingComponent[] = [
      {
        key: 'output_video',
        variant: '4K',
        unit: 'second',
        unit_price: '0.2',
      },
      {
        key: 'input_video',
        unit: 'second',
        unit_price: '0.2',
      },
      { key: 'custom', variant: 'custom', unit: 'second', unit_price: '0.2' },
    ]

    for (const component of unknownComponents) {
      assert.equal(getTaskRateCardComponentLabelKey(component), null)
    }
  })
})
