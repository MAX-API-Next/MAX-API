// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  entry,
  receipt,
  quoted,
  deferred,
} from './__tests__/model-pricing-fixture'
import {
  ModelPricingError,
  type ModelPricingTransport,
} from './model-pricing-api'
import { ModelPricingDraftSession } from './model-pricing-draft'
import type {
  GuardedPricingChange,
  ModelPricingCommit,
  ModelPricingEntry,
  ModelPricingQuote,
  ModelPricingQuoteRequest,
} from './model-pricing-types'

function fixture() {
  let server = entry()
  const sent: GuardedPricingChange[] = []
  const transport: ModelPricingTransport = {
    read: async (name) => ({ ...structuredClone(server), model_name: name }),
    save: async (change) => {
      sent.push(structuredClone(change))
      server = entry(change.model_name, 'price-v2', change.pricing)
      return receipt(server)
    },
    preview: async (name, pricing) => ({
      ...entry(name, server.version, pricing),
      draft_version: 'candidate-version',
    }),
    quote: async (input) => quoted(input),
  }
  const session = new ModelPricingDraftSession(transport)
  session.select('kling-v1')
  return {
    session,
    transport,
    sent,
    setServer(value: ModelPricingEntry) {
      server = value
    },
    view: () => session.getSnapshot().document!,
  }
}
test('draft keeps explicit zero, inherited absence and complete hidden configuration on save', async () => {
  const f = fixture()
  await f.session.refresh()
  const pricing = { ...f.view().pricing!, ModelPrice: 0 }
  delete pricing.CacheRatio
  f.session.replacePricing(pricing)
  assert.equal(await f.session.save(), true)
  assert.equal(f.sent.length, 1)
  assert.equal(f.sent[0].expected_version, 'price-v1')
  assert.equal(f.sent[0].expected_contract_digest, 'contracts-v1')
  assert.equal(f.sent[0].expected_binding_version, 'binding-v1')
  assert.deepEqual(f.sent[0].pricing, pricing)
  assert.equal(
    f.sent[0].pricing['billing_setting.plugin_billing_expr']?.kling,
    '0'
  )
  assert.equal(f.view().dirty, false)
  assert.equal(f.view().commitKnown, true)
})
test('numeric display text follows draft ownership and never becomes a second price', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.editNumber('ModelPrice', '0.050')
  assert.equal(f.view().pricing?.ModelPrice, 0.05)
  assert.equal(f.view().texts?.ModelPrice, '0.050')
  await f.session.refresh()
  assert.equal(f.view().texts?.ModelPrice, '0.050')
  assert.equal(await f.session.save(), true)
  assert.equal(f.sent[0].pricing.ModelPrice, 0.05)
  assert.deepEqual(f.view().texts, {})
  f.session.editNumber('ModelPrice', '0.0500')
  f.session.replacePricing({ ...f.view().pricing!, ModelPrice: 7 })
  assert.equal(f.view().texts?.ModelPrice, undefined)
  f.session.editNumber('ModelPrice', '0.020')
  assert.equal(f.session.importPricing('{"ModelPrice":0}'), true)
  assert.equal(f.view().texts?.$json, '{"ModelPrice":0}')
  f.session.editNumber('ModelPrice', '0.030')
  f.session.discardLocal()
  assert.deepEqual(f.view().texts, {})
  f.session.editNumber('ModelPrice', '0.040')
  f.setServer(entry('kling-v1', 'price-v3', { ModelPrice: 5 }))
  await f.session.refresh()
  assert.equal(f.view().texts?.ModelPrice, '0.040')
  f.session.adoptRemote()
  assert.deepEqual(f.view().texts, {})
  assert.equal(f.view().pricing?.ModelPrice, 5)
})
test('a save acknowledgment preserves decimal text entered after submission', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.editNumber('ModelPrice', '0.050')
  const pending = deferred<ModelPricingCommit>()
  f.transport.save = () => pending.promise
  const saving = f.session.save()
  f.session.editNumber('ModelPrice', '0.0020')
  const saved = entry('kling-v1', 'price-v2', {
    ...f.view().base!.configured,
    ModelPrice: 0.05,
  })
  f.setServer(saved)
  pending.resolve(receipt(saved))
  assert.equal(await saving, true)
  assert.equal(f.view().base?.configured.ModelPrice, 0.05)
  assert.equal(f.view().pricing?.ModelPrice, 0.002)
  assert.equal(f.view().texts?.ModelPrice, '0.0020')
  assert.equal(f.view().dirty, true)
})
test('refresh preserves numeric text even while its numeric value still equals the baseline', async () => {
  const f = fixture()
  f.setServer(entry('kling-v1', 'zero', { ModelPrice: 0 }))
  await f.session.refresh()
  f.session.editNumber('ModelPrice', '0.0')
  await f.session.refresh()
  assert.equal(f.view().texts?.ModelPrice, '0.0')
  assert.equal(f.view().pricing?.ModelPrice, 0)
  f.session.editNumber('ModelPrice', `${f.view().texts?.ModelPrice}5`)
  assert.equal(f.view().pricing?.ModelPrice, 0.05)
})
test('valid JSON remains an owned display draft until another edit or authoritative replacement', async () => {
  const f = fixture()
  f.setServer(entry('kling-v1', 'zero', { ModelPrice: 0 }))
  await f.session.refresh()
  const text = '{ "ModelPrice": 0.0 }'
  assert.equal(f.session.importPricing(text), true)
  await f.session.refresh()
  assert.equal(f.view().texts?.$json, text)
  assert.equal(f.view().writeState, 'idle')
  f.session.select('second')
  f.session.select('kling-v1')
  assert.equal(f.view().texts?.$json, text)
  f.session.editNumber('ModelPrice', '0.05')
  assert.equal(f.view().texts?.$json, undefined)
  assert.equal(f.session.importPricing('{ "ModelPrice": 0.050 }'), true)
  assert.equal(await f.session.save(), true)
  assert.equal(f.sent[0].pricing.ModelPrice, 0.05)
  assert.deepEqual(f.view().texts, {})
})
test('newest local read wins without ordering content hashes; dirty edits and import survive refresh', async () => {
  const f = fixture()
  await f.session.refresh()
  const old = deferred<ModelPricingEntry>(),
    fresh = deferred<ModelPricingEntry>()
  let requests = 0
  f.transport.read = () => (++requests === 1 ? old.promise : fresh.promise)
  const a = f.session.refresh(),
    b = f.session.refresh()
  fresh.resolve(entry('kling-v1', 'a-newer-hash', { ModelPrice: 2 }))
  await b
  old.resolve(entry('kling-v1', 'z-older-hash', { ModelPrice: 1 }))
  await a
  assert.equal(f.view().base?.version, 'a-newer-hash')
  const pending = deferred<ModelPricingEntry>()
  f.transport.read = () => pending.promise
  const reading = f.session.refresh()
  f.session.replacePricing({
    ModelPrice: 0,
    'billing_setting.plugin_billing_expr': { kling: '0' },
  })
  pending.resolve(entry('kling-v1', 'newest', { ModelPrice: 3 }))
  await reading
  assert.equal(f.view().pricing?.ModelPrice, 0)
  assert.equal(f.view().base?.version, 'a-newer-hash')
  assert.equal(f.view().remote?.version, 'newest')
  assert.equal(f.view().writeState, 'conflict')
  await assert.rejects(f.session.save())
  assert.equal(f.sent.length, 0)
  f.session.adoptRemote()
  assert.equal(f.view().pricing?.ModelPrice, 3)
  assert.equal(f.view().dirty, false)
})
test('model/source switches retain their own drafts and ignore previous-model responses', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.replacePricing({ ModelPrice: 0 })
  const old = deferred<ModelPricingEntry>()
  f.transport.read = () => old.promise
  const reading = f.session.refresh()
  f.session.select('second')
  f.transport.read = async (name) => entry(name, 'second-v1', { ModelPrice: 4 })
  await f.session.refresh()
  old.resolve(entry('kling-v1', 'late', { ModelPrice: 99 }))
  await reading
  assert.equal(f.session.getSnapshot().model, 'second')
  assert.equal(f.view().pricing?.ModelPrice, 4)
  f.session.select('kling-v1')
  f.session.selectSource('channel:1')
  assert.equal(f.view().pricing?.ModelPrice, 0)
  assert.equal(f.view().dirty, true)
  const exposed = f.view().pricing!
  assert.throws(() => {
    exposed.ModelPrice = 999
  }, 'external store snapshots must be immutable')
  f.session.selectSource('plugin:missing')
  assert.equal(
    f.view().pricing?.ModelPrice,
    0,
    'internal draft never aliases returned objects'
  )
})
test('late preview/quote cannot describe newer edits, a different source or changed sample facts', async () => {
  const f = fixture()
  await f.session.refresh()
  const projected = deferred<ModelPricingEntry>()
  f.transport.preview = () => projected.promise
  const previewing = f.session.preview()
  f.session.replacePricing({ ...f.view().pricing!, ModelPrice: 0 })
  projected.resolve({ ...entry(), draft_version: 'old-candidate' })
  await previewing
  assert.equal(f.view().preview, undefined)
  f.session.selectSource('channel:1')
  f.session.setSample({
    group: 'default',
    submit: { units: 1 },
    actual: { units: 0.6 },
  })
  const priced = deferred<ModelPricingQuote>()
  let input!: ModelPricingQuoteRequest
  f.transport.quote = (request) => {
    input = request
    return priced.promise
  }
  const quoting = f.session.quote()
  f.session.setSample({
    group: 'default',
    submit: { units: 2 },
    actual: { units: 0 },
  })
  priced.resolve(quoted(input))
  await quoting
  assert.equal(f.view().quote, undefined)
  f.transport.quote = async (request) => quoted(request)
  await f.session.quote()
  assert.equal(f.view().quote?.request.actual?.units, 0)
  f.session.selectSource('plugin:missing')
  assert.equal(f.view().quote, undefined)
  assert.equal(await f.session.quote(), false)
})
test('preview uses base guards rather than draft_version and refuses drift without inventing a baseline', async () => {
  const f = fixture()
  await f.session.refresh()
  assert.equal(await f.session.preview(), true)
  assert.equal(f.view().preview?.draft_version, 'candidate-version')
  assert.equal(f.view().base?.version, 'price-v1')
  f.transport.preview = async (name, values) => ({
    ...entry(name, 'new-server-price', values),
    draft_version: 'candidate',
  })
  assert.equal(await f.session.preview(), false)
  assert.equal(f.view().writeState, 'conflict')
  assert.equal(f.view().base?.version, 'price-v1')
  assert.equal(
    f.view().remote,
    undefined,
    'preview configured is the draft, not a remote saved baseline'
  )
})
test('edits during PATCH and switching models cannot be cleared by its acknowledgement/readback', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.replacePricing({ ModelPrice: 2 })
  const ack = deferred<ModelPricingCommit>()
  f.transport.save = async (change) => {
    f.sent.push(change)
    return ack.promise
  }
  const saving = f.session.save()
  f.session.replacePricing({ ModelPrice: 3 })
  f.session.select('second')
  await f.session.refresh()
  f.setServer(entry('kling-v1', 'price-v2', { ModelPrice: 2 }))
  ack.resolve(receipt(entry('kling-v1', 'price-v2', { ModelPrice: 2 }), false))
  await saving
  assert.equal(f.session.getSnapshot().model, 'second')
  f.session.select('kling-v1')
  assert.equal(f.view().pricing?.ModelPrice, 3)
  assert.equal(f.view().base?.configured.ModelPrice, 2)
  assert.equal(f.view().dirty, true)
  assert.equal(f.view().cachePublished, false)
  assert.ok(f.view().warning)
})
test('lost PATCH response is reconciled by a fresh read, without pretending the commit was acknowledged', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.replacePricing({ ModelPrice: 2 })
  f.transport.save = async (change) => {
    f.sent.push(change)
    f.setServer(entry('kling-v1', 'price-v2', change.pricing))
    throw new ModelPricingError('unknown-result', 'Lost response')
  }
  assert.equal(await f.session.save(), true)
  assert.equal(f.sent.length, 1)
  assert.equal(f.view().writeState, 'idle')
  assert.equal(f.view().commitKnown, false)
  assert.equal(f.view().dirty, false)
})
test('old or failed readback leaves an unknown write blocked; a later observation can resolve it', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.replacePricing({ ModelPrice: 2 })
  f.transport.save = async (change) => {
    f.sent.push(change)
    throw new ModelPricingError('unknown-result', 'Lost response')
  }
  assert.equal(await f.session.save(), false)
  assert.equal(f.view().writeState, 'unknown')
  assert.equal(f.view().dirty, true)
  await assert.rejects(f.session.save())
  assert.equal(f.sent.length, 1)
  f.transport.read = async () => {
    throw new Error('Synthetic read failure')
  }
  assert.equal(await f.session.refresh(), false)
  assert.equal(f.view().writeState, 'unknown')
  f.transport.read = async () =>
    entry('kling-v1', 'price-v2', { ModelPrice: 2 })
  await f.session.refresh()
  assert.equal(f.view().writeState, 'idle')
  assert.equal(f.sent.length, 1)
})
test('409 preserves the draft and requires fresh authority; 400 can be corrected without replaying automatically', async () => {
  for (const kind of ['conflict', 'rejected'] as const) {
    const f = fixture()
    await f.session.refresh()
    f.session.replacePricing({ ModelPrice: 0 })
    f.transport.save = async (change) => {
      f.sent.push(change)
      throw new ModelPricingError(
        kind,
        'Synthetic',
        kind === 'conflict' ? 409 : 400
      )
    }
    assert.equal(await f.session.save(), false)
    assert.equal(f.view().pricing?.ModelPrice, 0)
    assert.equal(f.view().dirty, true)
    assert.equal(f.view().writeState, kind === 'conflict' ? 'conflict' : 'idle')
    assert.equal(f.sent.length, 1)
  }
})

test('suspending cancels reads but does not pretend an in-flight PATCH was canceled', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.replacePricing({ ModelPrice: 2 })
  const ack = deferred<ModelPricingCommit>()
  let reads = 0
  f.transport.read = async (name) => {
    reads++
    return entry(name, 'price-v2', { ModelPrice: 2 })
  }
  f.transport.save = () => ack.promise
  const saving = f.session.save()
  f.session.suspend()
  ack.resolve(receipt(entry('kling-v1', 'price-v2', { ModelPrice: 2 })))
  assert.equal(await saving, false)
  assert.equal(
    reads,
    0,
    'no follow-up GET under a later auth session or an unmounted editor'
  )
  assert.equal(f.view().commitKnown, true)
  assert.equal(f.view().writeState, 'unknown')
  f.session.resume()
  await f.session.refresh()
  assert.equal(f.view().writeState, 'idle')
  assert.equal(reads, 1)
})

test('incomplete numeric and JSON inputs survive refresh/model switches and block writes/previews', async () => {
  const f = fixture()
  await f.session.refresh()
  f.session.editNumber('ModelPrice', '1.')
  await f.session.refresh()
  assert.equal(f.view().inputs?.ModelPrice, '1.')
  assert.equal(f.view().dirty, true)
  await assert.rejects(f.session.save())
  assert.equal(await f.session.preview(), false)
  f.session.select('second')
  f.session.select('kling-v1')
  assert.equal(f.view().inputs?.ModelPrice, '1.')
  f.session.editNumber('ModelPrice', '1.5')
  assert.equal(f.view().pricing?.ModelPrice, 1.5)
  assert.equal(f.session.importPricing('{'), false)
  await f.session.refresh()
  assert.equal(f.view().inputs?.$json, '{')
  assert.equal(
    f.session.importPricing(
      '{"ModelPrice":0,"billing_setting.plugin_billing_expr":{"kling":"0"}}'
    ),
    true
  )
  assert.deepEqual(f.view().inputs, {})
  assert.equal(await f.session.save(), true)
  assert.deepEqual(f.sent[0].pricing, {
    ModelPrice: 0,
    'billing_setting.plugin_billing_expr': { kling: '0' },
  })
})

test('reset reads its acknowledged compiled target and retains edits made during PATCH', async () => {
  const f = fixture()
  await f.session.refresh()
  const ack = deferred<ModelPricingCommit>()
  f.transport.save = async (change) => {
    f.sent.push(change)
    return ack.promise
  }
  const saving = f.session.save(true)
  f.session.editNumber('ModelPrice', '0')
  const compiled = entry('kling-v1', 'compiled-v2', { ModelPrice: 5 })
  f.setServer(compiled)
  ack.resolve(receipt(compiled, false))
  assert.equal(await saving, true)
  assert.deepEqual(f.sent[0].pricing, {})
  assert.equal(f.sent[0].reset, true)
  assert.equal(f.view().base?.configured.ModelPrice, 5)
  assert.equal(f.view().pricing?.ModelPrice, 0)
  assert.equal(f.view().dirty, true)
  assert.equal(f.view().cachePublished, false)
})

test('lost reset receipt cannot infer compiled defaults from a read and never retries automatically', async () => {
  const f = fixture()
  await f.session.refresh()
  f.transport.save = async (change) => {
    f.sent.push(change)
    f.setServer(entry('kling-v1', 'compiled-v2', { ModelPrice: 5 }))
    throw new ModelPricingError('unknown-result', 'Lost reset receipt')
  }
  assert.equal(await f.session.save(true), false)
  await f.session.refresh()
  assert.equal(f.view().writeState, 'unknown')
  assert.equal(f.view().commitKnown, false)
  await assert.rejects(f.session.save(true))
  assert.equal(f.sent.length, 1)
  f.session.adoptRemote()
  assert.equal(f.view().pricing?.ModelPrice, 5)
})

test('batch refuses dirty targets before staging any target and sends one guarded atomic request', async () => {
  const f = fixture()
  await f.session.refresh()
  const targets = [entry('second'), entry('third')]
  f.session.select('third')
  await f.session.refresh()
  f.session.replacePricing({ ModelPrice: 3 })
  f.session.select('kling-v1')
  let calls = 0
  f.transport.saveMany = async (changes) => {
    calls++
    assert.deepEqual(
      changes.map((c) => [
        c.model_name,
        c.expected_version,
        c.expected_contract_digest,
        c.expected_binding_version,
        c.pricing,
      ]),
      [
        ['second', 'price-v1', 'contracts-v1', 'binding-v1', { ModelPrice: 0 }],
        ['third', 'price-v1', 'contracts-v1', 'binding-v1', { ModelPrice: 0 }],
      ]
    )
    return {
      committed: true,
      cache_published: true,
      entries: changes.map((c) => entry(c.model_name, 'price-v2', c.pricing)),
    }
  }
  await assert.rejects(f.session.saveBatch(targets, { ModelPrice: 0 }))
  assert.equal(calls, 0)
  f.session.select('second')
  assert.equal(
    f.view().base,
    undefined,
    'the earlier target was not partially staged'
  )
  f.session.select('third')
  f.session.discardLocal()
  f.session.select('kling-v1')
  f.transport.read = async (name) => entry(name, 'price-v2', { ModelPrice: 0 })
  assert.equal(await f.session.saveBatch(targets, { ModelPrice: 0 }), true)
  assert.equal(calls, 1)
})

test('rejected batches leave unopened targets clean and can retry after required reconciliation', async () => {
  for (const kind of ['rejected', 'conflict'] as const) {
    const f = fixture()
    await f.session.refresh()
    f.session.replacePricing({ ModelPrice: 0 })
    const targets = [
      entry('second', 'price-v1', { ModelPrice: 7 }),
      entry('third', 'price-v1', { ModelPrice: 9 }),
    ]
    let calls = 0
    f.transport.saveMany = async (changes) => {
      calls++
      assert.deepEqual(
        changes.map((c) => c.pricing),
        [{ ModelPrice: 0 }, { ModelPrice: 0 }]
      )
      throw new ModelPricingError(
        kind,
        'Synthetic batch rejection',
        kind === 'conflict' ? 409 : 400
      )
    }
    assert.equal(await f.session.saveBatch(targets, { ModelPrice: 0 }), false)
    for (const target of targets) {
      f.session.select(target.model_name)
      assert.deepEqual(f.view().pricing, target.configured)
      assert.equal(
        f.view().dirty,
        false,
        'a rejected system-staged copy is not an operator draft'
      )
    }
    if (kind === 'conflict') {
      f.transport.read = async (name) =>
        structuredClone(targets.find((t) => t.model_name === name)!)
      assert.equal(await f.session.reconcileModels(['second', 'third']), true)
    }
    assert.equal(await f.session.saveBatch(targets, { ModelPrice: 0 }), false)
    assert.equal(calls, 2)
    f.session.select('kling-v1')
    assert.equal(f.view().pricing?.ModelPrice, 0)
    assert.equal(
      f.view().dirty,
      true,
      'the operator source draft remains owned'
    )
  }
})

test('batch ambiguous writes reconcile independently and retain a blocked old target', async () => {
  const f = fixture()
  await f.session.refresh()
  const targets = [entry('second'), entry('third')]
  let calls = 0
  f.transport.saveMany = async () => {
    calls++
    throw new ModelPricingError('unknown-result', 'Lost batch receipt')
  }
  f.transport.read = async (name) =>
    name === 'second' ? entry(name, 'new', { ModelPrice: 0 }) : entry(name)
  assert.equal(await f.session.saveBatch(targets, { ModelPrice: 0 }), false)
  assert.deepEqual(f.session.getSnapshot().blockedModels, ['third'])
  f.session.select('third')
  assert.equal(f.view().writeState, 'unknown')
  await assert.rejects(f.session.saveBatch(targets, { ModelPrice: 0 }))
  assert.equal(calls, 1)
  f.transport.read = async (name) => entry(name, 'new', { ModelPrice: 0 })
  assert.equal(await f.session.reconcileModels(['third']), true)
  assert.deepEqual(f.session.getSnapshot().blockedModels, [])
})

test('batch targets reject pricing edits while pending or unknown, then use acknowledged copied prices', async () => {
  for (const size of [1, 2]) {
    const f = fixture()
    await f.session.refresh()
    const targets = [
      entry('second', 'old', { ModelPrice: 7 }),
      entry('third', 'old', { ModelPrice: 9 }),
    ].slice(0, size)
    const pending = deferred<ModelPricingCommit>()
    f.transport.save = () => pending.promise
    f.transport.saveMany = () => pending.promise
    f.transport.read = async (name) =>
      structuredClone(targets.find((t) => t.model_name === name)!)
    const saving = f.session.saveBatch(targets, { ModelPrice: 0 })
    f.session.select('second')
    assert.throws(
      () => f.session.editNumber('ModelRatio', '2'),
      /Reconcile batch/
    )
    pending.reject(new ModelPricingError('unknown-result', 'Lost receipt'))
    assert.equal(await saving, false)
    assert.equal(f.view().writeState, 'unknown')
    assert.throws(
      () => f.session.importPricing('{"ModelPrice":7}'),
      /Reconcile batch/
    )
    assert.throws(
      () => f.session.replacePricing({ ModelPrice: 7 }),
      /Reconcile batch/
    )
    f.transport.read = async (name) =>
      entry(name, 'committed', { ModelPrice: 0 })
    assert.equal(
      await f.session.reconcileModels(targets.map((t) => t.model_name)),
      true
    )
    f.session.editNumber('ModelRatio', '2')
    assert.deepEqual(
      f.view().pricing,
      { ModelPrice: 0, ModelRatio: 2 },
      'later edits cannot carry the old target price into a new write'
    )
  }
})
