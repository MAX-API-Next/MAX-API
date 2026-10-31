// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import {
  AxiosError,
  type AxiosAdapter,
  type InternalAxiosRequestConfig,
} from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api } from '@/lib/api'
import {
  entry,
  receipt,
  quoted,
  deferred,
} from './__tests__/model-pricing-fixture'
import {
  getModelPricing,
  modelPricingTransport,
  saveModelPricing,
  previewModelPricing,
  quoteModelPricing,
  ModelPricingError,
} from './model-pricing-api'
import { ModelPricingDraftSession } from './model-pricing-draft'
import {
  normalizePricing,
  pricingChange,
  pricingEqual,
} from './model-pricing-types'

const original = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = original
})
function respond(
  value: unknown,
  inspect?: (config: InternalAxiosRequestConfig) => void
) {
  api.defaults.adapter = (async (config) => {
    inspect?.(config)
    return {
      data: { success: true, message: '', data: value },
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }) satisfies AxiosAdapter
}
test('pricing GET preserves namespaced model names and always bypasses GET deduplication', async () => {
  const value = entry('foo::bar')
  const controller = new AbortController()
  respond(
    {
      entries: [value],
      options: {},
      empty_version: 'empty',
      authority: 'database',
    },
    (c) => {
      assert.equal(c.url, '/api/option/model_pricing')
      assert.deepEqual(c.params.getAll('model'), ['foo::bar', 'other'])
      assert.equal(c.disableDuplicate, true)
      assert.equal(c.signal, controller.signal)
    }
  )
  const snapshot = await getModelPricing(
    ['foo::bar', 'other'],
    controller.signal
  )
  assert.deepEqual(snapshot.entries[0], value)
})
test('PATCH sends complete model values and all guards, preserving zero/missing/reset false', async () => {
  const base = entry()
  const values = { ...base.configured, ModelPrice: 0 }
  delete values.CacheRatio
  respond(receipt(entry(base.model_name, 'price-v2', values), false), (c) => {
    assert.equal(c.method, 'patch')
    const body = JSON.parse(c.data)
    assert.deepEqual(body, {
      changes: [
        {
          model_name: 'kling-v1',
          expected_version: 'price-v1',
          expected_contract_digest: 'contracts-v1',
          expected_binding_version: 'binding-v1',
          pricing: {
            ModelPrice: 0,
            'billing_setting.billing_mode': 'tiered_expr',
            'billing_setting.billing_expr': 'u("units") * 0.1',
            'billing_setting.plugin_billing_expr': { kling: '0' },
          },
          reset: false,
        },
      ],
    })
    assert.equal(body.changes[0].pricing.ModelPrice, 0)
    assert.equal(
      body.changes[0].pricing['billing_setting.plugin_billing_expr'].kling,
      '0'
    )
    assert.equal(Object.hasOwn(body.changes[0].pricing, 'CacheRatio'), false)
  })
  const result = await saveModelPricing([
    { ...pricingChange(base, values), reset: false },
  ])
  assert.equal(result.committed, true)
  assert.equal(result.cache_published, false)
  assert.ok(result.warning)
})
test('preview keeps base version separate and quote retains single-contract versus aggregate guards', async () => {
  const base = entry()
  respond({ ...base, draft_version: 'candidate-price' })
  assert.equal(
    (await previewModelPricing(base.model_name, base.configured)).version,
    'price-v1'
  )
  const { reset: _reset, ...change } = pricingChange(base, base.configured)
  const input = {
    ...change,
    context_id: 'channel:1',
    group: 'default',
    submit: { units: 1 },
    actual: { units: 0.6 },
  }
  respond(quoted(input))
  const result = await quoteModelPricing(input)
  assert.equal(result.preview.final?.quota_after_group, 60)
  assert.notEqual(result.contract_digest, result.preview.contract_digest)
  let requests = 0
  respond(quoted(input), () => {
    requests++
  })
  await assert.rejects(
    quoteModelPricing({ ...input, quota_per_unit: 1 } as typeof input)
  )
  assert.equal(requests, 0)
})
test('malformed responses, failed envelopes and absent model entries never become usable prices', async () => {
  respond({
    ...entry(),
    configured: { ...entry().configured, ModelPrice: null },
  })
  await assert.rejects(
    previewModelPricing('kling-v1', {}),
    (e) => e instanceof ModelPricingError && e.kind === 'invalid-response'
  )
  respond({
    entries: [],
    options: {},
    empty_version: 'empty',
    authority: 'database',
  })
  await assert.rejects(modelPricingTransport.read('kling-v1'))
  api.defaults.adapter = async (c) => ({
    config: c,
    status: 200,
    statusText: 'OK',
    headers: {},
    data: { success: false, message: 'Denied' },
  })
  await assert.rejects(
    getModelPricing(),
    (e) =>
      e instanceof ModelPricingError &&
      e.kind === 'rejected' &&
      e.message === 'Denied'
  )
})
test('HTTP conflicts, definite rejection and ambiguous write failures remain distinct', async () => {
  for (const [status, kind] of [
    [409, 'conflict'],
    [400, 'rejected'],
    [503, 'unknown-result'],
    [408, 'unknown-result'],
  ] as const) {
    api.defaults.adapter = async (c) => {
      throw new AxiosError('Synthetic', undefined, c, undefined, {
        config: c,
        status,
        statusText: 'Synthetic',
        headers: {},
        data: { success: false, message: 'Server message' },
      })
    }
    await assert.rejects(
      saveModelPricing([pricingChange(entry(), {})]),
      (e) =>
        e instanceof ModelPricingError && e.kind === kind && e.status === status
    )
  }
})
test('full draft validation rejects unknown fields without discarding hidden prices', () => {
  assert.throws(() =>
    normalizePricing({ ModelPrice: 0, future_price: 1 } as {
      ModelPrice: number
    })
  )
  assert.ok(pricingEqual({ 'billing_setting.plugin_billing_expr': {} }, {}))
  assert.equal(pricingEqual({ ModelPrice: 0 }, {}), false)
  assert.equal(
    pricingEqual({ 'billing_setting.plugin_billing_expr': { kling: '0' } }, {}),
    false
  )
})

test('diagnosed obsolete modes remain readable but cannot be written without correction', async () => {
  const old = entry('legacy-model', 'old-price', {
    'billing_setting.billing_mode': 'obsolete-mode',
    'billing_setting.billing_expr': 'dormant legacy text',
  })
  old.configuration_error = 'invalid billing mode'
  respond({
    entries: [old],
    options: {},
    empty_version: 'empty',
    authority: 'database',
  })
  const loaded = await modelPricingTransport.read('legacy-model')
  assert.equal(
    loaded.configured['billing_setting.billing_mode'],
    'obsolete-mode'
  )
  assert.equal(loaded.configuration_error, 'invalid billing mode')
  assert.throws(() => pricingChange(loaded, loaded.configured))
  assert.equal(
    pricingChange(loaded, {
      ...loaded.configured,
      'billing_setting.billing_mode': 'ratio',
    }).pricing['billing_setting.billing_mode'],
    'ratio'
  )
})

test('post-PATCH confirmation cannot reuse another component pre-PATCH in-flight GET', async () => {
  let current = entry()
  let reads = 0
  const held = deferred<unknown>()
  api.defaults.adapter = async (c) => {
    if (c.method === 'patch') {
      const change = JSON.parse(c.data).changes[0]
      current = entry(change.model_name, 'price-v2', change.pricing)
      return {
        config: c,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: { success: true, data: receipt(current) },
      }
    }
    reads++
    let data: unknown = {
      entries: [structuredClone(current)],
      options: {},
      empty_version: 'empty',
      authority: 'database',
    }
    if (reads === 2) data = await held.promise
    return {
      config: c,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data },
    }
  }
  const session = new ModelPricingDraftSession()
  session.select('kling-v1')
  await session.refresh()
  const old = getModelPricing(['kling-v1'])
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(reads, 2)
  session.replacePricing({ ModelPrice: 0 })
  const saving = session.save()
  await new Promise((resolve) => setTimeout(resolve, 0))
  assert.equal(
    reads,
    3,
    'confirmation must dispatch an independent GET after PATCH'
  )
  held.resolve({
    entries: [entry()],
    options: {},
    empty_version: 'empty',
    authority: 'database',
  })
  await old
  assert.equal(await saving, true)
  assert.equal(session.getSnapshot().document?.base?.version, 'price-v2')
  assert.equal(session.getSnapshot().document?.pricing?.ModelPrice, 0)
})
