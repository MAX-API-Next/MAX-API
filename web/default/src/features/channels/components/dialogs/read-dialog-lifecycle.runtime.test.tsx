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
import { act, useState } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { api } from '@/lib/api'
import { channelSchema } from '../../types'

const env = createReactTestEnvironment()
await env.setup()
Object.defineProperty(env.dom.window.Element.prototype, 'getAnimations', {
  configurable: true,
  value: () => [],
})
const storage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: env.dom.window.localStorage,
})
const { within, waitFor } = await import('@testing-library/react/pure')
const { ChannelsProvider, useChannels } = await import('../channels-provider')
const { MultiKeyManageDialog } = await import('./multi-key-manage-dialog')
const { BalanceQueryDialog } = await import('./balance-query-dialog')
const { UserBindingDialog } =
  await import('@/features/users/components/dialogs/user-binding-dialog')
after(() => {
  env.teardown()
  if (storage) Object.defineProperty(globalThis, 'localStorage', storage)
  else Reflect.deleteProperty(globalThis, 'localStorage')
})

function ChannelHarness({ mode }: { mode: 'keys' | 'usage' }) {
  const { setCurrentRow } = useChannels()
  const select = (id: number) =>
    setCurrentRow(
      channelSchema.parse({
        id,
        type: mode === 'keys' ? 1 : 57,
        key: '',
        status: 1,
        name: `Synthetic channel ${id}`,
        created_time: 0,
        test_time: 0,
        response_time: 0,
        balance_updated_time: 0,
      })
    )
  return (
    <>
      <button onClick={() => select(1)}>First channel</button>
      <button onClick={() => select(2)}>Second channel</button>
      {mode === 'keys' ? (
        <MultiKeyManageDialog open onOpenChange={() => {}} />
      ) : (
        <BalanceQueryDialog open onOpenChange={() => {}} />
      )}
    </>
  )
}

test('late multi-key results cannot replace the current channel keys', async () => {
  const adapter = api.defaults.adapter
  const pending = new Map<number, () => void>()
  const requests: number[] = []
  api.defaults.adapter = async (config) => {
    assert.equal(config.url, '/api/channel/multi_key/manage')
    const params = JSON.parse(config.data)
    assert.equal(params.action, 'get_key_status')
    const id = Number(params.channel_id)
    requests.push(id)
    await new Promise<void>((resolve) => pending.set(id, resolve))
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: {
          keys: [{ index: 0, status: 2, reason: `synthetic-reason-${id}` }],
          total: 1,
          page: 1,
          page_size: 10,
          total_pages: 1,
          enabled_count: 0,
          manual_disabled_count: 1,
          auto_disabled_count: 0,
        },
      },
    }
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = await env.render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelHarness mode='keys' />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  const body = within(env.dom.window.document.body)
  try {
    await view.click(within(view.container).getByText('First channel'))
    await view.click(within(view.container).getByText('Second channel'))
    assert.ok(pending.has(2))
    await act(async () => pending.get(2)!())
    assert.ok(body.queryByText('synthetic-reason-2'))
    await act(async () => pending.get(1)!())
    assert.ok(body.queryByText('synthetic-reason-2'))
    assert.equal(body.queryByText('synthetic-reason-1'), null)
    assert.deepEqual(requests, [1, 2])
  } finally {
    pending.forEach((resolve) => resolve())
    await view.unmount()
    client.clear()
    api.defaults.adapter = adapter
  }
})

test('Codex usage loads the new channel even when its type stays the same', async () => {
  const adapter = api.defaults.adapter
  const pending = new Map<number, () => void>()
  api.defaults.adapter = async (config) => {
    const id = Number(config.url?.match(/channel\/(\d+)\/codex\/usage/)?.[1])
    assert.ok(id === 1 || id === 2)
    await new Promise<void>((resolve) => pending.set(id, resolve))
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: { email: `codex-${id}@example.invalid`, plan_type: 'pro' },
      },
    }
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const view = await env.render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <ChannelHarness mode='usage' />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  const body = within(env.dom.window.document.body)
  try {
    await view.click(within(view.container).getByText('First channel'))
    await view.click(within(view.container).getByText('Second channel'))
    assert.ok(pending.has(2), 'changing channel ID must load its usage')
    await act(async () => pending.get(2)!())
    assert.ok(body.queryByText('codex-2@example.invalid'))
    await act(async () => pending.get(1)!())
    assert.ok(body.queryByText('codex-2@example.invalid'))
    assert.equal(body.queryByText('codex-1@example.invalid'), null)
  } finally {
    pending.forEach((resolve) => resolve())
    await view.unmount()
    client.clear()
    api.defaults.adapter = adapter
  }
})

test('late binding details cannot replace another selected user', async () => {
  const adapter = api.defaults.adapter
  const pending = new Map<number, () => void>()
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'get')
    let data: unknown = []
    const match = config.url?.match(/^\/api\/user\/(\d+)$/)
    if (match) {
      const id = Number(match[1])
      await new Promise<void>((resolve) => pending.set(id, resolve))
      data = {
        id,
        username: `synthetic-user-${id}`,
        email: `binding-${id}@example.invalid`,
      }
    } else if (config.url === '/api/status') data = { email_verification: true }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data },
    }
  }
  function Harness() {
    const [id, setId] = useState(1)
    return (
      <>
        <button onClick={() => setId(2)}>Another binding user</button>
        <UserBindingDialog userId={id} open onOpenChange={() => {}} />
      </>
    )
  }
  const view = await env.render(<Harness />)
  const body = within(env.dom.window.document.body)
  try {
    await waitFor(() => assert.ok(pending.has(1)))
    await view.click(within(view.container).getByText('Another binding user'))
    assert.ok(pending.has(2))
    await act(async () => pending.get(2)!())
    assert.ok(body.queryByText('binding-2@example.invalid'))
    await act(async () => pending.get(1)!())
    assert.ok(body.queryByText('binding-2@example.invalid'))
    assert.equal(body.queryByText('binding-1@example.invalid'), null)
  } finally {
    pending.forEach((resolve) => resolve())
    await view.unmount()
    api.defaults.adapter = adapter
  }
})
