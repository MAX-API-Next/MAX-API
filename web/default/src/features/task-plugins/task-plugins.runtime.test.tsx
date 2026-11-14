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
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after as afterAll, afterEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { PluginEntry, PluginMutation, TaskPluginAPI } from './api'

const env = createReactTestEnvironment()
await env.setup()
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
    validate: async (upload) => ({ ...entry, digest: upload.expected_sha256 }),
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
test('lists real metadata and disables with the observed revision while invalidating prices', async () => {
  const f = fixture()
  f.client.setQueryData(['model-pricing-config', 'fixture'], { saved: true })
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  await v.findByRole('heading', { name: 'Model usage schemas' })
  assert.equal(
    v
      .getByRole('link', { name: 'Open model and source pricing' })
      .getAttribute('href'),
    '/system-settings/billing/model-pricing'
  )
  fireEvent.click(v.getByRole('button', { name: 'Disable' }))
  fireEvent.click(v.getByRole('button', { name: 'Confirm' }))
  await waitFor(() => assert.equal(f.writes.length, 1))
  await v.findByRole('button', { name: 'Enable' })
  assert.equal(f.writes[0].expected_revision, '1')
  assert.equal(
    f.client.getQueryState(['model-pricing-config', 'fixture'])?.isInvalidated,
    true
  )
  assert.ok(v.getByText(/Execution and paid admission remain unavailable/))
  fireEvent.click(
    v.getByRole('link', { name: 'Open model and source pricing' })
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
test('closing details discards a pending revocation before reopening the same version', async () => {
  const f = fixture()
  const v = f.view()
  await v.findByText('Kling Fixture')
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  fireEvent.click(await v.findByRole('button', { name: 'Revoke version' }))
  assert.ok(v.getByRole('button', { name: 'Confirm' }))
  fireEvent.click(v.getByRole('button', { name: 'Close' }))
  await waitFor(() => assert.equal(v.queryByRole('dialog'), null))
  fireEvent.click(v.getByRole('button', { name: 'Details' }))
  await v.findByRole('button', { name: 'Revoke version' })
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
  fireEvent.click(v.getByRole('button', { name: 'Revoke version' }))
  fireEvent.click(v.getByRole('button', { name: 'Confirm' }))
  await waitFor(() => assert.equal(f.writes.length, 1))
  assert.equal(f.writes[0].action, 'revoke')
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
test('late validation is ignored after changing the pin; v2 installation requires acknowledgement', async () => {
  const f = fixture(),
    pending = deferred<PluginEntry>(),
    v = f.view()
  await v.findByText('Kling Fixture')
  f.api.validate = () => pending.promise
  fireEvent.click(v.getByRole('button', { name: 'Install plugin package' }))
  fireEvent.change(v.getByLabelText('Package file'), {
    target: {
      files: [
        { name: 'fixture.json', size: 8, text: async () => 'document-A' },
      ],
    },
  })
  fireEvent.change(v.getByLabelText('Trusted package SHA-256'), {
    target: { value: 'a'.repeat(64) },
  })
  const check = v.getByRole('button', { name: 'Validate package' })
  await waitFor(() =>
    assert.equal((check as HTMLButtonElement).disabled, false)
  )
  fireEvent.click(check)
  fireEvent.change(v.getByLabelText('Trusted package SHA-256'), {
    target: { value: 'b'.repeat(64) },
  })
  await act(async () => pending.resolve(entry))
  assert.equal(v.queryByRole('button', { name: 'Install and activate' }), null)
  f.api.validate = async (upload) => ({
    ...entry,
    digest: upload.expected_sha256,
  })
  fireEvent.click(check)
  const install = await v.findByRole('button', { name: 'Install and activate' })
  assert.equal((install as HTMLButtonElement).disabled, true)
  fireEvent.click(v.getByRole('checkbox'))
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
  fireEvent.click(v.getByRole('button', { name: 'Install plugin package' }))
  const select = await v.findByLabelText('Bundled plugins')
  fireEvent.change(select, { target: { value: 'kling@1.2.0' } })
  fireEvent.change(v.getByLabelText('Trusted package SHA-256'), {
    target: { value: 'b'.repeat(64) },
  })
  await act(async () =>
    download.resolve({
      document: 'late source',
      expected_sha256: 'a'.repeat(64),
    })
  )
  assert.equal(
    (v.getByLabelText('Trusted package SHA-256') as HTMLInputElement).value,
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
  await v.findByText('Task plugin catalog could not be loaded')
  assert.equal(
    (
      v.getByRole('button', {
        name: 'Install plugin package',
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
  fireEvent.click(v.getByRole('button', { name: 'Install plugin package' }))
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
  await panel.findByText('Task plugin catalog could not be loaded')
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

test('empty and legacy sidebar settings expose and persist the task-plugin switch', async () => {
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
    for (const initial of ['', '{"admin":{"enabled":true,"channel":false}}']) {
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
        assert.ok(v.getByText('task-plugin-menu-visible'))
        const toggle = v.getByRole('switch', { name: 'Task Plugins' })
        assert.equal(toggle.getAttribute('aria-checked'), 'true')
        fireEvent.click(toggle)
        fireEvent.click(v.getByRole('button', { name: 'Save sidebar modules' }))
        await v.findByText('task-plugin-menu-hidden')
        assert.equal(requests.length, 1)
        assert.equal(requests[0].key, 'SidebarModulesAdmin')
        assert.equal(JSON.parse(saved).admin.task_plugins, false)
        if (initial) assert.equal(JSON.parse(saved).admin.channel, false)
        assert.equal(parseSidebarModulesAdmin(saved).admin.task_plugins, false)
        await waitFor(() =>
          assert.equal(toggle.getAttribute('aria-checked'), 'false')
        )
        const save = v.getByRole('button', { name: 'Save sidebar modules' })
        await waitFor(() =>
          assert.equal((save as HTMLButtonElement).disabled, false)
        )
        fireEvent.click(toggle)
        fireEvent.click(save)
        await v.findByText('task-plugin-menu-visible')
        assert.equal(requests.length, 2)
        assert.equal(JSON.parse(saved).admin.task_plugins, true)
        await waitFor(() => {
          assert.equal(client.isMutating(), 0)
          assert.equal(client.isFetching(), 0)
        })
      } finally {
        await act(async () => {
          v.unmount()
          client.clear()
        })
        actions.remove()
      }
    }
  } finally {
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(previous)
    window.localStorage.removeItem('status')
  }
})
