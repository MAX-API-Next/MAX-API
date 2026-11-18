// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useMemo } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterContextProvider,
} from '@tanstack/react-router'
import english from '@/i18n/locales/en.json'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after as afterAll, afterEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { PluginEntry, PluginMutation, TaskPluginAPI } from './api'

const env = createReactTestEnvironment()
await env.setup()
env.i18n.addResourceBundle('en', 'translation', {
  'Task Plugins': english.translation['Task Plugins'],
  'Plugin details': english.translation['Plugin details'],
  'Install plugin package': english.translation['Install plugin package'],
  'Task plugin catalog could not be loaded':
    english.translation['Task plugin catalog could not be loaded'],
  'Plugin catalog updated. Execution and paid admission remain unavailable.':
    english.translation[
      'Plugin catalog updated. Execution and paid admission remain unavailable.'
    ],
  'Task plugin request failed':
    english.translation['Task plugin request failed'],
})
const originalScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo')
// The real router scrolls on navigation; JSDOM needs this browser primitive.
Object.defineProperty(globalThis, 'scrollTo', {
  configurable: true,
  value: () => {},
})
const { render, cleanup, fireEvent, waitFor, act, within } =
  await import('@testing-library/react')
const { TaskPlugins } = await import('./index')
const { PluginError } = await import('./api')
const { useSidebarData } = await import('@/hooks/use-sidebar-data')
const { useAuthStore } = await import('@/stores/auth-store')
const { Route } = await import('@/routes/_authenticated/task-plugins')
const { SidebarModulesSection } =
  await import('@/features/system-settings/maintenance/sidebar-modules-section')
const { parseSidebarModulesAdmin, serializeSidebarModulesAdmin } =
  await import('@/features/system-settings/maintenance/config')
const { SettingsPageProvider } =
  await import('@/features/system-settings/components/settings-page-context')
const { useSidebarConfig } = await import('@/hooks/use-sidebar-config')
const { useStatus } = await import('@/hooks/use-status')
const { api } = await import('@/lib/api')
const fixtureClients: QueryClient[] = []
afterEach(async () => {
  // Finish real query/mutation notifications before removing browser globals.
  await waitFor(() => {
    for (const client of fixtureClients) {
      assert.equal(client.isFetching(), 0)
      assert.equal(client.isMutating(), 0)
    }
  })
  await act(async () => {
    cleanup()
    for (const client of fixtureClients) client.clear()
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
  })
  fixtureClients.length = 0
})
afterAll(() => {
  if (originalScrollTo)
    Object.defineProperty(globalThis, 'scrollTo', originalScrollTo)
  else Reflect.deleteProperty(globalThis, 'scrollTo')
  env.teardown()
})
test('the management route and sidebar are restricted to the root role', () => {
  const previous = useAuthStore.getState().auth.user
  const beforeLoad = Route.options.beforeLoad!
  function SidebarProbe() {
    return (
      <output>
        {useSidebarData()
          .navGroups.flatMap((g) => g.items)
          .some((item) => 'url' in item && item.url === '/task-plugins')
          ? 'plugin-link-visible'
          : 'plugin-link-hidden'}
      </output>
    )
  }
  try {
    for (const role of [1, 10, 100]) {
      useAuthStore
        .getState()
        .auth.setUser({ id: 901, username: 'fixture-root-role', role })
      const v = render(
        <I18nextProvider i18n={env.i18n}>
          <SidebarProbe />
        </I18nextProvider>
      )
      assert.ok(
        v.getByText(role === 100 ? 'plugin-link-visible' : 'plugin-link-hidden')
      )
      if (role === 100) assert.doesNotThrow(() => beforeLoad({} as never))
      else assert.throws(() => beforeLoad({} as never))
      v.unmount()
    }
  } finally {
    useAuthStore.getState().auth.setUser(previous)
  }
})
const entry: PluginEntry = {
  key: 'kling',
  name: 'Kling Fixture',
  version: '1.2.0',
  digest: 'a'.repeat(64),
  format_version: 2,
  active: true,
  enabled: true,
  revoked: false,
  models: ['kling-v3'],
  protocols: [{ name: 'openai_video', supports: [] }],
  routes: [],
  usage_schema: { units: { type: 'number', unit: 'credit' } },
  usage_profiles: [],
  execution_ready: false,
  admission_ready: false,
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
function fixture() {
  const state = { revision: '1', entries: [{ ...entry }] }
  const writes: PluginMutation[] = []
  const api: TaskPluginAPI = {
    read: async () => ({
      ...state,
      entries: state.entries.map((e) => ({ ...e })),
    }),
    validate: async (upload) => ({
      ...entry,
      digest: upload.expected_sha256 ?? entry.digest,
    }),
    mutate: async (command) => {
      writes.push(command)
      state.revision = String(BigInt(state.revision) + 1n)
      state.entries[0].enabled = false
      return {
        committed: true,
        revision: state.revision,
        execution_ready: false,
        admission_ready: false,
      }
    },
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  fixtureClients.push(client)
  const rootRoute = createRootRoute({ component: Outlet })
  const listRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/task-plugins',
    component: () => <TaskPlugins transport={api} />,
  })
  const pricingRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/system-settings/billing/$section',
    component: () => <p>pricing-route-reached</p>,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([listRoute, pricingRoute]),
    history: createMemoryHistory({ initialEntries: ['/task-plugins'] }),
    defaultPendingMinMs: 0,
  })
  const view = () =>
    render(
      <I18nextProvider i18n={env.i18n}>
        <QueryClientProvider client={client}>
          <RouterContextProvider router={router}>
            <TaskPlugins transport={api} />
          </RouterContextProvider>
        </QueryClientProvider>
      </I18nextProvider>
    )
  return { state, writes, api, client, view, router }
}
test('catalog filters retain exact version selection across card and table views', async () => {
  const f = fixture()
  f.state.entries.push(
    { ...entry, key: 'disabled', name: 'Disabled Fixture', enabled: false },
    { ...entry, version: '1.1.0', name: 'Historical Fixture', active: false },
    { ...entry, key: 'revoked', name: 'Revoked Fixture', revoked: true }
  )
  const v = f.view()
  await v.findByText('Kling Fixture')
  assert.equal(v.getAllByRole('article').length, 4)
  fireEvent.change(v.getByRole('combobox', { name: 'Filter by status' }), {
    target: { value: 'historical' },
  })
  assert.equal(v.getAllByRole('article').length, 1)
  assert.ok(v.getByText('Historical Fixture'))
  fireEvent.click(v.getByRole('button', { name: 'Table view' }))
  assert.ok(v.getByRole('table'))
  assert.equal(
    v.getByRole('button', { name: 'Table view' }).getAttribute('aria-pressed'),
    'true'
  )
  assert.equal(v.queryByText('Kling Fixture'), null)
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  const panel = within(await v.findByRole('dialog'))
  assert.ok(panel.getByRole('heading', { name: 'Historical Fixture' }))
  assert.ok(panel.getByText('kling · 1.1.0'))
  assert.ok(panel.getByRole('button', { name: 'Activate version' }))
  assert.deepEqual(
    f.writes,
    [],
    'view/filter/detail interactions never mutate the catalog'
  )
})

test('custom package validation and installation work without an external SHA-256', async () => {
  const f = fixture()
  const uploads: unknown[] = []
  f.api.validate = async (upload) => {
    uploads.push(upload)
    return { ...entry, format_version: 1 }
  }
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Import API extension' }))
  fireEvent.change(v.getByLabelText('Package file'), {
    target: {
      files: [{ name: 'custom.json', size: 2, text: async () => '{}' }],
    },
  })
  const check = v.getByRole('button', { name: 'Validate package' })
  await waitFor(() =>
    assert.equal((check as HTMLButtonElement).disabled, false)
  )
  const pin = v.getByLabelText('SHA-256 (optional)')
  fireEvent.change(pin, { target: { value: 'not-a-digest' } })
  assert.equal((check as HTMLButtonElement).disabled, true)
  fireEvent.change(pin, { target: { value: '' } })
  assert.equal((check as HTMLButtonElement).disabled, false)
  fireEvent.click(check)
  const install = await v.findByRole('button', { name: 'Install and activate' })
  assert.ok(v.getByText(`Submitted content SHA-256: ${entry.digest}`))
  assert.equal((install as HTMLButtonElement).disabled, false)
  assert.deepEqual(uploads, [{ document: '{}', expected_sha256: undefined }])
  fireEvent.click(install)
  await waitFor(() => assert.equal(f.writes.length, 1))
  assert.equal(f.writes[0].action, 'install')
  assert.equal(f.writes[0].expected_sha256, undefined)
})

for (const [name, document, expectedDigest] of [
  ['raw JavaScript', 'export const meta = {};', 'b'.repeat(64)],
  ['format-3 JSON envelope', '{"format_version":3}', entry.digest],
]) {
  test(`computed upload digest identifies the file bytes for ${name}`, async () => {
    const f = fixture()
    f.api.validate = async () => ({
      ...entry,
      format_version: 3,
      source_sha256: 'b'.repeat(64),
    })
    const v = f.view()
    await v.findByText('Kling Fixture')
    fireEvent.click(v.getByRole('button', { name: 'Import API extension' }))
    fireEvent.change(v.getByLabelText('Package file'), {
      target: {
        files: [
          { name: 'custom', size: document.length, text: async () => document },
        ],
      },
    })
    const validate = v.getByRole('button', { name: 'Validate package' })
    await waitFor(() =>
      assert.equal((validate as HTMLButtonElement).disabled, false)
    )
    fireEvent.click(validate)
    await v.findByText(`Submitted content SHA-256: ${expectedDigest}`)
    assert.deepEqual(f.writes, [])
  })
}

test('deleting an installed version requires node acknowledgement and closes only after a receipt', async () => {
  const f = fixture()
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  f.api.mutate = async (command) => {
    f.writes.push(command)
    await pending
    f.state.entries = []
    f.state.revision = '2'
    return {
      committed: true,
      revision: '2',
      execution_ready: false,
      admission_ready: false,
    }
  }
  const v = f.view()
  fireEvent.click(await v.findByRole('button', { name: 'Details' }))
  const panel = within(await v.findByRole('dialog'))
  fireEvent.click(panel.getByRole('button', { name: 'Delete version' }))
  const confirm = panel.getByRole('button', { name: 'Confirm' })
  assert.equal((confirm as HTMLButtonElement).disabled, true)
  fireEvent.click(panel.getByRole('checkbox'))
  assert.equal((confirm as HTMLButtonElement).disabled, false)
  await act(async () => {
    fireEvent.click(confirm)
  })
  try {
    await waitFor(() => assert.equal(f.writes.length, 1))
    assert.ok(v.getByRole('dialog'), 'the dialog stays open before the receipt')
  } finally {
    await act(async () => {
      release()
      await pending
    })
  }
  await v.findByText('No API extensions found')
  await waitFor(() => assert.equal(v.queryByRole('dialog'), null))
  assert.equal(f.writes.length, 1)
  assert.equal(f.writes[0].action, 'delete')
  assert.equal(f.writes[0].expected_revision, '1')
  assert.equal(f.writes[0].deletion_nodes_ready, true)
})

test('delete is available directly and rejects referenced versions without retry', async () => {
  const f = fixture()
  f.state.entries[0].revoked = true
  const v = f.view()
  fireEvent.click(await v.findByRole('button', { name: 'Details' }))
  const panel = within(await v.findByRole('dialog'))
  assert.ok(
    panel.getByText(
      'Delete directly when there are no task, channel or pricing references. Installed source is removed; distribution bundles, audit identity and historical revocation are retained. Re-importing the same file remains blocked.'
    )
  )
  assert.equal(
    panel.queryByText(
      'Delete directly when there are no task, channel or pricing references. Installed source is removed; distribution bundles and audit identity are retained. The same file can be imported again.'
    ),
    null
  )
  assert.equal(panel.queryByRole('button', { name: 'Revoke version' }), null)
  assert.equal(
    (panel.getByRole('button', { name: 'Delete version' }) as HTMLButtonElement)
      .disabled,
    false
  )
  f.api.mutate = async (command) => {
    f.writes.push(command)
    throw new PluginError(
      'rejected',
      'Task plugin version is still referenced; remove task, channel or pricing references before deleting'
    )
  }
  fireEvent.click(panel.getByRole('button', { name: 'Delete version' }))
  fireEvent.click(panel.getByRole('checkbox'))
  fireEvent.click(panel.getByRole('button', { name: 'Confirm' }))
  await panel.findByText(
    'Task plugin version is still referenced; remove task, channel or pricing references before deleting'
  )
  assert.equal(f.writes.length, 1)
  assert.ok(v.getByRole('dialog'))
})

test('a lost deletion receipt stays unknown after the version disappears from the catalog', async () => {
  const f = fixture()
  f.state.entries[0].enabled = false
  f.api.mutate = async (command) => {
    f.writes.push(command)
    f.state.entries = []
    f.state.revision = '2'
    throw new PluginError('unknown', 'lost deletion receipt')
  }
  const v = f.view()
  fireEvent.click(await v.findByRole('button', { name: 'Details' }))
  const panel = within(await v.findByRole('dialog'))
  fireEvent.click(panel.getByRole('button', { name: 'Delete version' }))
  fireEvent.click(panel.getByRole('checkbox'))
  fireEvent.click(panel.getByRole('button', { name: 'Confirm' }))
  await panel.findByText(/The mutation result is unknown/)
  await waitFor(() =>
    assert.equal(
      (panel.getByRole('button', { name: 'Refresh' }) as HTMLButtonElement)
        .disabled,
      false
    )
  )
  const refreshed = deferred<{ revision: string; entries: PluginEntry[] }>()
  let refreshStarted = false
  let refreshCompleted = false
  f.api.read = async () => {
    refreshStarted = true
    const result = await refreshed.promise
    refreshCompleted = true
    return result
  }
  fireEvent.click(panel.getByRole('button', { name: 'Refresh' }))
  await waitFor(() => assert.equal(refreshStarted, true))
  assert.ok(panel.getByText(/The mutation result is unknown/))
  await act(async () => refreshed.resolve({ revision: '2', entries: [] }))
  await waitFor(() => {
    assert.equal(refreshCompleted, true)
    assert.equal(
      (panel.getByRole('button', { name: 'Refresh' }) as HTMLButtonElement)
        .disabled,
      false
    )
  })
  assert.equal(f.writes.length, 1)
  assert.ok(v.getByRole('dialog'))
  assert.ok(panel.getByText(/The mutation result is unknown/))
  assert.equal(
    panel.queryByText(
      'API extension catalog updated. Execution and paid admission remain unavailable.'
    ),
    null
  )
})

test('catalog search includes models hidden behind card overflow and distinguishes empty matches', async () => {
  const f = fixture()
  f.state.entries[0].models = ['one', 'two', 'three', 'four', 'hidden-model']
  const v = f.view()
  const content = within(v.container)
  await v.findByText('Kling Fixture')
  const searchStatus = v.getByRole('status', { name: 'API extension catalog' })
  assert.equal(searchStatus.textContent, '')
  fireEvent.change(v.getByRole('textbox', { name: 'Search API extensions' }), {
    target: { value: ' hidden-model ' },
  })
  assert.ok(v.getByText('Kling Fixture'))
  fireEvent.click(v.getByRole('button', { name: 'Declared models: +1' }))
  const panel = within(await v.findByRole('dialog'))
  assert.ok(panel.getByText('hidden-model'))
  fireEvent.click(panel.getByRole('button', { name: 'Close' }))
  await waitFor(() => assert.equal(v.queryByRole('dialog'), null))
  const searchInput = await content.findByRole('textbox', {
    name: 'Search API extensions',
  })
  await act(async () => searchInput.focus())
  fireEvent.change(searchInput, {
    target: { value: 'does-not-exist' },
  })
  assert.ok(v.getByText('No matching plugins'))
  assert.equal(
    v.getByRole('status', { name: 'API extension catalog' }),
    searchStatus
  )
  assert.match(searchStatus.textContent || '', /No matching plugins/)
  assert.equal(document.activeElement, searchInput)
  assert.equal(v.queryByText('No API extensions found'), null)
  fireEvent.change(searchInput, { target: { value: '' } })
  assert.equal(
    v.getByRole('status', { name: 'API extension catalog' }),
    searchStatus
  )
  assert.equal(searchStatus.textContent, '')
  assert.equal(document.activeElement, searchInput)
  assert.deepEqual(f.writes, [])
})

test('lists real metadata and disables with the observed revision while invalidating prices', async () => {
  const f = fixture()
  f.client.setQueryData(['model-pricing-config', 'fixture'], { saved: true })
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  const panel = within(await v.findByRole('dialog'))
  await panel.findByRole('heading', { name: 'Model usage schemas' })
  const pricingLink = await panel.findByRole('link', {
    name: 'Open model and source pricing',
  })
  assert.equal(
    pricingLink.getAttribute('href'),
    '/system-settings/billing/model-pricing'
  )
  fireEvent.click(panel.getByRole('button', { name: 'Disable' }))
  fireEvent.click(await panel.findByRole('button', { name: 'Confirm' }))
  await waitFor(() => assert.equal(f.writes.length, 1))
  await panel.findByRole('button', { name: 'Enable' })
  assert.equal(f.writes[0].expected_revision, '1')
  assert.equal(
    f.client.getQueryState(['model-pricing-config', 'fixture'])?.isInvalidated,
    true
  )
  assert.ok(panel.getByText(/Execution and paid admission remain unavailable/))
  fireEvent.click(
    await panel.findByRole('link', { name: 'Open model and source pricing' })
  )
  await waitFor(() =>
    assert.equal(
      f.router.state.location.pathname,
      '/system-settings/billing/model-pricing'
    )
  )
  assert.deepEqual(f.client.getQueryData(['model-pricing-config', 'fixture']), {
    saved: true,
  })
})
test('closing details discards a pending disable before reopening the same version', async () => {
  const f = fixture()
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  fireEvent.click(await v.findByRole('button', { name: 'Disable' }))
  assert.ok(v.getByRole('button', { name: 'Confirm' }))
  fireEvent.click(v.getByRole('button', { name: 'Close' }))
  await waitFor(() => assert.equal(v.queryByRole('dialog'), null))
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  await v.findByRole('button', { name: 'Disable' })
  assert.equal(
    v.queryByRole('button', { name: 'Confirm' }) === null,
    true,
    'reopening the same version requires a fresh confirmation'
  )
  assert.equal(
    f.writes.length,
    0,
    'dismissing and reopening must not mutate the catalog'
  )
  fireEvent.click(v.getByRole('button', { name: 'Disable' }))
  fireEvent.click(v.getByRole('button', { name: 'Confirm' }))
  await waitFor(() => assert.equal(f.writes.length, 1))
  assert.equal(f.writes[0].action, 'disable')
  assert.equal(f.writes[0].expected_revision, '1')
})
test('a refreshed catalog cannot silently rebase an open confirmation', async () => {
  const f = fixture(),
    v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  fireEvent.click(await v.findByRole('button', { name: 'Disable' }))
  f.state.revision = '2'
  // Programmatic click exercises the page refresh even while the modal is open.
  fireEvent.click(v.getByText('Refresh'))
  await v.findByText(
    'Catalog changed; reopen the confirmation before continuing.'
  )
  assert.equal(
    (v.getByRole('button', { name: 'Confirm' }) as HTMLButtonElement).disabled,
    true
  )
  assert.equal(f.writes.length, 0)
})
test('unknown mutation rereads without replay and remains unresolved after matching state', async () => {
  const f = fixture()
  f.api.mutate = async (command) => {
    f.writes.push(command)
    f.state.revision = '2'
    f.state.entries[0].enabled = false
    throw new PluginError('unknown', 'lost receipt')
  }
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  fireEvent.click(await v.findByRole('button', { name: 'Disable' }))
  fireEvent.click(v.getByRole('button', { name: 'Confirm' }))
  await v.findByRole('button', { name: 'Enable' })
  assert.equal(f.writes.length, 1)
  assert.equal(
    (v.getByRole('button', { name: 'Enable' }) as HTMLButtonElement).disabled,
    true
  )
  const panel = within(v.getByRole('dialog'))
  assert.ok(panel.getByText(/The mutation result is unknown/))
  assert.ok(panel.getByText('lost receipt'))
  assert.ok(
    panel.getByRole('button', { name: 'I have checked the catalog; continue' })
  )
  // Closing the sheet keeps the unresolved operation visible on the page.
  fireEvent.click(v.getByRole('button', { name: 'Close' }))
  await v.findByText(/The mutation result is unknown/)
  const acknowledge = await v.findByRole('button', {
    name: 'I have checked the catalog; continue',
  })
  await waitFor(() =>
    assert.equal((acknowledge as HTMLButtonElement).disabled, false)
  )
  assert.ok(v.getByText('lost receipt'))
})
test('late validation is ignored after changing the pin', async () => {
  const f = fixture(),
    pending = deferred<PluginEntry>(),
    v = f.view()
  await v.findByText('Kling Fixture')
  f.api.validate = () => pending.promise
  fireEvent.click(v.getByRole('button', { name: 'Import API extension' }))
  const panel = within(await v.findByRole('dialog'))
  fireEvent.change(panel.getByLabelText('Package file'), {
    target: {
      files: [
        { name: 'fixture.json', size: 8, text: async () => 'document-A' },
      ],
    },
  })
  fireEvent.change(panel.getByLabelText('SHA-256 (optional)'), {
    target: { value: 'a'.repeat(64) },
  })
  const check = panel.getByRole('button', { name: 'Validate package' })
  await waitFor(() =>
    assert.equal((check as HTMLButtonElement).disabled, false)
  )
  fireEvent.click(check)
  fireEvent.change(panel.getByLabelText('SHA-256 (optional)'), {
    target: { value: 'b'.repeat(64) },
  })
  await act(async () => pending.resolve(entry))
  assert.equal(
    panel.queryByRole('button', { name: 'Install and activate' }),
    null
  )
  assert.deepEqual(f.writes, [])
})

test('format-2 installation requires node acknowledgement', async () => {
  const f = fixture()
  const v = f.view()
  fireEvent.click(
    await v.findByRole('button', { name: 'Import API extension' })
  )
  const panel = within(await v.findByRole('dialog'))
  fireEvent.change(panel.getByLabelText('Package file'), {
    target: {
      files: [
        { name: 'fixture.json', size: 8, text: async () => 'document-A' },
      ],
    },
  })
  fireEvent.change(panel.getByLabelText('SHA-256 (optional)'), {
    target: { value: 'b'.repeat(64) },
  })
  const check = panel.getByRole('button', { name: 'Validate package' })
  await waitFor(() =>
    assert.equal((check as HTMLButtonElement).disabled, false)
  )
  fireEvent.click(check)
  const install = await panel.findByRole('button', {
    name: 'Install and activate',
  })
  assert.equal((install as HTMLButtonElement).disabled, true)
  fireEvent.click(panel.getByRole('checkbox'))
  fireEvent.click(install)
  await waitFor(() => assert.equal(f.writes.length, 1))
  assert.equal(f.writes[0].action, 'install')
  assert.equal(f.writes[0].expected_revision, '1')
  if (f.writes[0].action === 'install')
    assert.equal(f.writes[0].expected_sha256, 'b'.repeat(64))
})
test('late official source download cannot replace a manually changed pin', async () => {
  const f = fixture(),
    download = deferred<{ document: string; expected_sha256: string }>(),
    v = f.view()
  f.api.bundled = async () => [
    { key: 'kling', version: '1.2.0', sha256: 'a'.repeat(64) },
  ]
  f.api.bundle = async () => download.promise
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Import API extension' }))
  const select = await v.findByLabelText('Bundled plugins')
  fireEvent.change(select, { target: { value: 'kling@1.2.0' } })
  fireEvent.change(v.getByLabelText('SHA-256 (optional)'), {
    target: { value: 'b'.repeat(64) },
  })
  await act(async () =>
    download.resolve({
      document: 'late source',
      expected_sha256: 'a'.repeat(64),
    })
  )
  assert.equal(
    (v.getByLabelText('SHA-256 (optional)') as HTMLInputElement).value,
    'b'.repeat(64)
  )
  assert.equal(
    (v.getByRole('button', { name: 'Validate package' }) as HTMLButtonElement)
      .disabled,
    true
  )
  assert.equal(f.writes.length, 0)
})

test('catalog read failures remove actionable stale state', async () => {
  const f = fixture(),
    v = f.view()
  await v.findByText('Kling Fixture')
  f.api.read = async () => {
    throw new Error('synthetic store fault')
  }
  fireEvent.click(v.getByRole('button', { name: 'Refresh' }))
  await v.findByText('API extension catalog could not be loaded')
  assert.equal(
    (
      v.getByRole('button', {
        name: 'Import API extension',
      }) as HTMLButtonElement
    ).disabled,
    true
  )
  assert.equal(v.queryByText('Kling Fixture'), null)
})

test('failed bundle query clears loading and does not retry or install', async () => {
  const f = fixture()
  let requests = 0
  f.api.bundled = async () => [
    { key: 'kling', version: '1.2.0-maxapi.1', sha256: 'a'.repeat(64) },
  ]
  f.api.bundle = async () => {
    requests++
    throw new Error('synthetic download failure')
  }
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Import API extension' }))
  const select = await v.findByLabelText('Bundled plugins')
  fireEvent.change(select, { target: { value: 'kling@1.2.0-maxapi.1' } })
  await v.findByText('Official plugin source could not be read')
  await waitFor(() =>
    assert.equal((select as HTMLSelectElement).disabled, false)
  )
  assert.equal(requests, 1)
  assert.equal(f.writes.length, 0)
  assert.equal(
    (v.getByRole('button', { name: 'Validate package' }) as HTMLButtonElement)
      .disabled,
    true
  )
})

test('a rejected catalog operation stays visible inside the detail dialog', async () => {
  const f = fixture()
  f.api.mutate = async (command) => {
    f.writes.push(command)
    throw new PluginError(
      'conflict',
      'Task plugin catalog changed; refresh before retrying'
    )
  }
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  const panel = within(v.getByRole('dialog'))
  fireEvent.click(panel.getByRole('button', { name: 'Disable' }))
  fireEvent.click(panel.getByRole('button', { name: 'Confirm' }))
  const alert = await panel.findByRole('alert')
  assert.match(alert.textContent ?? '', /catalog changed/)
  assert.equal(alert.closest('[aria-hidden="true"], [inert]'), null)
  assert.equal(f.writes.length, 1)
  assert.equal(f.state.entries[0].enabled, true)
})

test('an unknown operation can recover a failed reread inside the dialog without replay', async () => {
  const f = fixture()
  // Per-operation retry:false must override aggressive client defaults.
  f.client.setDefaultOptions({
    queries: { retry: 3, retryDelay: 0 },
    mutations: { retry: 3, retryDelay: 0 },
  })
  let reads = 0
  f.api.read = async () => {
    if (++reads === 2) throw new Error('synthetic failed reread')
    return { ...f.state, entries: f.state.entries.map((e) => ({ ...e })) }
  }
  f.api.mutate = async (command) => {
    f.writes.push(command)
    f.state.revision = '2'
    f.state.entries[0].enabled = false
    throw new PluginError('unknown', 'lost receipt')
  }
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  const panel = within(v.getByRole('dialog'))
  fireEvent.click(panel.getByRole('button', { name: 'Disable' }))
  fireEvent.click(panel.getByRole('button', { name: 'Confirm' }))
  await panel.findByText('API extension catalog could not be loaded')
  assert.ok(panel.getByText(/The mutation result is unknown/))
  assert.equal(
    (
      panel.getByRole('button', {
        name: 'I have checked the catalog; continue',
      }) as HTMLButtonElement
    ).disabled,
    true
  )
  const refresh = panel.getByRole('button', { name: 'Refresh' })
  await waitFor(() =>
    assert.equal((refresh as HTMLButtonElement).disabled, false)
  )
  fireEvent.click(refresh)
  const acknowledge = await panel.findByRole('button', {
    name: 'I have checked the catalog; continue',
  })
  await waitFor(() =>
    assert.equal((acknowledge as HTMLButtonElement).disabled, false)
  )
  assert.ok(panel.getByText(/The mutation result is unknown/))
  fireEvent.click(acknowledge)
  await waitFor(() =>
    assert.equal(
      (panel.getByRole('button', { name: 'Enable' }) as HTMLButtonElement)
        .disabled,
      false
    )
  )
  assert.equal(
    f.writes.length,
    1,
    'refresh and acknowledgement must never repeat the write'
  )
  assert.equal(f.state.revision, '2')
})

for (const [caseName, initial] of [
  ['empty', ''],
  ['legacy', '{"admin":{"enabled":true,"channel":false}}'],
]) {
  test(`${caseName} sidebar settings expose and persist the task-plugin switch`, async () => {
    const adapter = api.defaults.adapter
    const previous = useAuthStore.getState().auth.user
    function NavigationProbe() {
      const groups = useSidebarConfig(useSidebarData().navGroups)
      const visible = groups
        .flatMap((g) => g.items)
        .some((item) => 'url' in item && item.url === '/task-plugins')
      return (
        <output>
          {visible ? 'task-plugin-menu-visible' : 'task-plugin-menu-hidden'}
        </output>
      )
    }
    function SidebarSettingsProbe() {
      const { status } = useStatus()
      const serialized =
        typeof status?.SidebarModulesAdmin === 'string'
          ? status.SidebarModulesAdmin
          : ''
      const config = useMemo(
        () => parseSidebarModulesAdmin(serialized),
        [serialized]
      )
      return (
        <SidebarModulesSection
          config={config}
          initialSerialized={serializeSidebarModulesAdmin(config)}
        />
      )
    }
    try {
      useAuthStore
        .getState()
        .auth.setUser({ id: 901, username: 'fixture-root-role', role: 100 })
      let saved = initial
      const requests: Array<{ key: string; value: string }> = []
      api.defaults.adapter = async (config) => {
        let data: unknown
        if (config.method === 'put' && config.url === '/api/option/') {
          const body = JSON.parse(String(config.data)) as {
            key: string
            value: string
          }
          requests.push(body)
          saved = body.value
          data = { success: true }
        } else {
          assert.equal(config.url, '/api/status')
          data = { success: true, data: { SidebarModulesAdmin: saved } }
        }
        return { config, data, status: 200, statusText: 'fixture', headers: {} }
      }
      const client = new QueryClient({
        defaultOptions: {
          queries: { retry: false },
          mutations: { retry: false },
        },
      })
      client.setQueryData(['status'], { SidebarModulesAdmin: initial })
      const actions = document.createElement('div')
      document.body.append(actions)
      const v = render(
        <I18nextProvider i18n={env.i18n}>
          <QueryClientProvider client={client}>
            <SettingsPageProvider actionsContainer={actions}>
              <SidebarSettingsProbe />
              <NavigationProbe />
            </SettingsPageProvider>
          </QueryClientProvider>
        </I18nextProvider>
      )
      try {
        const content = within(v.container)
        const navigation = content.getByText('task-plugin-menu-visible')
        const toggle = content.getByRole('switch', { name: 'API Extensions' })
        const save = within(actions).getByRole('button', {
          name: 'Save sidebar modules',
        })
        assert.equal(toggle.getAttribute('aria-checked'), 'true')
        fireEvent.click(toggle)
        fireEvent.click(save)
        await waitFor(() => {
          assert.ok(navigation.isConnected)
          assert.equal(navigation.textContent, 'task-plugin-menu-hidden')
        })
        assert.equal(requests.length, 1)
        assert.equal(requests[0].key, 'SidebarModulesAdmin')
        assert.equal(JSON.parse(saved).admin.task_plugins, false)
        if (initial) assert.equal(JSON.parse(saved).admin.channel, false)
        assert.equal(parseSidebarModulesAdmin(saved).admin.task_plugins, false)
        await waitFor(() =>
          assert.equal(toggle.getAttribute('aria-checked'), 'false')
        )
        await waitFor(() =>
          assert.equal((save as HTMLButtonElement).disabled, false)
        )
        fireEvent.click(toggle)
        fireEvent.click(save)
        await waitFor(() => {
          assert.ok(navigation.isConnected)
          assert.equal(navigation.textContent, 'task-plugin-menu-visible')
        })
        assert.equal(requests.length, 2)
        assert.equal(JSON.parse(saved).admin.task_plugins, true)
        await waitFor(() => {
          assert.equal(client.isMutating(), 0)
          assert.equal(client.isFetching(), 0)
        })
      } finally {
        await act(async () => {
          await client.cancelQueries()
          v.unmount()
          client.clear()
        })
        actions.remove()
      }
    } finally {
      api.defaults.adapter = adapter
      useAuthStore.getState().auth.setUser(previous)
      window.localStorage.removeItem('status')
    }
  })
}
