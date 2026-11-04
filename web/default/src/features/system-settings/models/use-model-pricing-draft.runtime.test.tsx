// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { act, StrictMode, useEffect } from 'react'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import {
  entry,
  quoted,
  receipt,
  deferred,
} from './__tests__/model-pricing-fixture'
import type { ModelPricingTransport } from './model-pricing-api'
import type { ModelPricingEntry } from './model-pricing-types'
import { useModelPricingDraft } from './use-model-pricing-draft'

const env = createReactTestEnvironment()
await env.setup()
const { render, cleanup, waitFor } = await import('@testing-library/react/pure')
const previousUser = useAuthStore.getState().auth.user
afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.setUser(previousUser)
})
after(() => env.teardown())
let latest: ReturnType<typeof useModelPricingDraft>
function Harness(props: {
  model: string
  transport: ModelPricingTransport
  active?: boolean
}) {
  const current = useModelPricingDraft(
    props.model,
    props.transport,
    props.active
  )
  useEffect(() => {
    latest = current
  }, [current])
  return <output>{current.document?.pricing?.ModelPrice ?? 'empty'}</output>
}
function transport(): ModelPricingTransport {
  return {
    read: async (name) => entry(name),
    save: async (change) =>
      receipt(entry(change.model_name, 'price-v2', change.pricing)),
    preview: async (name, pricing) => ({
      ...entry(name, 'price-v1', pricing),
      draft_version: 'draft',
    }),
    quote: async (input) => quoted(input),
  }
}
test('React adapter keeps drafts across model switches and tolerates StrictMode setup/cleanup', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 9910001, username: 'synthetic-root', role: 100 })
  const client = transport()
  const view = render(
    <StrictMode>
      <Harness model='kling-v1' transport={client} />
    </StrictMode>
  )
  await waitFor(() => assert.equal(latest.document?.pricing?.ModelPrice, 7))
  await act(async () =>
    latest.session.replacePricing({
      ...latest.document!.pricing!,
      ModelPrice: 0,
    })
  )
  assert.equal(view.container.textContent, '0')
  view.rerender(
    <StrictMode>
      <Harness model='second' transport={client} />
    </StrictMode>
  )
  await waitFor(() => assert.equal(latest.document?.base?.model_name, 'second'))
  view.rerender(
    <StrictMode>
      <Harness model='kling-v1' transport={client} />
    </StrictMode>
  )
  await waitFor(() => assert.equal(latest.document?.pricing?.ModelPrice, 0))
  assert.equal(latest.document?.dirty, true)
})
test('non-Root has no read and auth revision prevents old responses or drafts leaking into a new session', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 9910002, username: 'synthetic-admin', role: 10 })
  const client = transport()
  let reads = 0
  const old = deferred<ModelPricingEntry>()
  client.read = () => {
    reads++
    return old.promise
  }
  const view = render(<Harness model='kling-v1' transport={client} />)
  assert.equal(reads, 0)
  assert.equal(latest.canEdit, false)
  await act(async () =>
    useAuthStore
      .getState()
      .auth.setUser({ id: 9910001, username: 'synthetic-root', role: 100 })
  )
  await waitFor(() => assert.equal(reads, 1))
  client.read = async (name) => {
    reads++
    return entry(name, 'new-auth', { ModelPrice: 3 })
  }
  await act(async () =>
    useAuthStore.getState().auth.setUser({
      id: 9910003,
      username: 'synthetic-root-other',
      role: 100,
    })
  )
  await waitFor(() => assert.equal(latest.document?.pricing?.ModelPrice, 3))
  await act(async () =>
    old.resolve(entry('kling-v1', 'old-auth', { ModelPrice: 999 }))
  )
  assert.equal(view.container.textContent, '3')
  assert.equal(latest.document?.base?.version, 'new-auth')
  await act(async () => useAuthStore.getState().auth.reset())
  assert.equal(latest.document, undefined)
})

test('returning from legacy tools rereads authority without losing a dirty draft', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 9910001, username: 'synthetic-root', role: 100 })
  const client = transport()
  let server = entry()
  let reads = 0
  client.read = async () => {
    reads++
    return structuredClone(server)
  }
  const view = render(<Harness model='kling-v1' transport={client} active />)
  await waitFor(() => assert.equal(latest.document?.pricing?.ModelPrice, 7))
  await act(async () => latest.session.editNumber('ModelPrice', '0'))
  view.rerender(<Harness model='kling-v1' transport={client} active={false} />)
  const before = reads
  server = entry('kling-v1', 'changed-by-legacy', { ModelPrice: 3 })
  assert.equal(reads, before)
  view.rerender(<Harness model='kling-v1' transport={client} active />)
  await waitFor(() => assert.equal(latest.document?.writeState, 'conflict'))
  assert.equal(reads, before + 1)
  assert.equal(latest.document?.pricing?.ModelPrice, 0)
  assert.equal(latest.document?.remote?.configured.ModelPrice, 3)
})
