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
import { waitFor, within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { api } from '@/lib/api'

const testEnv = createReactTestEnvironment()
await testEnv.setup()
const previousStorage = Object.getOwnPropertyDescriptor(
  globalThis,
  'localStorage'
)
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: testEnv.dom.window.localStorage,
})

const { ChannelsProvider, useChannels } = await import('../channels-provider')
const { TagBatchEditDialog } = await import('./tag-batch-edit-dialog')
after(() => {
  testEnv.teardown()
  if (previousStorage)
    Object.defineProperty(globalThis, 'localStorage', previousStorage)
  else Reflect.deleteProperty(globalThis, 'localStorage')
})

test('a late response for a previous tag cannot overwrite the current tag', async () => {
  const originalAdapter = api.defaults.adapter
  const pending = new Map<string, (models: string) => void>()
  const requests: string[] = []
  let groupRequests = 0
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'get')
    if (config.url === '/api/group/') groupRequests += 1
    let data: unknown = { success: true, data: [] }
    if (config.url === '/api/channel/tag/models') {
      const tag = String(config.params.tag)
      requests.push(tag)
      const models = await new Promise<string>((resolve) => {
        pending.set(tag, resolve)
      })
      data = { success: true, data: models }
    }
    return { data, config, status: 200, statusText: 'OK', headers: {} }
  }
  function Harness() {
    const { setCurrentTag } = useChannels()
    const [, setRevision] = useState(0)
    return (
      <>
        <button onClick={() => setCurrentTag('first')}>Select first tag</button>
        <button onClick={() => setCurrentTag('second')}>
          Select second tag
        </button>
        <button onClick={() => setRevision((value) => value + 1)}>
          Rerender
        </button>
        <TagBatchEditDialog open onOpenChange={() => {}} />
      </>
    )
  }
  const view = await testEnv.render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <Harness />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  const first = within(view.container).getByRole('button', {
    name: 'Select first tag',
  })
  const second = within(view.container).getByRole('button', {
    name: 'Select second tag',
  })
  const rerender = within(view.container).getByRole('button', {
    name: 'Rerender',
  })
  const fields = within(testEnv.dom.window.document.body)
  try {
    await view.click(first)
    await waitFor(() => assert.ok(pending.has('first')))
    await view.click(second)
    await waitFor(() => assert.ok(pending.has('second')))
    await act(async () => pending.get('second')!('model-second'))
    await waitFor(() => {
      assert.equal(
        (fields.getByLabelText('Models') as HTMLTextAreaElement).value,
        'model-second'
      )
    })
    await act(async () => pending.get('first')!('model-first'))
    await waitFor(() => {
      assert.equal(
        (fields.getByLabelText('Models') as HTMLTextAreaElement).value,
        'model-second'
      )
      assert.equal(
        (fields.getByLabelText('Tag Name') as HTMLInputElement).value,
        'second'
      )
    })
    await view.click(rerender)
    await act(async () => {
      await testEnv.i18n.changeLanguage('fr')
    })
    assert.deepEqual(requests, ['first', 'second'])
    assert.equal(groupRequests, 1)
  } finally {
    await view.unmount()
    client.clear()
    api.defaults.adapter = originalAdapter
  }
})

test('tag models become editable without waiting for an unused all-models request', async () => {
  const originalAdapter = api.defaults.adapter
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  let auxiliaryRequests = 0
  let releaseAuxiliary: () => void = () => {}
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'get')
    let data: unknown = { success: true, data: [] }
    if (config.url === '/api/channel/tag/models') {
      data = { success: true, data: 'tag-only-model' }
    } else if (config.url === '/api/channel/models') {
      auxiliaryRequests += 1
      data = await new Promise((resolve) => {
        releaseAuxiliary = () => resolve({ success: true, data: [] })
      })
    }
    return { data, config, status: 200, statusText: 'OK', headers: {} }
  }
  function Harness() {
    const { setCurrentTag } = useChannels()
    return (
      <>
        <button onClick={() => setCurrentTag('ready')}>Select ready tag</button>
        <TagBatchEditDialog open onOpenChange={() => {}} />
      </>
    )
  }
  const view = await testEnv.render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <Harness />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  try {
    await view.click(
      within(view.container).getByRole('button', { name: 'Select ready tag' })
    )
    await act(async () => {
      await new Promise<void>((resolve) => setTimeout(resolve, 0))
    })
    const models = within(testEnv.dom.window.document.body).queryByLabelText(
      'Models'
    ) as HTMLTextAreaElement | null
    assert.equal(models !== null, true)
    assert.equal(models?.value, 'tag-only-model')
    assert.equal(auxiliaryRequests, 0)
  } finally {
    await act(async () => releaseAuxiliary())
    await view.unmount()
    client.clear()
    api.defaults.adapter = originalAdapter
  }
})
