// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { act, useEffect } from 'react'
import { useForm, type UseFormReturn } from 'react-hook-form'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import {
  entry,
  receipt,
  quoted,
  deferred,
} from './__tests__/model-pricing-fixture'
import { acknowledgePriceField } from './acknowledge-price-field'
import { ModelPricingError } from './model-pricing-api'
import { ModelPricingDraftSession } from './model-pricing-draft'
import type {
  GuardedPricingChange,
  ModelPricingEntry,
  ModelPricingCommit,
} from './model-pricing-types'

const env = createReactTestEnvironment()
await env.setup()
const storageDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'localStorage'
)
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: env.dom.window.localStorage,
})
const { render, cleanup, fireEvent, waitFor } =
  await import('@testing-library/react/pure')
const { ModelPricingWorkspace } = await import('./model-pricing-workspace')
const { ModelPricingFields } = await import('./model-pricing-fields')
const { RatioSettingsCard } = await import('./ratio-settings-card')
const { SettingsPageProvider } =
  await import('../components/settings-page-context')
const adapter = api.defaults.adapter
const user = useAuthStore.getState().auth.user
const clients: QueryClient[] = []
const actionContainers: HTMLDivElement[] = []
afterEach(async () => {
  await act(async () => {
    cleanup()
    for (const c of clients) c.clear()
    clients.length = 0
    for (const container of actionContainers) container.remove()
    actionContainers.length = 0
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  api.defaults.adapter = adapter
  useAuthStore.getState().auth.setUser(user)
})
after(async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  if (storageDescriptor)
    Object.defineProperty(globalThis, 'localStorage', storageDescriptor)
  else Reflect.deleteProperty(globalThis, 'localStorage')
  env.teardown()
})

function setup(values: ModelPricingEntry[] = [entry()]) {
  const server = new Map(values.map((v) => [v.model_name, structuredClone(v)]))
  const writes: GuardedPricingChange[][] = []
  const methods: string[] = []
  api.defaults.adapter = async (config) => {
    methods.push(config.method!)
    let data: unknown
    if (config.method === 'get') {
      const names: string[] = config.params.getAll('model')
      data = {
        entries: structuredClone(
          names.length
            ? names.map((n) => server.get(n) ?? entry(n))
            : [...server.values()]
        ),
        options: {},
        empty_version: 'empty',
        authority: 'database',
      }
    } else if (config.method === 'patch') {
      const changes = JSON.parse(config.data).changes as GuardedPricingChange[]
      writes.push(changes)
      for (const c of changes)
        server.set(c.model_name, entry(c.model_name, 'price-v2', c.pricing))
      data = {
        ...receipt(server.get(changes[0].model_name)!),
        entries: changes.map((c) => server.get(c.model_name)!),
      }
    } else if (config.url?.endsWith('/preview')) {
      const input = JSON.parse(config.data)
      data = {
        ...server.get(input.model_name),
        configured: input.pricing,
        effective: input.pricing,
        draft_version: 'draft',
      }
    } else if (config.url?.endsWith('/quote'))
      data = quoted(JSON.parse(config.data))
    else throw new Error(`Unexpected method ${config.method}`)
    return {
      config,
      data: { success: true, data },
      status: 200,
      statusText: 'OK',
      headers: {},
    }
  }
  useAuthStore
    .getState()
    .auth.setUser({ id: 9910021, username: 'synthetic-root', role: 100 })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const view = render(
    <I18nextProvider i18n={env.i18n}>
      <QueryClientProvider client={client}>
        <ModelPricingWorkspace />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { ...view, writes, methods, server }
}

type LegacyOptionWrite = { key: string; value: string | boolean }
async function legacyCard(tab: 'models' | 'groups' = 'models') {
  const f = setup([entry('legacy-model', 'price-v1', { ModelPrice: 7 })])
  await act(async () => cleanup())
  const pricingAdapter = api.defaults.adapter
  const writes: LegacyOptionWrite[] = []
  let response: (write: LegacyOptionWrite) => Promise<boolean> = async () =>
    true
  api.defaults.adapter = async (config) => {
    if (config.method !== 'put') {
      if (typeof pricingAdapter !== 'function')
        throw new Error('Missing pricing fixture adapter')
      return pricingAdapter(config)
    }
    const write = JSON.parse(config.data) as LegacyOptionWrite
    writes.push(write)
    const success = await response(write)
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success, message: success ? '' : 'Synthetic rejection' },
    }
  }
  const actions = env.dom.window.document.createElement('div')
  env.dom.window.document.body.appendChild(actions)
  actionContainers.push(actions)
  let modelDefaults = {
    ModelPrice: '{"legacy-model":7}',
    ModelRatio: '{}',
    CacheRatio: '{}',
    CreateCacheRatio: '{}',
    CompletionRatio: '{}',
    ImageRatio: '{}',
    AudioRatio: '{}',
    AudioCompletionRatio: '{}',
    ExposeRatioEnabled: false,
    BillingMode: '{}',
    BillingExpr: '{}',
  }
  let groupDefaults = {
    GroupRatio: '{"default":1}',
    TopupGroupRatio: '{}',
    UserUsableGroups: '{}',
    GroupGroupRatio: '{}',
    AutoGroups: '[]',
    AutoGroupRoutes: '{}',
    DefaultUseAutoGroup: false,
    GroupSpecialUsableGroup: '{}',
  }
  const element = () => (
    <I18nextProvider i18n={env.i18n}>
      <QueryClientProvider client={clients[0]}>
        <SettingsPageProvider actionsContainer={actions}>
          <RatioSettingsCard
            modelDefaults={modelDefaults}
            groupDefaults={groupDefaults}
            toolPricesDefault='{}'
            visibleTabs={[tab]}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
  const view = render(element())
  if (tab === 'models') {
    await waitFor(() =>
      assert.ok(view.getByRole('button', { name: 'legacy-model' }))
    )
    fireEvent.click(view.getByRole('button', { name: 'Legacy price tools' }))
  }
  fireEvent.click(view.getByRole('button', { name: 'Switch to JSON' }))
  return {
    ...view,
    writes,
    actions,
    pricing: f,
    respond(fn: typeof response) {
      response = fn
    },
    async modelPrice(price: number) {
      modelDefaults = {
        ...modelDefaults,
        ModelPrice: JSON.stringify({ 'legacy-model': price }),
      }
      await act(async () => view.rerender(element()))
    },
    async groupRatio(ratio: number) {
      groupDefaults = {
        ...groupDefaults,
        GroupRatio: JSON.stringify({ default: ratio }),
      }
      await act(async () => view.rerender(element()))
    },
  }
}

test('acknowledged legacy zero is no longer dirty when a later explicit edit restores seven', async () => {
  const f = await legacyCard()
  await act(async () =>
    fireEvent.change(f.getByLabelText('Model fixed pricing'), {
      target: { value: '{"legacy-model":0}' },
    })
  )
  await act(async () => {
    fireEvent.click(f.getByRole('button', { name: 'Save model prices' }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await waitFor(() => assert.equal(f.writes.length, 1))
  assert.deepEqual(f.writes, [
    { key: 'ModelPrice', value: '{"legacy-model":0}' },
  ])
  await f.modelPrice(0) // Reread this successful save, then simulate a separate valid edit.
  await f.modelPrice(7)
  assert.equal(
    JSON.parse(
      (f.getByLabelText('Model fixed pricing') as HTMLTextAreaElement).value
    )['legacy-model'],
    7
  )
  await act(async () => {
    fireEvent.click(f.getByRole('button', { name: 'Save model prices' }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  assert.equal(
    f.writes.length,
    1,
    'a clean form must not replay the acknowledged zero'
  )
})

test('clearing an unavailable plugin keeps its selected identity through save and permits returning to default', async () => {
  const value = entry('legacy-model', 'price-v1', {
    ModelPrice: 7,
    'billing_setting.plugin_billing_expr': { retired: '0' },
  })
  value.contexts = []
  value.plugin_variants = [
    {
      id: 'plugin:retired',
      plugin_key: 'retired',
      mapped_model: 'legacy-model',
      configured: '0',
      compatible: false,
      schema_compatible: false,
      available: false,
      execution_ready: false,
      reason: 'source unavailable',
    },
  ]
  const f = setup([value])
  await waitFor(() =>
    assert.ok(f.getByRole('button', { name: 'legacy-model' }))
  )
  fireEvent.click(f.getByRole('button', { name: 'legacy-model' }))
  await waitFor(() => assert.ok(f.queryByLabelText('Pricing source')))
  fireEvent.change(f.getByLabelText('Pricing source'), {
    target: { value: 'plugin:retired' },
  })
  fireEvent.change(f.getByLabelText('Plugin expression override'), {
    target: { value: '' },
  })
  assert.equal(
    (f.getByLabelText('Pricing source') as HTMLSelectElement).value,
    'plugin:retired'
  )
  assert.ok(f.queryByText('source unavailable'))
  fireEvent.click(f.getByRole('button', { name: 'Save model pricing' }))
  await waitFor(() => assert.ok(f.queryByText('Database commit acknowledged.')))
  assert.equal(
    f.writes[0][0].pricing['billing_setting.plugin_billing_expr'],
    undefined
  )
  const saved = f.server.get('legacy-model')!
  saved.contexts = []
  saved.plugin_variants = []
  fireEvent.click(f.getByRole('button', { name: 'Refresh pricing' }))
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  assert.equal(
    (f.getByLabelText('Pricing source') as HTMLSelectElement).value,
    'plugin:retired'
  )
  fireEvent.change(f.getByLabelText('Pricing source'), {
    target: { value: '' },
  })
  assert.equal(
    (f.getByLabelText('ModelPrice (USD / request)') as HTMLInputElement).value,
    '7'
  )
  assert.equal(f.queryByLabelText('Plugin expression override'), null)
})

test('legacy acknowledgement preserves later edits to the same field and unrelated fields', async () => {
  const f = await legacyCard()
  const pending = deferred<boolean>()
  f.respond(() => pending.promise)
  await act(async () =>
    fireEvent.change(f.getByLabelText('Model fixed pricing'), {
      target: { value: '{"legacy-model":0}' },
    })
  )
  await act(async () => {
    fireEvent.click(f.getByRole('button', { name: 'Save model prices' }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  assert.equal(f.writes.length, 1)
  await act(async () => {
    fireEvent.change(f.getByLabelText('Model fixed pricing'), {
      target: { value: '{"legacy-model":2}' },
    })
    fireEvent.change(f.getByLabelText('Model ratio'), {
      target: { value: '{"other-model":3}' },
    })
  })
  await act(async () => {
    pending.resolve(true)
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await f.modelPrice(7)
  assert.equal(
    JSON.parse(
      (f.getByLabelText('Model fixed pricing') as HTMLTextAreaElement).value
    )['legacy-model'],
    2
  )
  assert.equal(
    JSON.parse((f.getByLabelText('Model ratio') as HTMLTextAreaElement).value)[
      'other-model'
    ],
    3
  )
  assert.deepEqual(f.writes, [
    { key: 'ModelPrice', value: '{"legacy-model":0}' },
  ])
})

test('a partially rejected legacy save acknowledges only the successful field', async () => {
  const f = await legacyCard()
  f.respond(async (write) => write.key !== 'CompletionRatio')
  await act(async () => {
    fireEvent.change(f.getByLabelText('Model fixed pricing'), {
      target: { value: '{"legacy-model":0}' },
    })
    fireEvent.change(f.getByLabelText('Completion ratio'), {
      target: { value: '{"legacy-model":2}' },
    })
  })
  await act(async () => {
    fireEvent.click(f.getByRole('button', { name: 'Save model prices' }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await waitFor(() => assert.equal(f.writes.length, 2))
  await f.modelPrice(7)
  assert.equal(
    JSON.parse(
      (f.getByLabelText('Model fixed pricing') as HTMLTextAreaElement).value
    )['legacy-model'],
    7
  )
  assert.equal(
    JSON.parse(
      (f.getByLabelText('Completion ratio') as HTMLTextAreaElement).value
    )['legacy-model'],
    2
  )
  assert.deepEqual(
    f.writes.map((w) => w.key),
    ['ModelPrice', 'CompletionRatio']
  )
})

for (const failure of ['rejected', 'unknown'] as const) {
  test(`legacy ${failure} write remains dirty across a configuration refresh`, async () => {
    const f = await legacyCard()
    f.respond(async () => {
      if (failure === 'unknown') throw new Error('Synthetic lost response')
      return false
    })
    await act(async () =>
      fireEvent.change(f.getByLabelText('Model fixed pricing'), {
        target: { value: '{"legacy-model":0}' },
      })
    )
    await act(async () => {
      fireEvent.click(f.getByRole('button', { name: 'Save model prices' }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    await waitFor(() => assert.equal(f.writes.length, 1))
    await f.modelPrice(7)
    assert.equal(
      JSON.parse(
        (f.getByLabelText('Model fixed pricing') as HTMLTextAreaElement).value
      )['legacy-model'],
      0
    )
    assert.equal(f.writes.length, 1)
  })
}

test('group price acknowledgement clears saved zero before a separate edit restores one', async () => {
  const f = await legacyCard('groups')
  await act(async () =>
    fireEvent.change(f.getByLabelText('Group ratios'), {
      target: { value: '{"default":0}' },
    })
  )
  await act(async () => {
    fireEvent.click(f.getByRole('button', { name: 'Save group ratios' }))
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await waitFor(() => assert.ok(f.writes.some((w) => w.key === 'GroupRatio')))
  await f.groupRatio(0)
  await f.groupRatio(1)
  assert.equal(
    JSON.parse((f.getByLabelText('Group ratios') as HTMLTextAreaElement).value)
      .default,
    1
  )
})

test('acknowledgement handles visual fields without Controllers and an explicit false value', async () => {
  type Values = { price: string; other: string; expose: boolean }
  let form!: UseFormReturn<Values>
  function Harness() {
    const current = useForm<Values>({
      defaultValues: { price: '7', other: '1', expose: true },
    })
    useEffect(() => {
      form = current
    }, [current])
    return <output>{JSON.stringify(current.formState.dirtyFields)}</output>
  }
  render(<Harness />)
  await act(async () => {
    form.setValue('price', '0', { shouldDirty: true })
    form.setValue('other', '2', { shouldDirty: true })
    form.setValue('expose', false, { shouldDirty: true })
    acknowledgePriceField(form, 'price', '0', '0')
    acknowledgePriceField(form, 'expose', false, false)
  })
  assert.equal(form.getFieldState('price').isDirty, false)
  assert.equal(form.getFieldState('expose').isDirty, false)
  assert.equal(form.getFieldState('other').isDirty, true)
  await act(async () =>
    form.reset(
      { price: '7', other: '1', expose: true },
      { keepDirtyValues: true }
    )
  )
  assert.deepEqual(form.getValues(), { price: '7', other: '2', expose: true })
})

test('visible editor saves explicit zero with hidden plugin data and base guards, then uses server preview/quote', async () => {
  const v = setup()
  await waitFor(() => assert.ok(v.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(v.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() =>
    assert.equal(
      (v.getByLabelText('ModelPrice (USD / request)') as HTMLInputElement)
        .value,
      '7'
    )
  )
  fireEvent.change(v.getByLabelText('ModelPrice (USD / request)'), {
    target: { value: '0' },
  })
  fireEvent.click(v.getByRole('button', { name: 'Save model pricing' }))
  await waitFor(() => assert.equal(v.writes.length, 1))
  assert.deepEqual(v.writes[0][0], {
    model_name: 'kling-v1',
    expected_version: 'price-v1',
    expected_contract_digest: 'contracts-v1',
    expected_binding_version: 'binding-v1',
    pricing: {
      ModelPrice: 0,
      CacheRatio: 1.5,
      'billing_setting.billing_mode': 'tiered_expr',
      'billing_setting.billing_expr': 'u("units") * 0.1',
      'billing_setting.plugin_billing_expr': { kling: '0' },
    },
  })
  await waitFor(() => assert.ok(v.queryByText('Database commit acknowledged.')))
  fireEvent.click(v.getByRole('button', { name: 'Preview configuration' }))
  await waitFor(() => assert.ok(v.queryByText('Configuration preview')))
  fireEvent.change(v.getByLabelText('Pricing source'), {
    target: { value: 'channel:1' },
  })
  fireEvent.change(v.getByLabelText('Quote sample JSON'), {
    target: {
      value: '{"group":"default","submit":{"units":1},"actual":{"units":0.6}}',
    },
  })
  fireEvent.click(v.getByRole('button', { name: 'Calculate task quote' }))
  await waitFor(() =>
    assert.match(
      v.getByLabelText('Quote result').textContent!,
      /Final amount: USD 0.06/
    )
  )
  assert.equal(v.methods.includes('put'), false)
})

test('typing decimal prices preserves every zero instead of turning 0.05 into 5', async () => {
  const f = setup()
  await waitFor(() => assert.ok(f.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(f.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() =>
    assert.ok(f.queryByLabelText('ModelPrice (USD / request)'))
  )
  const input = f.getByLabelText(
    'ModelPrice (USD / request)'
  ) as HTMLInputElement
  for (const amount of ['0.002', '1.05', '0.05']) {
    fireEvent.change(input, { target: { value: '' } })
    let prefix = ''
    for (const character of amount) {
      prefix += character
      fireEvent.change(input, { target: { value: input.value + character } })
      assert.equal(input.value, prefix, 'each keystroke must remain visible')
    }
  }
  fireEvent.click(f.getByRole('button', { name: 'Save model pricing' }))
  await waitFor(() => assert.ok(f.queryByText('Database commit acknowledged.')))
  assert.equal(f.writes[0][0].pricing.ModelPrice, 0.05)
  assert.equal(input.value, '0.05')
})

test('full JSON editing retains formatting through valid intermediate decimal input', async () => {
  const f = setup()
  await waitFor(() => assert.ok(f.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(f.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() => assert.ok(f.queryByLabelText('Full model JSON / import')))
  const input = f.getByLabelText(
    'Full model JSON / import'
  ) as HTMLTextAreaElement
  for (const text of [
    '{ "ModelPrice": 0. }',
    '{ "ModelPrice": 0.0 }',
    '{ "ModelPrice": 0.05 }',
  ]) {
    fireEvent.change(input, { target: { value: text } })
    assert.equal(
      input.value,
      text,
      'valid JSON is still editable text, not canonical display JSON'
    )
  }
  fireEvent.click(f.getByRole('button', { name: 'Save model pricing' }))
  await waitFor(() => assert.ok(f.queryByText('Database commit acknowledged.')))
  assert.equal(f.writes[0][0].pricing.ModelPrice, 0.05)
})

test('incomplete visible input survives refresh and switching models without permitting a save', async () => {
  const v = setup([entry(), entry('second')])
  await waitFor(() => assert.ok(v.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(v.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() =>
    assert.ok(v.queryByLabelText('ModelPrice (USD / request)'))
  )
  fireEvent.change(v.getByLabelText('ModelPrice (USD / request)'), {
    target: { value: '1.' },
  })
  fireEvent.click(v.getByRole('button', { name: 'Refresh pricing' }))
  await waitFor(() =>
    assert.equal(
      (v.getByLabelText('ModelPrice (USD / request)') as HTMLInputElement)
        .value,
      '1.'
    )
  )
  fireEvent.click(v.getByRole('button', { name: 'second' }))
  await waitFor(() => assert.ok(v.queryByText('second', { selector: 'h2' })))
  fireEvent.click(v.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() =>
    assert.equal(
      (v.getByLabelText('ModelPrice (USD / request)') as HTMLInputElement)
        .value,
      '1.'
    )
  )
  assert.equal(
    (v.getByRole('button', { name: 'Save model pricing' }) as HTMLButtonElement)
      .disabled,
    true
  )
  assert.equal(v.writes.length, 0)
})

test('schema-only plugin exposes inheritance and explicit expression zero but cannot quote', async () => {
  const value = entry()
  value.contexts.push({
    id: 'plugin:kling',
    plugin_key: 'kling',
    mapped_model: value.model_name,
    usage_schema: { units: { type: 'number', unit: 'credit' } },
    compatible: true,
    schema_compatible: true,
    available: false,
    execution_ready: false,
    reason: 'no vetted host phase contract',
  })
  const v = setup([value])
  await waitFor(() => assert.ok(v.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(v.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() => assert.ok(v.queryByLabelText('Pricing source')))
  fireEvent.change(v.getByLabelText('Pricing source'), {
    target: { value: 'plugin:kling' },
  })
  assert.equal(
    (v.getByLabelText('Plugin expression override') as HTMLTextAreaElement)
      .value,
    '0'
  )
  assert.equal(
    (
      v.getByRole('button', {
        name: 'Calculate task quote',
      }) as HTMLButtonElement
    ).disabled,
    true
  )
  fireEvent.change(v.getByLabelText('Plugin expression override'), {
    target: { value: '' },
  })
  fireEvent.click(v.getByRole('button', { name: 'Save model pricing' }))
  await waitFor(() => assert.equal(v.writes.length, 1))
  assert.equal(
    v.writes[0][0].pricing['billing_setting.plugin_billing_expr'],
    undefined
  )
  await waitFor(() => assert.ok(v.queryByText('Database commit acknowledged.')))
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
})

test('auth change hides previous configuration and a non-Root mount makes no pricing request', async () => {
  const v = setup()
  await waitFor(() => assert.ok(v.getByRole('button', { name: 'kling-v1' })))
  await act(async () =>
    useAuthStore
      .getState()
      .auth.setUser({ id: 9910022, username: 'synthetic-admin', role: 10 })
  )
  assert.equal(v.queryByRole('button', { name: 'kling-v1' }), null)
  const before = v.methods.length
  await act(async () =>
    v.rerender(
      <I18nextProvider i18n={env.i18n}>
        <QueryClientProvider client={clients[0]}>
          <ModelPricingWorkspace />
        </QueryClientProvider>
      </I18nextProvider>
    )
  )
  assert.equal(v.methods.length, before)
})

test('batch confirmation sends both observed target guards in one request and leaves the source draft intact', async () => {
  const v = setup([entry(), entry('second'), entry('third')])
  await waitFor(() => assert.ok(v.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(v.getByRole('button', { name: 'kling-v1' }))
  await waitFor(() =>
    assert.ok(v.queryByLabelText('ModelPrice (USD / request)'))
  )
  fireEvent.change(v.getByLabelText('ModelPrice (USD / request)'), {
    target: { value: '0' },
  })
  fireEvent.click(v.getByLabelText('Select second'))
  fireEvent.click(v.getByLabelText('Select third'))
  fireEvent.click(v.getByRole('button', { name: 'Copy pricing to selected' }))
  assert.equal(
    v.writes.length,
    0,
    'opening confirmation must not mutate prices'
  )
  fireEvent.click(v.getByRole('button', { name: 'Continue' }))
  await waitFor(() => assert.equal(v.writes.length, 1))
  assert.deepEqual(
    v.writes[0].map((c) => [
      c.model_name,
      c.expected_version,
      c.expected_contract_digest,
      c.expected_binding_version,
      c.pricing.ModelPrice,
    ]),
    [
      ['second', 'price-v1', 'contracts-v1', 'binding-v1', 0],
      ['third', 'price-v1', 'contracts-v1', 'binding-v1', 0],
    ]
  )
  assert.equal(
    v.server.get('kling-v1')!.configured.ModelPrice,
    7,
    'copying does not save the source'
  )
  await waitFor(() =>
    assert.equal(
      v.queryByText('Resolve these model writes before another batch'),
      null
    )
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
})

test('pricing fields lock unresolved batch targets while individual pending saves remain editable', async () => {
  let observed = 7
  const batch = deferred<ModelPricingCommit>(),
    single = deferred<ModelPricingCommit>()
  const read = async (name: string) =>
    entry(name, observed === 7 ? 'price-v1' : 'new', { ModelPrice: observed })
  const session = new ModelPricingDraftSession({
    read,
    save: () => single.promise,
    saveMany: () => batch.promise,
    preview: async (name, pricing) => ({
      ...entry(name, 'price-v1', pricing),
      draft_version: 'draft',
    }),
    quote: async (input) => quoted(input),
  })
  session.select('target')
  await session.refresh()
  const element = () => (
    <I18nextProvider i18n={env.i18n}>
      <ModelPricingFields
        document={session.getSnapshot().document!}
        session={session}
      />
    </I18nextProvider>
  )
  const view = render(element())
  const numeric = view.getByLabelText('ModelPrice (USD / request)')
  const json = view.getByLabelText('Full model JSON / import')
  assert.equal(numeric.matches(':disabled'), false)
  const saving = session.saveBatch(
    [await read('target'), await read('other')],
    { ModelPrice: 0 }
  )
  view.rerender(element())
  assert.equal(numeric.matches(':disabled'), true)
  assert.equal(json.matches(':disabled'), true)
  batch.reject(new ModelPricingError('unknown-result', 'Lost receipt'))
  assert.equal(await saving, false)
  view.rerender(element())
  assert.equal(numeric.matches(':disabled'), true)
  observed = 0
  assert.equal(await session.reconcileModels(['target', 'other']), true)
  view.rerender(element())
  assert.equal(numeric.matches(':disabled'), false)
  session.editNumber('ModelPrice', '1')
  const individual = session.save()
  view.rerender(element())
  assert.equal(
    numeric.matches(':disabled'),
    false,
    'individual saves retain their edit-during-write contract'
  )
  observed = 1
  single.resolve(receipt(await read('target')))
  assert.equal(await individual, true)
})

test('system-option refresh preserves an unsaved legacy JSON field beside the new editor', async () => {
  const f = setup()
  await act(async () => cleanup())
  const { RatioSettingsCard } = await import('./ratio-settings-card')
  const modelDefaults = {
    ModelPrice: '{"kling-v1":7}',
    ModelRatio: '{}',
    CacheRatio: '{}',
    CreateCacheRatio: '{}',
    CompletionRatio: '{}',
    ImageRatio: '{}',
    AudioRatio: '{}',
    AudioCompletionRatio: '{}',
    ExposeRatioEnabled: false,
    BillingMode: '{}',
    BillingExpr: '{}',
  }
  const groupDefaults = {
    GroupRatio: '{}',
    TopupGroupRatio: '{}',
    UserUsableGroups: '{}',
    GroupGroupRatio: '{}',
    AutoGroups: '[]',
    AutoGroupRoutes: '{}',
    DefaultUseAutoGroup: false,
    GroupSpecialUsableGroup: '{}',
  }
  const element = (price: string) => (
    <I18nextProvider i18n={env.i18n}>
      <QueryClientProvider client={clients[0]}>
        <RatioSettingsCard
          modelDefaults={{ ...modelDefaults, ModelPrice: price }}
          groupDefaults={groupDefaults}
          toolPricesDefault='{}'
          visibleTabs={['models']}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
  const view = render(element(modelDefaults.ModelPrice))
  await waitFor(() => assert.ok(view.getByRole('button', { name: 'kling-v1' })))
  fireEvent.click(view.getByRole('button', { name: 'Legacy price tools' }))
  fireEvent.click(view.getByRole('button', { name: 'Switch to JSON' }))
  await act(async () => {
    fireEvent.change(view.getByLabelText('Model fixed pricing'), {
      target: { value: '{"kling-v1":0}' },
    })
  })
  await act(async () => view.rerender(element('{"kling-v1":5}')))
  await waitFor(() =>
    assert.equal(
      (view.getByLabelText('Model fixed pricing') as HTMLTextAreaElement).value,
      '{"kling-v1":0}'
    )
  )
  assert.equal(f.writes.length, 0)
})
