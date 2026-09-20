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
import { within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { after, before, describe, test } from 'node:test'
import { useSystemConfigStore } from '@/stores/system-config-store'
import type { PricingModel } from '../types'
import { DynamicPricingBreakdown } from './dynamic-pricing-breakdown'
import { ModelDetailsContent } from './model-details'
import { ModelTierPricing } from './model-tier-pricing'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

describe('Model square expression pricing', () => {
  const displayOptions = {
    tokenUnit: 'M' as const,
    priceRate: 1,
    usdExchangeRate: 1,
    showRechargePrice: false,
  }
  const expression =
    '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 && weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") < 6) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18 && weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") < 6) ? tier("peak", p * 2 + c * 8 + cr * 0.04) : tier("off", p * 1 + c * 4 + cr * 0)'

  for (const locale of ['en', 'zh', 'fr', 'ja', 'ru', 'vi']) {
    test(`provides model square display translations in ${locale}`, () => {
      const resource = JSON.parse(
        readFileSync(
          new URL(`../../../i18n/locales/${locale}.json`, import.meta.url),
          'utf8'
        )
      ).translation
      for (const key of [
        'Tier applicability',
        'Prices shown per {{unit}} tokens',
        'Tiers are checked in order; the first match applies.',
        'When no earlier tier matches',
        'All conditions in a group must match.',
        'These conditions cannot match together.',
        'OR',
        'Base tier prices, before group and request multipliers. A dash means no separate coefficient in this tier.',
      ]) {
        assert.ok(resource[key]?.trim(), `${locale}: ${key}`)
        assert.doesNotMatch(resource[key], /\?{2,}/, `${locale}: ${key}`)
        if (locale !== 'en') assert.notEqual(resource[key], key)
      }
    })
  }

  test('localizes the complete token-unit sentence for both display units', async () => {
    const resource = JSON.parse(
      readFileSync(
        new URL('../../../i18n/locales/fr.json', import.meta.url),
        'utf8'
      )
    ).translation
    testEnv.i18n.addResourceBundle('fr', 'translation', resource)
    await testEnv.i18n.changeLanguage('fr')
    try {
      for (const tokenUnit of ['K', 'M'] as const) {
        const view = await testEnv.render(
          <ModelTierPricing
            {...displayOptions}
            tokenUnit={tokenUnit}
            billingExpr='tier("base", p * 2 + c * 8)'
          />
        )
        try {
          assert.ok(
            within(view.container).getByText(
              `Prix affichés pour 1${tokenUnit} jetons`
            )
          )
        } finally {
          await view.unmount()
        }
      }
    } finally {
      await testEnv.i18n.changeLanguage('en')
    }
  })

  test('preserves zero, absent and tiny coefficients and follows model square unit selection', async () => {
    const view = await testEnv.render(
      <ModelTierPricing
        {...displayOptions}
        tokenUnit='K'
        billingExpr='len < 100 ? tier("free", p * 0 + c * 0 + cr * 0) : tier("base", p * 2 + c * 4 + cc * 0.01)'
      />
    )
    try {
      const content = within(view.container)
      const table = within(content.getByRole('table'))
      assert.deepEqual(
        table.getAllByRole('columnheader').map((node) => node.textContent),
        ['Tier', 'Input', 'Output', 'Cache Read', 'Cache Write']
      )
      assert.deepEqual(
        within(table.getByRole('row', { name: /^free / }))
          .getAllByRole('cell')
          .map((node) => node.textContent),
        ['free', '$0', '$0', '$0', '—']
      )
      assert.deepEqual(
        within(table.getByRole('row', { name: /^base / }))
          .getAllByRole('cell')
          .map((node) => node.textContent),
        ['base', '$0.002', '$0.004', '—', '$0.00001']
      )
      assert.match(view.container.textContent || '', /1K tokens/)
    } finally {
      await view.unmount()
    }
  })

  test('keeps unsupported formulas and request multipliers available without partial pricing claims', async () => {
    const source =
      '(tier("custom", max(p, 1) * 3 + c * 15)) * (param("fast") == true ? 2 : 1)'
    const view = await testEnv.render(
      <ModelTierPricing {...displayOptions} billingExpr={source} />
    )
    try {
      assert.equal(within(view.container).queryByRole('table'), null)
      assert.ok(
        within(view.container).getByText('Unable to parse structured pricing')
      )
      assert.ok(view.container.querySelector('details[open]'))
      assert.equal(
        view.container.querySelector('details code')?.textContent,
        source
      )
      assert.ok(within(view.container).getByText('Conditional multipliers'))
    } finally {
      await view.unmount()
    }
  })

  test('uses model square currency and recharge-price settings consistently', async () => {
    const previous = useSystemConfigStore.getState().config
    useSystemConfigStore.setState({
      config: {
        ...previous,
        currency: {
          ...previous.currency,
          quotaDisplayType: 'CNY',
          usdExchangeRate: 7,
        },
      },
    })
    try {
      for (const [showRechargePrice, expected] of [
        [false, '¥14'],
        [true, '¥7'],
      ] as const) {
        const view = await testEnv.render(
          <ModelTierPricing
            {...displayOptions}
            priceRate={3.5}
            usdExchangeRate={7}
            showRechargePrice={showRechargePrice}
            billingExpr='tier("base", p * 2)'
          />
        )
        try {
          const row = within(view.container).getByRole('row', {
            name: /^base /,
          })
          assert.equal(
            within(row).getAllByRole('cell')[1].textContent,
            expected
          )
        } finally {
          await view.unmount()
        }
      }
    } finally {
      useSystemConfigStore.setState({ config: previous })
    }
  })

  test('shows all request multipliers without disguising an OR time condition as an AND range', async () => {
    const source =
      '(tier("base", p * 2)) * ((hour("UTC") >= 22 || hour("UTC") < 2) ? 0.5 : 1) * (param("fast") == true ? 2 : 1)'
    const view = await testEnv.render(
      <ModelTierPricing {...displayOptions} billingExpr={source} />
    )
    try {
      const content = within(view.container)
      assert.ok(content.getByText('Hour ≥ 22 OR < 2 (UTC)'))
      assert.ok(content.getByText('Body param fast = true'))
      assert.ok(content.getByText('0.5×'))
      assert.ok(content.getByText('2×'))
      assert.equal(
        view.container.querySelector('details code')?.textContent,
        source
      )
    } finally {
      await view.unmount()
    }
  })

  test('warns on impossible time conditions and never invents a matching tier', async () => {
    const view = await testEnv.render(
      <ModelTierPricing
        {...displayOptions}
        billingExpr='hour("UTC") >= 9 && hour("UTC") < 12 && hour("UTC") >= 14 ? tier("peak", p * 2) : tier("off", p * 1)'
      />
    )
    try {
      assert.ok(
        within(view.container).getByText(
          'These conditions cannot match together.'
        )
      )
      assert.doesNotMatch(view.container.textContent || '', /09:00–12:00/)
      assert.equal(within(view.container).queryByText('Matched'), null)
    } finally {
      await view.unmount()
    }
  })

  test('separates prices from conditions in the actual model square without changing the log layout', async () => {
    const queryClient = new QueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ModelDetailsContent
          model={
            {
              id: 2,
              model_name: 'time-priced-model',
              billing_mode: 'tiered_expr',
              billing_expr: expression,
              enable_groups: ['default'],
            } as PricingModel
          }
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
    try {
      const square = within(view.container).getByRole('region', {
        name: 'Tiered price table',
      })
      const prices = within(square).getByRole('table')
      assert.doesNotMatch(prices.textContent || '', /Asia\/Shanghai|&&|\|\|/)
      assert.match(prices.textContent || '', /peak/)
      const conditions = within(square).getByRole('region', {
        name: 'Tier applicability',
      })
      assert.match(conditions.textContent || '', /09:00–12:00/)
      assert.match(conditions.textContent || '', /14:00–18:00/)
      assert.match(conditions.textContent || '', /OR/)
      assert.match(conditions.textContent || '', /When no earlier tier matches/)
      assert.match(square.textContent || '', /1M tokens/)
      assert.equal(
        square.querySelector('details code')?.textContent,
        expression
      )
    } finally {
      await view.unmount()
      queryClient.clear()
    }

    const log = await testEnv.render(
      <DynamicPricingBreakdown billingExpr={expression} />
    )
    try {
      assert.match(
        within(log.container).getByRole('table').textContent || '',
        /Asia\/Shanghai/
      )
      assert.equal(
        within(log.container).queryByRole('region', {
          name: 'Tier applicability',
        }),
        null
      )
    } finally {
      await log.unmount()
    }
  })
})

for (const expression of [
  'len < 100 && c == 5 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)',
  'len < 100 ? tier("peak", p * 2 + c * 4) : max(p, 1)',
  'len < 9007199254740993 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)',
  'len < 1e-999 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)',
  'tier("custom", max(p, 1) * 3 + c * 15)',
  'tier("custom", p * 1e999 + c * 15)',
  'tier("custom", p * 1e-999 + c * 15)',
  'len < 100 ? tier("short", p * 1 + c * 2)',
]) {
  test(`shows the complete raw expression when structured parsing is unsupported: ${expression}`, async (): Promise<void> => {
    const view = await testEnv.render(
      <DynamicPricingBreakdown billingExpr={expression} />
    )
    try {
      const pricing = within(view.container)
      assert.ok(pricing.getByText('Unable to parse structured pricing'))
      assert.ok(pricing.getByText(expression, { exact: true }))
      assert.equal(pricing.queryByRole('table'), null)
    } finally {
      await view.unmount()
    }
  })
}

test('shows configured zero coefficients and leaves absent coefficients blank in both layouts', async (): Promise<void> => {
  const view = await testEnv.render(
    <DynamicPricingBreakdown
      billingExpr={
        'len < 100 ? tier("free", p * 0 + c * 0 + cr * 0 + cc * 0 + cc1h * 0 + img * 0 + img_o * 0 + ai * 0 + ao * 0) : tier("base", p * 2 + c * 4)'
      }
    />
  )
  try {
    const pricing = within(view.container)
    const table = pricing.getByRole('table')
    const desktop = within(table)
    assert.equal(desktop.getAllByRole('columnheader').length, 10)
    const freeRow = desktop.getByRole('row', { name: /^free / })
    const baseRow = desktop.getByRole('row', { name: /^base / })
    assert.deepEqual(
      within(freeRow)
        .getAllByRole('cell')
        .slice(1)
        .map((cell: HTMLElement): string | null => cell.textContent),
      Array(9).fill('$0.0000')
    )
    assert.deepEqual(
      within(baseRow)
        .getAllByRole('cell')
        .slice(3)
        .map((cell: HTMLElement): string | null => cell.textContent),
      Array(7).fill('-')
    )
    // Mobile cards render outside the semantic desktop table.
    const mobilePrices = (price: string): HTMLElement[] =>
      pricing
        .getAllByText(price, { exact: true })
        .filter((element: HTMLElement): boolean => !table.contains(element))
    assert.equal(mobilePrices('$0.0000').length, 9)
    assert.equal(mobilePrices('-').length, 7)
    assert.equal(mobilePrices('$2.0000').length, 1)
    assert.equal(mobilePrices('$4.0000').length, 1)
  } finally {
    await view.unmount()
  }
})

test('still hides cache columns when the request has no cache usage', async (): Promise<void> => {
  const view = await testEnv.render(
    <DynamicPricingBreakdown
      billingExpr={'tier("free", p * 0 + c * 0 + cr * 0 + img * 0)'}
      hideCacheColumns
    />
  )
  try {
    const headers = within(view.container)
      .getAllByRole('columnheader')
      .map((cell: HTMLElement): string | null => cell.textContent)
    assert.deepEqual(headers, ['Tier', 'Input', 'Output', 'Image In'])
  } finally {
    await view.unmount()
  }
})

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
          {
            key: 'experimental_component',
            variant: 'custom',
            unit: 'second',
            unit_price: '0.3',
          },
        ],
      },
    } as PricingModel
  }

  async function renderModel(model: PricingModel): Promise<{
    queryClient: QueryClient
    view: Awaited<ReturnType<typeof testEnv.render>>
  }> {
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
      assert.match(content, /Other pricing component/)
      assert.match(content, /Free input images/)
      assert.match(content, /\/ images/)
      assert.doesNotMatch(content, /\/ image(?!s)/)
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
