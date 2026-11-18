// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import type { ComponentProps } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from '@tanstack/react-router'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { PluginEntry, PluginMutation, TaskPluginAPI } from './api'

const env = createReactTestEnvironment()
await env.setup()
// The app typecheck uses Node types; load the Bun-only API at runtime.
const testRuntimeModule = 'bun:test'
const { mock } = (await import(testRuntimeModule)) as {
  mock: {
    module: (name: string, factory: () => Record<string, unknown>) => void
    restore: () => void
  }
}
const { render, cleanup, fireEvent, waitFor, within, act } =
  await import('@testing-library/react')
type CatalogProps = ComponentProps<
  typeof import('./plugin-catalog').PluginCatalog
>
type DetailProps = ComponentProps<
  typeof import('./plugin-detail-sheet').PluginDetailSheet
>

// Exercise the real container and catalog request lifecycle. The presentation
// adapters keep this state interleaving independent of sheet animation/layout;
// the companion runtime suite covers the actual sheets and confirmations.
mock.module('./plugin-catalog', () => ({
  PluginCatalog: (props: CatalogProps) => (
    <div>
      {props.entries.map((entry) => (
        <button
          key={`${entry.key}@${entry.version}`}
          onClick={(event) => props.onDetails(entry, event.currentTarget)}
        >
          Open {entry.version}
        </button>
      ))}
    </div>
  ),
}))
mock.module('./plugin-install-sheet', () => ({
  PluginInstallSheet: () => null,
}))
mock.module('./plugin-detail-sheet', () => ({
  PluginDetailSheet: (props: DetailProps) =>
    props.detail ? (
      <section role='dialog' aria-label='Selected version'>
        <h2>{props.detail.name}</h2>
        <button onClick={() => props.setSelected(undefined)}>Close</button>
        <button
          disabled={props.disabled}
          onClick={() => {
            const detail = props.detail
            if (detail)
              void props.mutate({
                action: 'delete',
                key: detail.key,
                version: detail.version,
                expected_revision: props.snapshot!.revision,
                deletion_nodes_ready: true,
              })
          }}
        >
          Delete version
        </button>
      </section>
    ) : null,
}))
const { TaskPlugins } = await import('./index')
after(() => {
  cleanup()
  mock.restore()
  env.teardown()
})

test('a delayed deletion preserves another selected version of the same extension', async () => {
  const entry: PluginEntry = {
    key: 'selection',
    name: 'Active Fixture',
    version: '2.0.0',
    digest: 'a'.repeat(64),
    format_version: 1,
    active: true,
    enabled: true,
    revoked: false,
    models: [],
    protocols: [],
    routes: [],
    usage_schema: {},
    usage_profiles: [],
    execution_ready: false,
    admission_ready: false,
  }
  const state = {
    revision: '1',
    entries: [
      entry,
      { ...entry, name: 'Historical Fixture', version: '1.0.0', active: false },
    ],
  }
  const writes: PluginMutation[] = []
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  const transport: TaskPluginAPI = {
    read: async () => ({ ...state, entries: [...state.entries] }),
    validate: async () => entry,
    mutate: async (command) => {
      if (command.action !== 'delete') throw new Error('expected deletion')
      writes.push(command)
      await pending
      state.entries = state.entries.filter(
        (item) => item.key !== command.key || item.version !== command.version
      )
      state.revision = '2'
      return {
        committed: true,
        revision: '2',
        execution_ready: false,
        admission_ready: false,
      }
    },
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const router = createRouter({
    routeTree: createRootRoute({ component: TaskPlugins }),
    history: createMemoryHistory(),
  })
  try {
    const view = render(
      <I18nextProvider i18n={env.i18n}>
        <QueryClientProvider client={client}>
          <RouterContextProvider router={router}>
            <TaskPlugins transport={transport} />
          </RouterContextProvider>
        </QueryClientProvider>
      </I18nextProvider>
    )
    fireEvent.click(await view.findByRole('button', { name: 'Open 2.0.0' }))
    const first = within(view.getByRole('dialog'))
    assert.ok(first.getByRole('heading', { name: 'Active Fixture' }))
    fireEvent.click(first.getByRole('button', { name: 'Delete version' }))
    await waitFor(() => assert.equal(writes.length, 1))
    fireEvent.click(first.getByRole('button', { name: 'Close' }))
    assert.equal(view.queryByRole('dialog'), null)
    fireEvent.click(view.getByRole('button', { name: 'Open 1.0.0' }))
    assert.ok(
      within(view.getByRole('dialog')).getByRole('heading', {
        name: 'Historical Fixture',
      })
    )
    await act(async () => release())
    await waitFor(() =>
      assert.equal(view.queryByRole('button', { name: 'Open 2.0.0' }), null)
    )
    assert.ok(
      within(view.getByRole('dialog')).getByRole('heading', {
        name: 'Historical Fixture',
      })
    )
    assert.equal(writes.length, 1)
    const command = writes[0]
    assert.ok(command.action === 'delete')
    assert.equal(command.version, '2.0.0')
  } finally {
    release()
    await act(async () => {
      await client.cancelQueries()
      cleanup()
      client.clear()
      await new Promise<void>((resolve) => setTimeout(resolve, 0))
    })
  }
})
