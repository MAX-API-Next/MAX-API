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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import type { PricingModel } from '../types'
import { ModelDetailsContent } from './model-details'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

describe('ModelDetailsContent structured task pricing', () => {
  function createModel(inputVideoMaxQuantity?: number): PricingModel {
    return {
      id: 1,
      model_name: 'MiniMax-H3',
      quota_type: 1,
      model_ratio: 0,
      completion_ratio: 0,
      model_price: 0,
      enable_groups: ['default'],
      task_rate_card: {
        rule_key: 'minimax/minimax-h3',
        vendor: 'minimax',
        billing_type: 'minimax',
        currency: 'USD',
        unit: 'second',
        quantity_field: 'output_duration',
        strict: true,
        min_unit_price: 0.08,
        max_unit_price: 0.13,
        rows: [],
        components: [
          {
            key: 'output_video',
            variant: '768P',
            unit: 'second',
            unit_price: '0.08',
            min_quantity: 4,
            max_quantity: 15,
          },
          {
            key: 'output_video',
            variant: '2K',
            unit: 'second',
            unit_price: '0.13',
            min_quantity: 4,
            max_quantity: 15,
          },
          {
            key: 'input_video',
            variant: '768P',
            unit: 'second',
            unit_price: '0.08',
            max_quantity: inputVideoMaxQuantity,
          },
          {
            key: 'input_video',
            variant: '2K',
            unit: 'second',
            unit_price: '0.13',
            max_quantity: inputVideoMaxQuantity,
          },
          {
            key: 'input_image',
            unit: 'image',
            unit_price: '0.04',
            free_quantity: 5,
            max_quantity: 9,
          },
          {
            key: 'input_audio',
            unit: 'second',
            unit_price: '0',
            max_quantity: 15,
          },
        ],
      },
    } as PricingModel
  }

  async function renderModel(model: PricingModel) {
    const queryClient = new QueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ModelDetailsContent
          model={model}
          groupRatio={{ default: 1 }}
          usableGroup={{ default: { desc: 'Default', ratio: 1 } }}
          endpointMap={{}}
          autoGroups={[]}
          priceRate={1}
          usdExchangeRate={1}
          tokenUnit='M'
        />
      </QueryClientProvider>
    )
    return { queryClient, view }
  }

  test('shows every MiniMax billing component without falling back to model price', async () => {
    const { queryClient, view } = await renderModel(createModel(15))

    try {
      const content = view.container.textContent || ''
      assert.match(content, /Output video \/ 768P/)
      assert.match(content, /Output video \/ 2K/)
      assert.match(content, /Input video \/ 768P/)
      assert.match(content, /Input video \/ 2K/)
      assert.match(content, /Extra input image price/)
      assert.match(content, /Input audio price/)
      assert.match(content, /Free input images/)
      assert.doesNotMatch(content, /Per request/)
    } finally {
      await view.unmount()
      queryClient.clear()
    }
  })

  test('does not append seconds when input video cap is absent', async () => {
    const { queryClient, view } = await renderModel(createModel())

    try {
      const content = view.container.textContent || ''
      assert.match(content, /Input video reservation cap-/)
      assert.doesNotMatch(content, /- seconds/)
    } finally {
      await view.unmount()
      queryClient.clear()
    }
  })
})
