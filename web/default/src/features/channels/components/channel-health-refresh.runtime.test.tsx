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
import { act, useState, type ReactElement } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { api } from '@/lib/api'
import type { ChannelRoutingHealth } from '../lib/channel-routing-health'
import { channelSchema, type Channel } from '../types'

const env = createReactTestEnvironment()
await env.setup()
const globals = new Map<string, PropertyDescriptor | undefined>()
for (const [key, value] of Object.entries({
  localStorage: env.dom.window.localStorage,
  matchMedia: env.dom.window.matchMedia.bind(env.dom.window),
  customElements: env.dom.window.customElements,
  scrollTo: () => {},
})) {
  globals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
  Object.defineProperty(globalThis, key, { configurable: true, value })
}
Object.defineProperty(env.dom.window.Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})
Object.defineProperty(env.dom.window.HTMLElement.prototype, 'scrollIntoView', {
  configurable: true,
  value: () => {},
})
Object.defineProperty(env.dom.window.document, 'visibilityState', {
  configurable: true,
  value: 'visible',
})
const { within, waitFor, fireEvent } =
  await import('@testing-library/react/pure')
const { ChannelRoutingHealthCard } =
  await import('./channel-routing-health-card')
const { ChannelRoutingHealthCell } = await import('./channel-routing-health')
const { ChannelHealthSnapshotContext } =
  await import('./channel-health-snapshot-context')
const { ChannelMutateDrawer } = await import('./drawers/channel-mutate-drawer')
const { ChannelsProvider, useChannels } = await import('./channels-provider')
const { ChannelsTable } = await import('./channels-table')
const { useChannelHealthRefresh, CHANNEL_HEALTH_REFRESH_MS } =
  await import('../hooks/use-channel-health-refresh')
const { channelsQueryKeys, invalidateChannelConfiguration } =
  await import('../lib/channel-actions')
const { getChannel } = await import('../api')

after(() => {
  env.teardown()
  for (const [key, descriptor] of globals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
})

function health(
  overrides: Partial<ChannelRoutingHealth> = {}
): ChannelRoutingHealth {
  return {
    schema_version: 1,
    status: 'penalized',
    source: 'redis',
    base_priority: 10,
    effective_priority: 9,
    stored_penalty: 1,
    active_penalty: 1,
    stored_runtime_disabled: false,
    runtime_disabled: false,
    penalty_until: null,
    disabled_until: null,
    penalty_recovery_mode: 'manual',
    disable_recovery_mode: 'probe_then_automatic',
    last_observed_at: null,
    evaluated_at: new Date(Date.now()).toISOString(),
    unavailable_reason: null,
    ...overrides,
  }
}
function channel(
  id: number,
  snapshot: unknown = health(),
  priority = 10
): Channel {
  return channelSchema.parse({
    id,
    priority,
    routing_health: snapshot,
    type: 1,
    key: '',
    status: 1,
    name: `Synthetic channel ${id}`,
    models: 'synthetic-model',
    group: 'default',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
}
function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
}
function observation(requestOrder = 0) {
  return { requestOrder, receivedAt: performance.now() }
}
async function renderPage(content: ReactElement, queryClient: QueryClient) {
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const page = createRoute({
    getParentRoute: () => auth,
    path: 'channels/',
    component: () => content,
    validateSearch: (search) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([page])]),
    history: createMemoryHistory({ initialEntries: ['/channels/'] }),
  })
  await router.load()
  const view = await env.render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <RouterProvider router={router} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  return { ...view, router }
}

test(
  'runtime polling updates the real editor card while draft and detail invalidations remain isolated',
  // This exercises the complete drawer. Full form interactions can exceed 20s
  // on Windows; keep the budget local to this test.
  { timeout: 60_000 },
  async () => {
    const queryClient = client(),
      adapter = api.defaults.adapter
    let savedPriority = 10,
      penalty = 1,
      reads = 0
    const writes: Record<string, unknown>[] = []
    api.defaults.adapter = async (config) => {
      let data: unknown = []
      if (config.method === 'put') writes.push(JSON.parse(String(config.data)))
      if (config.url === '/api/channel/7') {
        reads += 1
        data = channel(
          7,
          health({
            base_priority: savedPriority,
            effective_priority: savedPriority - penalty,
            active_penalty: penalty,
            stored_penalty: penalty,
          }),
          savedPriority
        )
      }
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: { success: true, data },
      }
    }
    function Editor() {
      const [open, setOpen] = useState(true)
      return (
        <ChannelMutateDrawer
          open={open}
          onOpenChange={setOpen}
          currentRow={channel(7)}
        />
      )
    }
    const view = await renderPage(<Editor />, queryClient)
    const body = within(env.dom.window.document.body)
    try {
      await waitFor(() =>
        assert.ok(body.queryByLabelText('Configured priority'))
      )
      const input = body.getByLabelText(
        'Configured priority'
      ) as HTMLInputElement
      await waitFor(() => assert.equal(input.value, '10'))
      const card = within(body.getByLabelText('Routing state snapshot'))
      assert.equal(
        reads,
        1,
        'fresh details seed the first runtime snapshot without a duplicate GET'
      )
      await act(async () =>
        fireEvent.change(input, { target: { value: '20' } })
      )
      penalty = 2
      await view.click(
        card.getByRole('button', { name: 'Refresh runtime state' })
      )
      await waitFor(() => assert.ok(card.queryByText('Timeout penalty: 2')))
      assert.equal(input.value, '20')
      savedPriority = 11
      await act(async () => {
        await invalidateChannelConfiguration(queryClient, 7)
      })
      await waitFor(() =>
        assert.ok(card.queryByText('Configured priority: 11'))
      )
      assert.equal(
        input.value,
        '20',
        'even detail-cache invalidation preserves the draft'
      )
      const cached = queryClient.getQueryData<Record<string, unknown>>(
        channelsQueryKeys.runtime(7)
      )!
      assert.deepEqual(Object.keys(cached).sort(), [
        'id',
        'observation',
        'priority',
        'routing_health',
      ])
      await view.click(body.getByText('Update Channel', { selector: 'button' }))
      await waitFor(() => assert.equal(writes.length, 1))
      assert.equal(writes[0].priority, 20)
      assert.equal('routing_health' in writes[0], false)
      assert.equal('observation' in writes[0], false)
    } finally {
      await view.unmount()
      queryClient.clear()
      api.defaults.adapter = adapter
    }
  }
)

test(
  'P44 mixed-version refreshes replace incompatible snapshots without resetting or submitting the draft',
  { timeout: 60_000 },
  async () => {
    const queryClient = client()
    const adapter = api.defaults.adapter
    let snapshot: unknown = undefined
    let configuredPriority = 10
    let failure = false
    const writes: Record<string, unknown>[] = []
    api.defaults.adapter = async (config) => {
      if (config.method === 'put') {
        writes.push(JSON.parse(String(config.data)))
      }
      const data = channel(7, snapshot, configuredPriority)
      // The helper has a default health argument. Delete the property so this
      // really exercises an old server's absent field, rather than a null value.
      if (snapshot === undefined) delete data.routing_health
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data:
          config.url === '/api/channel/7' && failure
            ? { success: false, message: 'synthetic-p44-read-failure' }
            : {
                success: true,
                data: config.url === '/api/channel/7' ? data : [],
              },
      }
    }
    function Editor() {
      const [open, setOpen] = useState(true)
      return (
        <ChannelMutateDrawer
          open={open}
          onOpenChange={setOpen}
          currentRow={channel(7)}
        />
      )
    }
    const view = await renderPage(<Editor />, queryClient)
    const body = within(document.body)
    try {
      await waitFor(() =>
        assert.ok(body.queryByLabelText('Configured priority'))
      )
      const input = body.getByLabelText(
        'Configured priority'
      ) as HTMLInputElement
      await waitFor(() => assert.equal(input.value, '10'))
      const card = within(body.getByLabelText('Routing state snapshot'))
      const missing = 'Runtime state display is not supported by this server.'
      await waitFor(() => assert.ok(card.queryByText(missing)))
      assert.equal(card.queryByText('No runtime restriction'), null)
      await act(async () =>
        fireEvent.change(input, { target: { value: '20' } })
      )
      const refresh = async (expected: string) => {
        const button = card.getByRole('button', {
          name: 'Refresh runtime state',
        }) as HTMLButtonElement
        await waitFor(() => assert.equal(button.disabled, false))
        await view.click(button)
        await waitFor(() =>
          assert.equal(
            queryClient.getQueryData<{ routing_health?: unknown }>(
              channelsQueryKeys.runtime(7)
            )!.routing_health,
            snapshot
          )
        )
        await waitFor(() => assert.ok(card.queryByText(expected)))
        assert.equal(
          input.value,
          '20',
          'version changes must preserve the draft'
        )
      }
      snapshot = { ...health(), schema_version: 2 }
      await refresh('Unsupported runtime state version')
      assert.equal(card.queryByText(missing), null)
      snapshot = { ...health(), effective_priority: 'broken' }
      await refresh('Runtime state unavailable')
      assert.equal(card.queryByText('Unsupported runtime state version'), null)
      snapshot = health({
        status: 'unknown',
        source: 'unavailable',
        effective_priority: null,
        active_penalty: null,
        stored_penalty: null,
        stored_runtime_disabled: null,
        runtime_disabled: null,
        penalty_recovery_mode: null,
        unavailable_reason: 'redis_read_failed',
      })
      await refresh('Runtime state unavailable')
      assert.ok(card.getByText('—', { exact: true }))
      assert.equal(card.queryByText('0', { exact: true }), null)
      configuredPriority = 0
      snapshot = health({
        status: 'normal',
        base_priority: 0,
        effective_priority: 0,
        stored_penalty: 0,
        active_penalty: 0,
        penalty_recovery_mode: null,
      })
      await refresh('No runtime restriction')
      assert.ok(card.getByText('0', { exact: true }))
      assert.equal(card.queryByText('Runtime state unavailable'), null)
      failure = true
      await refresh(
        'Runtime refresh failed. The previous snapshot may be outdated.'
      )
      assert.ok(
        card.getByText('0', { exact: true }),
        'failed reads retain zero'
      )
      failure = false
      snapshot = undefined
      configuredPriority = 10
      await refresh(missing)
      assert.equal(card.queryByText('0', { exact: true }), null)
      assert.equal(card.queryByText('No runtime restriction'), null)
      assert.equal(
        card.queryByText(
          'Runtime refresh failed. The previous snapshot may be outdated.'
        ),
        null
      )
      snapshot = health({
        effective_priority: 8,
        active_penalty: 2,
        stored_penalty: 2,
      })
      await refresh('Timeout penalty: 2')
      assert.ok(card.getByText('8', { exact: true }))
      assert.equal(card.queryByText(missing), null)
      await view.click(body.getByText('Update Channel', { selector: 'button' }))
      await waitFor(() => assert.equal(writes.length, 1))
      assert.equal(writes[0].priority, 20)
      for (const field of [
        'routing_health',
        'effective_priority',
        'active_penalty',
        'observation',
      ]) {
        assert.equal(Object.hasOwn(writes[0], field), false)
      }
    } finally {
      await view.unmount()
      queryClient.clear()
      api.defaults.adapter = adapter
    }
  }
)

test(
  'cached configuration stays blocked until a successful read and new channels have no runtime card',
  { timeout: 20_000 },
  async () => {
    const queryClient = client(),
      adapter = api.defaults.adapter
    queryClient.setQueryData(
      channelsQueryKeys.detail(7),
      { success: true, data: channel(7) },
      { updatedAt: Date.now() - 60_000 }
    )
    let failure = true
    api.defaults.adapter = async (config) => {
      const isDetails = config.url === '/api/channel/7'
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data:
          isDetails && failure
            ? { success: false }
            : {
                success: true,
                data: isDetails
                  ? channel(
                      7,
                      health({ base_priority: 11, effective_priority: 10 }),
                      11
                    )
                  : [],
              },
      }
    }
    function Editor() {
      const [row, setRow] = useState<Channel | null>(channel(7))
      return (
        <>
          <button onClick={() => setRow(null)}>Create another channel</button>
          <ChannelMutateDrawer open onOpenChange={() => {}} currentRow={row} />
        </>
      )
    }
    const view = await renderPage(<Editor />, queryClient),
      body = within(document.body)
    try {
      await waitFor(() =>
        assert.ok(
          body.queryByText(
            'Unable to load channel configuration. Retry before editing.'
          )
        )
      )
      assert.equal(body.queryByLabelText('Configured priority'), null)
      assert.equal(
        (
          body.getByRole('button', {
            name: 'Update Channel',
          }) as HTMLButtonElement
        ).disabled,
        true
      )
      failure = false
      await view.click(body.getByRole('button', { name: 'Retry' }))
      await waitFor(() =>
        assert.equal(
          (body.getByLabelText('Configured priority') as HTMLInputElement)
            .value,
          '11'
        )
      )
      await view.click(
        within(view.container).getByText('Create another channel')
      )
      await waitFor(() =>
        assert.equal(
          body.queryByRole('region', { name: 'Routing state snapshot' }),
          null
        )
      )
    } finally {
      await view.unmount()
      queryClient.clear()
      api.defaults.adapter = adapter
    }
  }
)

test('failed runtime refresh preserves the old value and a later retry obtains a new snapshot', async () => {
  const queryClient = client(),
    adapter = api.defaults.adapter
  let failure = true
  api.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: 'OK',
    headers: {},
    data: failure
      ? { success: false }
      : {
          success: true,
          data: channel(
            7,
            health({
              status: 'normal',
              effective_priority: 10,
              active_penalty: 0,
              stored_penalty: 0,
              penalty_recovery_mode: null,
            })
          ),
        },
  })
  const view = await renderPage(
    <ChannelRoutingHealthCard
      initialChannel={channel(7)}
      initialUpdatedAt={Date.now()}
      initialObservation={observation()}
    />,
    queryClient
  )
  const fields = within(view.container)
  try {
    await view.click(
      fields.getByRole('button', { name: 'Refresh runtime state' })
    )
    await waitFor(() =>
      assert.ok(
        fields.queryByText(
          'Runtime refresh failed. The previous snapshot may be outdated.'
        )
      )
    )
    assert.ok(fields.getByText('9', { exact: true }))
    failure = false
    await view.click(
      fields.getByRole('button', { name: 'Refresh runtime state' })
    )
    await waitFor(() => assert.ok(fields.queryByText('No runtime restriction')))
    assert.equal(
      fields.queryByText(
        'Runtime refresh failed. The previous snapshot may be outdated.'
      ),
      null
    )
  } finally {
    await view.unmount()
    queryClient.clear()
    api.defaults.adapter = adapter
  }
})

test('late configuration responses cannot replace a newer runtime projection', async () => {
  const queryClient = client()
  const timestamp = Date.now()
  queryClient.setQueryData(
    channelsQueryKeys.runtime(7),
    {
      id: 7,
      priority: 20,
      routing_health: health({
        base_priority: 20,
        effective_priority: 19,
        evaluated_at: new Date(timestamp).toISOString(),
      }),
      observation: observation(1),
    },
    { updatedAt: timestamp - 100 }
  )
  const view = await renderPage(
    <ChannelRoutingHealthCard
      initialChannel={channel(
        7,
        health({ evaluated_at: new Date(timestamp - 1000).toISOString() })
      )}
      initialUpdatedAt={timestamp + 100}
      initialObservation={observation(0)}
    />,
    queryClient
  )
  try {
    assert.ok(within(view.container).getByText('Configured priority: 20'))
    assert.ok(within(view.container).getByText('19', { exact: true }))
  } finally {
    await view.unmount()
    queryClient.clear()
  }
})

test(
  'a late polling response cannot undo accepted details, but a subsequent read can update them',
  { timeout: 20_000 },
  async () => {
    const queryClient = client(),
      adapter = api.defaults.adapter
    let finishOld: (() => void) | undefined
    let reads = 0
    api.defaults.adapter = async (config) => {
      reads += 1
      const read = reads
      if (read === 1)
        await new Promise<void>((resolve) => {
          finishOld = resolve
        })
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: channel(
            7,
            health({
              effective_priority: read === 2 ? 8 : 9,
              active_penalty: read === 2 ? 2 : 1,
              stored_penalty: read === 2 ? 2 : 1,
            })
          ),
        },
      }
    }
    function Card() {
      const [details, setDetails] = useState({
        data: channel(7),
        observation: { requestOrder: 0, receivedAt: performance.now() },
      })
      return (
        <>
          <button
            onClick={async () => {
              const response = await getChannel(7)
              setDetails({
                data: response.data!,
                observation: response.observation,
              })
            }}
          >
            Receive newer details
          </button>
          <ChannelRoutingHealthCard
            initialChannel={details.data}
            initialUpdatedAt={Date.now()}
            initialObservation={details.observation}
          />
        </>
      )
    }
    const view = await renderPage(<Card />, queryClient),
      fields = within(view.container)
    try {
      await view.click(
        fields.getByRole('button', { name: 'Refresh runtime state' })
      )
      await waitFor(() => assert.ok(finishOld))
      await view.click(fields.getByText('Receive newer details'))
      await waitFor(() => assert.ok(fields.queryByText('8', { exact: true })))
      const accepted = queryClient.getQueryData<{ observation: unknown }>(
        channelsQueryKeys.runtime(7)
      )!.observation
      await act(async () => {
        finishOld!()
      })
      await waitFor(() => assert.equal(queryClient.isFetching(), 0))
      assert.ok(
        fields.getByText('8', { exact: true }),
        'old request must not restore 9'
      )
      assert.equal(fields.queryByText('9', { exact: true }), null)
      assert.deepEqual(
        queryClient.getQueryData<{ observation: unknown }>(
          channelsQueryKeys.runtime(7)
        )!.observation,
        accepted,
        'a rejected response must not reset observation age'
      )
      await view.click(
        fields.getByRole('button', { name: 'Refresh runtime state' })
      )
      await waitFor(() => assert.ok(fields.queryByText('9', { exact: true })))
    } finally {
      finishOld?.()
      await view.unmount()
      queryClient.clear()
      api.defaults.adapter = adapter
    }
  }
)

test(
  'node clocks in either direction do not override local request order',
  { timeout: 20_000 },
  async () => {
    for (const skew of [-120_000, 120_000]) {
      const queryClient = client(),
        timestamp = Date.now()
      queryClient.setQueryData(channelsQueryKeys.runtime(7), {
        id: 7,
        priority: 10,
        routing_health: health({
          evaluated_at: new Date(timestamp + skew).toISOString(),
        }),
        observation: { requestOrder: 0, receivedAt: performance.now() },
      })
      const view = await renderPage(
        <ChannelRoutingHealthCard
          initialChannel={channel(
            7,
            health({
              effective_priority: 8,
              active_penalty: 2,
              stored_penalty: 2,
              evaluated_at: new Date(timestamp).toISOString(),
            })
          )}
          initialUpdatedAt={timestamp}
          initialObservation={{
            requestOrder: 1,
            receivedAt: performance.now(),
          }}
        />,
        queryClient
      )
      try {
        assert.equal(
          queryClient.getQueryData<{ routing_health: ChannelRoutingHealth }>(
            channelsQueryKeys.runtime(7)
          )!.routing_health.effective_priority,
          8
        )
        // Cache notifications are batched; wait for their visible result.
        await waitFor(() =>
          assert.ok(within(view.container).queryByText('8', { exact: true }))
        )
        assert.equal(
          within(view.container).queryByText('9', { exact: true }),
          null
        )
      } finally {
        await view.unmount()
        queryClient.clear()
      }
    }
  }
)

test('snapshot age and recovery hints use elapsed observation time despite node clock skew', async () => {
  for (const skew of [-120_000, 120_000]) {
    const evaluatedAt = Date.now() + skew
    const source = channel(
      7,
      health({
        penalty_recovery_mode: 'automatic',
        evaluated_at: new Date(evaluatedAt).toISOString(),
        penalty_until: new Date(evaluatedAt + 30_000).toISOString(),
      })
    )
    function Snapshot() {
      const [elapsed, setElapsed] = useState(0)
      return (
        <>
          <button onClick={() => setElapsed(61_000)}>
            Advance elapsed time
          </button>
          <ChannelHealthSnapshotContext
            value={{
              now: 1000 + elapsed,
              receivedAt: 1000,
              placeholder: false,
            }}
          >
            <ChannelRoutingHealthCell channel={source} />
          </ChannelHealthSnapshotContext>
        </>
      )
    }
    const view = await env.render(<Snapshot />),
      fields = within(view.container)
    try {
      assert.equal(
        fields.queryByText('Runtime snapshot may be outdated.'),
        null
      )
      assert.equal(
        fields.queryByText('Waiting for a fresh runtime snapshot'),
        null
      )
      await view.click(fields.getByText('Advance elapsed time'))
      assert.ok(fields.getByText('Runtime snapshot may be outdated.'))
      assert.ok(fields.getByText('Waiting for a fresh runtime snapshot'))
      assert.ok(
        fields.getByText('9', { exact: true }),
        'elapsed time must not restore priority'
      )
    } finally {
      await view.unmount()
    }
  }
})

test('deadline scheduling uses server-relative duration and ignores early timer callbacks for either node clock skew', async () => {
  const originalNow = Object.getOwnPropertyDescriptor(performance, 'now')
  const originalTimeout = globalThis.setTimeout
  const delays: number[] = []
  const timers: Array<{ callback: () => void; delay: number }> = []
  let timestamp = 1000
  Object.defineProperty(performance, 'now', {
    configurable: true,
    value: () => timestamp,
  })
  globalThis.setTimeout = ((callback: () => void, delay?: number) => {
    delays.push(delay ?? 0)
    if (delay === 30_000 || delay === 1) {
      timers.push({ callback, delay })
      // Drive deadline callbacks explicitly; React's zero-delay tasks still run.
      return originalTimeout(() => {}, 120_000)
    }
    return originalTimeout(callback, delay)
  }) as typeof setTimeout
  try {
    for (const skew of [-120_000, 120_000]) {
      delays.length = 0
      timers.length = 0
      timestamp = 1000
      const evaluatedAt = Date.now() + skew
      const snapshots = [
        channel(
          7,
          health({
            penalty_recovery_mode: 'automatic',
            evaluated_at: new Date(evaluatedAt).toISOString(),
            penalty_until: new Date(evaluatedAt + 30_000).toISOString(),
          })
        ),
      ]
      let calls = 0
      const refetch = async () => {
        calls += 1
      }
      function Clock() {
        useChannelHealthRefresh({ snapshots, receivedAt: 1000, refetch })
        return null
      }
      const view = await env.render(<Clock />)
      try {
        assert.ok(
          delays.includes(30_000),
          '30 seconds on the server must schedule 30 seconds locally'
        )
        assert.equal(
          calls,
          0,
          'a slow server clock must not trigger immediate expiry'
        )
        timestamp = 30_999.5
        await act(async () => timers[0].callback())
        assert.equal(
          calls,
          0,
          'an early timer cannot request expiry confirmation'
        )
        assert.equal(
          timers.at(-1)!.delay,
          1,
          'remaining fractional milliseconds round up'
        )
        timestamp = 31_000
        await act(async () => timers.at(-1)!.callback())
        assert.equal(calls, 1, 'a reached deadline requests one confirmation')
      } finally {
        await view.unmount()
      }
    }
  } finally {
    globalThis.setTimeout = originalTimeout
    if (originalNow) Object.defineProperty(performance, 'now', originalNow)
    else Reflect.deleteProperty(performance, 'now')
  }
})

test(
  'switching channels cancels the previous runtime request and closing stops foreground refresh',
  { timeout: 20_000 },
  async () => {
    const queryClient = client(),
      adapter = api.defaults.adapter
    let completeOld: (() => void) | undefined
    let previousSignal: { readonly aborted: boolean } | undefined
    let reads = 0
    api.defaults.adapter = async (config) => {
      reads += 1
      previousSignal = config.signal as AbortSignal
      await new Promise<void>((resolve) => {
        completeOld = resolve
      })
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: channel(7, health({ effective_priority: -77 })),
        },
      }
    }
    function Cards() {
      const [id, setId] = useState(7),
        [open, setOpen] = useState(true)
      return (
        <>
          <button onClick={() => setId(8)}>Next channel</button>
          <button onClick={() => setOpen(false)}>Close card</button>
          {open && (
            <ChannelRoutingHealthCard
              key={id}
              initialChannel={channel(
                id,
                health({ base_priority: id + 3, effective_priority: id + 2 })
              )}
              initialUpdatedAt={Date.now()}
              initialObservation={observation()}
            />
          )}
        </>
      )
    }
    const view = await renderPage(<Cards />, queryClient),
      fields = within(view.container)
    try {
      await act(async () => {
        void queryClient.refetchQueries({
          queryKey: channelsQueryKeys.runtime(7),
        })
      })
      await waitFor(() => assert.ok(completeOld))
      await view.click(fields.getByText('Next channel'))
      assert.equal(previousSignal?.aborted, true)
      await act(async () => {
        completeOld!()
      })
      assert.ok(fields.getByText('Configured priority: 11'))
      assert.equal(fields.queryByText('-77', { exact: true }), null)
      await view.click(fields.getByText('Close card'))
      await act(async () => {
        window.dispatchEvent(new Event('focus'))
      })
      assert.equal(reads, 1)
      assert.equal(
        fields.queryByRole('region', { name: 'Routing state snapshot' }),
        null
      )
    } finally {
      completeOld?.()
      await view.unmount()
      queryClient.clear()
      api.defaults.adapter = adapter
    }
  }
)

test('visible polling pauses for background and editing, resumes on focus, and stops after unmount', async () => {
  const originalInterval = globalThis.setInterval,
    originalNow = Object.getOwnPropertyDescriptor(performance, 'now')
  const callbacks: Array<() => void> = []
  let timestamp = performance.now(),
    calls = 0
  Object.defineProperty(performance, 'now', {
    configurable: true,
    value: () => timestamp,
  })
  globalThis.setInterval = ((callback: () => void, delay?: number) => {
    if (delay === CHANNEL_HEALTH_REFRESH_MS) callbacks.push(callback)
    return originalInterval(callback, delay)
  }) as typeof setInterval
  const refetch = async () => {
    calls += 1
  }
  const snapshots = [channel(7)]
  function Clock() {
    const [paused, setPaused] = useState(false)
    useChannelHealthRefresh({ snapshots, paused, refetch })
    return (
      <button onClick={() => setPaused((value) => !value)}>
        Toggle editing
      </button>
    )
  }
  const view = await env.render(<Clock />)
  const visible = (value: string) =>
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      value,
    })
  try {
    await act(async () => {
      timestamp += 30_000
      callbacks.at(-1)!()
    })
    assert.equal(calls, 1)
    visible('hidden')
    await act(async () => {
      timestamp += 30_000
      callbacks.at(-1)!()
      document.dispatchEvent(new Event('visibilitychange'))
    })
    assert.equal(calls, 1)
    visible('visible')
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
      document.dispatchEvent(new Event('visibilitychange'))
    })
    assert.equal(calls, 2, 'one foreground transition is deduplicated')
    await view.click(within(view.container).getByText('Toggle editing'))
    await act(async () => {
      timestamp += 30_000
      window.dispatchEvent(new Event('focus'))
    })
    assert.equal(calls, 2)
    await view.click(within(view.container).getByText('Toggle editing'))
    assert.equal(calls, 3)
    await view.unmount()
    await act(async () => {
      timestamp += 30_000
      window.dispatchEvent(new Event('focus'))
    })
    assert.equal(calls, 3)
  } finally {
    await view.unmount()
    visible('visible')
    globalThis.setInterval = originalInterval
    if (originalNow) Object.defineProperty(performance, 'now', originalNow)
    else Reflect.deleteProperty(performance, 'now')
  }
})

test('a deadline triggers one refresh per snapshot and retains the penalized value until server confirmation', async () => {
  let calls = 0
  const receivedAt = performance.now()
  const snapshots = [
    channel(
      7,
      health({
        penalty_recovery_mode: 'automatic',
        penalty_until: new Date(Date.now() + 200).toISOString(),
      })
    ),
  ]
  const refetch = async () => {
    calls += 1
    throw new Error('synthetic read failure')
  }
  function Deadline() {
    const now = useChannelHealthRefresh({ snapshots, receivedAt, refetch })
    return (
      <ChannelHealthSnapshotContext
        value={{ now, receivedAt, placeholder: false }}
      >
        <ChannelRoutingHealthCell channel={snapshots[0]} />
      </ChannelHealthSnapshotContext>
    )
  }
  const view = await env.render(<Deadline />),
    fields = within(view.container)
  try {
    await act(async () => window.dispatchEvent(new Event('focus')))
    assert.equal(calls, 1)
    await waitFor(() => assert.equal(calls, 2))
    assert.ok(fields.getByText('Waiting for a fresh runtime snapshot'))
    assert.ok(fields.getByText('9', { exact: true }))
    await new Promise((resolve) => setTimeout(resolve, 30))
    assert.equal(
      calls,
      2,
      'foreground deduplication must not suppress the later deadline refresh'
    )
  } finally {
    await view.unmount()
  }
})

test('list and search share refresh protection, hide placeholder health, and retain failed-refresh data', async () => {
  const queryClient = client(),
    adapter = api.defaults.adapter
  let reads = 0,
    fail = false
  let finishSearch: (() => void) | undefined
  api.defaults.adapter = async (config) => {
    let data: unknown = []
    if (config.url === '/api/channel' || config.url === '/api/channel/search') {
      reads += 1
      if (config.url.endsWith('/search'))
        await new Promise<void>((resolve) => {
          finishSearch = resolve
        })
      data = { items: [channel(7)], total: 1, type_counts: { '1': 1 } }
      if (fail)
        return {
          config,
          status: 200,
          statusText: 'OK',
          headers: {},
          data: { success: false },
        }
    }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data },
    }
  }
  function List() {
    const { refreshPaused } = useChannels()
    return (
      <>
        <p>{refreshPaused ? 'Refresh paused' : 'Refresh active'}</p>
        <ChannelsTable />
      </>
    )
  }
  const view = await renderPage(<List />, queryClient),
    fields = within(view.container)
  try {
    await waitFor(() => assert.ok(fields.queryByText('Timeout penalty: 1')))
    await view.click(fields.getByRole('button', { name: '10' }))
    await waitFor(() => assert.ok(fields.queryByText('Refresh paused')))
    await act(async () => {
      await queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.lists(),
      })
    })
    assert.equal(reads, 1)
    const input = view.container.querySelector(
      'input.font-mono'
    ) as HTMLInputElement
    await act(async () => {
      fireEvent.change(input, { target: { value: '20' } })
      fireEvent.keyDown(input, { key: 'Escape' })
    })
    await waitFor(() => assert.ok(fields.queryByText('Refresh active')))
    fail = true
    await act(async () => {
      await queryClient.refetchQueries({ queryKey: channelsQueryKeys.lists() })
    })
    await waitFor(() =>
      assert.ok(
        fields.queryByText(
          'Channel refresh failed. Displayed results may be outdated.'
        )
      )
    )
    assert.ok(fields.getByText('Timeout penalty: 1'))
    fail = false
    await act(async () => {
      void view.router.navigate({
        to: '/channels',
        search: { filter: 'synthetic' },
      })
    })
    await waitFor(() => assert.ok(finishSearch))
    assert.equal(fields.queryByText('Timeout penalty: 1'), null)
    assert.ok(
      fields.getAllByText(
        'Waiting for the current results. Previous runtime values are hidden.'
      ).length > 0
    )
    await act(async () => {
      finishSearch!()
    })
    await waitFor(() => assert.ok(fields.queryByText('Timeout penalty: 1')))
  } finally {
    finishSearch?.()
    await view.unmount()
    queryClient.clear()
    api.defaults.adapter = adapter
  }
})

test(
  'tag priority confirmation keeps its pending value and pauses refresh until cancellation',
  { timeout: 20_000 },
  async () => {
    const queryClient = client(),
      adapter = api.defaults.adapter
    const previousTagMode = localStorage.getItem('enable-tag-mode')
    localStorage.setItem('enable-tag-mode', 'true')
    let reads = 0,
      writes = 0
    api.defaults.adapter = async (config) => {
      if (config.method !== 'get') writes += 1
      let data: unknown = []
      if (config.url === '/api/channel') {
        reads += 1
        data = {
          items: [
            { ...channel(7), tag: 'shared' },
            { ...channel(8), tag: 'shared' },
          ],
          total: 2,
          type_counts: { '1': 2 },
        }
      }
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: { success: true, data },
      }
    }
    const view = await renderPage(<ChannelsTable />, queryClient),
      fields = within(view.container),
      body = within(document.body)
    try {
      await waitFor(() =>
        assert.ok(fields.queryByRole('button', { name: '10' }))
      )
      await view.click(fields.getByRole('button', { name: '10' }))
      const input = view.container.querySelector(
        'input.font-mono'
      ) as HTMLInputElement
      await act(async () => {
        fireEvent.change(input, { target: { value: '20' } })
        fireEvent.keyDown(input, { key: 'Enter' })
      })
      await waitFor(() => assert.ok(body.queryByRole('alertdialog')))
      await act(async () => {
        await queryClient.invalidateQueries({
          queryKey: channelsQueryKeys.lists(),
        })
        window.dispatchEvent(new Event('focus'))
      })
      assert.equal(reads, 1)
      assert.ok(
        body.getByText(
          'This will update the priority to 20 for all 2 channel(s) with tag "shared". Continue?'
        )
      )
      await view.click(body.getByRole('button', { name: 'Cancel' }))
      await waitFor(() => assert.ok(reads >= 2))
      assert.equal(writes, 0)
    } finally {
      await view.unmount()
      queryClient.clear()
      api.defaults.adapter = adapter
      if (previousTagMode === null) localStorage.removeItem('enable-tag-mode')
      else localStorage.setItem('enable-tag-mode', previousTagMode)
    }
  }
)
