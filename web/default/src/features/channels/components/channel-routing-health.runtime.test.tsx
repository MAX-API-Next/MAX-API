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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import type { ChannelRoutingHealth } from '../lib/channel-routing-health'
import { channelSchema, type Channel } from '../types'

const env = createReactTestEnvironment()
await env.setup()
const previousStorage = Object.getOwnPropertyDescriptor(
  globalThis,
  'localStorage'
)
const previousMatchMedia = Object.getOwnPropertyDescriptor(
  globalThis,
  'matchMedia'
)
const previousCustomElements = Object.getOwnPropertyDescriptor(
  globalThis,
  'customElements'
)
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: env.dom.window.localStorage,
})
Object.defineProperty(globalThis, 'matchMedia', {
  configurable: true,
  value: env.dom.window.matchMedia.bind(env.dom.window),
})
Object.defineProperty(globalThis, 'customElements', {
  configurable: true,
  value: env.dom.window.customElements,
})
const { within, waitFor } = await import('@testing-library/react/pure')
const { ChannelRoutingHealthCell } = await import('./channel-routing-health')
const { useChannelsColumns } = await import('./channels-columns')
const { ChannelsProvider } = await import('./channels-provider')
const { MobileCardList } =
  await import('@/components/data-table/mobile-card-list')
const { api } = await import('@/lib/api')
const { aggregateChannelsByTag } = await import('../lib/channel-utils')

after(() => {
  env.teardown()
  if (previousStorage)
    Object.defineProperty(globalThis, 'localStorage', previousStorage)
  else Reflect.deleteProperty(globalThis, 'localStorage')
  if (previousMatchMedia)
    Object.defineProperty(globalThis, 'matchMedia', previousMatchMedia)
  else Reflect.deleteProperty(globalThis, 'matchMedia')
  if (previousCustomElements)
    Object.defineProperty(globalThis, 'customElements', previousCustomElements)
  else Reflect.deleteProperty(globalThis, 'customElements')
})

function health(
  overrides: Partial<ChannelRoutingHealth> = {}
): ChannelRoutingHealth {
  return {
    schema_version: 1,
    status: 'normal',
    source: 'redis',
    base_priority: 10,
    effective_priority: 10,
    stored_penalty: 0,
    active_penalty: 0,
    stored_runtime_disabled: false,
    runtime_disabled: false,
    penalty_until: null,
    disabled_until: null,
    penalty_recovery_mode: null,
    disable_recovery_mode: 'probe_then_automatic',
    last_observed_at: null,
    evaluated_at: new Date().toISOString(),
    unavailable_reason: null,
    ...overrides,
  }
}

function channel(id: number, snapshot?: unknown): Channel {
  return channelSchema.parse({
    id,
    priority: 10,
    routing_health: snapshot,
    name: `Channel ${id}`,
    type: 1,
    key: '',
    status: 1,
    tag: 'shared',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
}

test('penalized priority and recovery details are visible without changing the configured value', async () => {
  const source = channel(
    7,
    health({
      status: 'penalized',
      effective_priority: 9,
      stored_penalty: 1,
      active_penalty: 1,
      penalty_recovery_mode: 'manual',
    })
  )
  const view = await env.render(<ChannelRoutingHealthCell channel={source} />)
  try {
    const fields = within(view.container)
    assert.ok(fields.getByText('9', { exact: true }))
    assert.ok(fields.getByText('Timeout penalty: 1'))
    assert.ok(fields.getByText('Waiting for manual recovery'))
    assert.equal(view.container.querySelectorAll('input').length, 0)
    const details = view.container.querySelector('details')!
    await view.click(fields.getByText('Routing snapshot details'))
    assert.equal(details.open, true)
    assert.ok(fields.getByText('Configured priority: 10'))
    assert.equal(source.priority, 10)
  } finally {
    await view.unmount()
  }
})

test('zero, unavailable snapshots and unsupported servers remain distinguishable', async () => {
  for (const [snapshot, expected] of [
    [
      health({ base_priority: 0, effective_priority: 0 }),
      'No runtime restriction',
    ],
    [undefined, 'Runtime state display is not supported by this server.'],
    [{ schema_version: 2 }, 'Unsupported runtime state version'],
    [{ schema_version: 1 }, 'Runtime state unavailable'],
    [
      health({
        status: 'unknown',
        source: 'unavailable',
        effective_priority: null,
        stored_penalty: null,
        active_penalty: null,
        stored_runtime_disabled: null,
        runtime_disabled: null,
        unavailable_reason: 'private diagnostic',
      }),
      'Runtime state unavailable',
    ],
  ] as const) {
    const view = await env.render(
      <ChannelRoutingHealthCell channel={channel(7, snapshot)} />
    )
    try {
      assert.ok(within(view.container).getByText(expected))
      if (expected === 'No runtime restriction') {
        assert.ok(within(view.container).getByText('0', { exact: true }))
      } else {
        assert.ok(within(view.container).getByText('—', { exact: true }))
      }
      assert.equal(
        view.container.textContent?.includes('private diagnostic'),
        false
      )
    } finally {
      await view.unmount()
    }
  }
})

test('a due disabled state still waits for a probe and never displays priority zero', async () => {
  const view = await env.render(
    <ChannelRoutingHealthCell
      channel={channel(
        7,
        health({
          status: 'runtime_disabled',
          effective_priority: null,
          stored_runtime_disabled: true,
          runtime_disabled: true,
          disabled_until: new Date(Date.now() - 60_000).toISOString(),
        })
      )}
    />
  )
  try {
    const fields = within(view.container)
    assert.ok(fields.getByText('Paused for upstream timeouts'))
    assert.ok(fields.getByText('Waiting for a successful recovery probe'))
    assert.equal(fields.queryByText('0', { exact: true }), null)
  } finally {
    await view.unmount()
  }
})

test('a penalty at its minimum and an expired snapshot are explained without recomputing routing', async () => {
  const view = await env.render(
    <ChannelRoutingHealthCell
      channel={channel(
        7,
        health({
          status: 'penalized',
          base_priority: 0,
          effective_priority: 0,
          stored_penalty: 1,
          active_penalty: 1,
          penalty_recovery_mode: 'automatic',
          penalty_until: new Date(Date.now() - 60_000).toISOString(),
        })
      )}
    />
  )
  try {
    const fields = within(view.container)
    assert.ok(fields.getByText('0', { exact: true }))
    assert.ok(fields.getByText('Priority is at its minimum'))
    assert.ok(fields.getByText('Waiting for a fresh runtime snapshot'))
  } finally {
    await view.unmount()
  }
})

test('tag rows report current-result counts and do not present the first child as the tag', async () => {
  const rows = aggregateChannelsByTag([
    channel(
      1,
      health({
        status: 'penalized',
        effective_priority: 9,
        stored_penalty: 1,
        active_penalty: 1,
        penalty_recovery_mode: 'manual',
      })
    ),
    channel(
      2,
      health({
        status: 'runtime_disabled',
        effective_priority: null,
        stored_runtime_disabled: true,
        runtime_disabled: true,
      })
    ),
    channel(3),
  ])
  const view = await env.render(<ChannelRoutingHealthCell channel={rows[0]!} />)
  try {
    const fields = within(view.container)
    assert.ok(
      fields.getByText('Current results: 1 penalized / 1 paused / 1 unknown')
    )
    assert.ok(fields.getByText('Runtime state is partially unknown'))
    assert.ok(fields.getByText('Includes channels paused by timeout policy'))
    assert.equal(fields.queryByText('Timeout penalty: 1'), null)
  } finally {
    await view.unmount()
  }
})

function MobileHarness(props: { channel: Channel }) {
  const columns = useChannelsColumns().filter((column) => {
    const id =
      column.id ?? ('accessorKey' in column ? column.accessorKey : undefined)
    return id === 'name' || id === 'priority' || id === 'effective_priority'
  })
  const table = useReactTable({
    data: [props.channel],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  return <MobileCardList table={table} />
}

test('actual mobile columns show both priorities and editing sends only configured priority', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const originalAdapter = api.defaults.adapter
  const requests: Record<string, unknown>[] = []
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'put')
    requests.push(JSON.parse(String(config.data)) as Record<string, unknown>)
    return {
      data: { success: true },
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
    }
  }
  const view = await env.render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <MobileHarness
          channel={channel(
            7,
            health({
              status: 'penalized',
              effective_priority: 9,
              stored_penalty: 1,
              active_penalty: 1,
              penalty_recovery_mode: 'manual',
            })
          )}
        />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  try {
    const fields = within(view.container)
    assert.ok(fields.getByText('Configured priority', { exact: true }))
    assert.ok(fields.getByText('Snapshot effective priority', { exact: true }))
    assert.ok(fields.getByText('10', { exact: true }))
    assert.ok(fields.getByText('9', { exact: true }))
    await view.click(fields.getByRole('button', { name: 'Increment' }))
    await waitFor(() => assert.equal(requests.length, 1))
    assert.deepEqual(requests[0], { id: 7, priority: 11 })
  } finally {
    await view.unmount()
    api.defaults.adapter = originalAdapter
    client.clear()
  }
})
