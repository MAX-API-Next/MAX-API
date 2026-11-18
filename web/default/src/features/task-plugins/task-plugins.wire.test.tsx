// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from '@tanstack/react-router'
import english from '@/i18n/locales/en.json'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { after, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'

const configuredEndpoint = process.env.TASK_PLUGIN_ADMIN_UI_FIXTURE_URL
const endpoint =
  configuredEndpoint &&
  /^http:\/\/127\.0\.0\.1:[0-9]+$/.test(configuredEndpoint)
    ? configuredEndpoint
    : undefined
if (configuredEndpoint && !endpoint)
  throw new Error(
    'The task plugin UI fixture must use an explicit loopback origin'
  )
const env = createReactTestEnvironment()
await env.setup()
env.i18n.addResourceBundle('en', 'translation', {
  'Search task plugins': english.translation['Search task plugins'],
  'No task plugin packages found':
    english.translation['No task plugin packages found'],
  'Plugin catalog': english.translation['Plugin catalog'],
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
const { render, cleanup, fireEvent, waitFor, within, act } =
  await import('@testing-library/react')
const { TaskPlugins } = await import('./index')
const { api } = await import('@/lib/api')
const originalTimeout = api.defaults.timeout
api.defaults.timeout = 3000
const { taskPluginAPI } = await import('./api')
const { useAuthStore } = await import('@/stores/auth-store')
const activeFixtures = new Set<Promise<void>>()

after(async () => {
  await Promise.allSettled([...activeFixtures])
  cleanup()
  if (originalScrollTo)
    Object.defineProperty(globalThis, 'scrollTo', originalScrollTo)
  else Reflect.deleteProperty(globalThis, 'scrollTo')
  env.teardown()
  api.defaults.timeout = originalTimeout
  if (endpoint)
    await fetch(endpoint + '/plugin-test-stop', {
      method: 'POST',
      signal: AbortSignal.timeout(3000),
    })
})

async function withRootFixture(
  run: (mount: () => ReturnType<typeof render>) => Promise<void>
) {
  const operation = runRootFixture(run)
  activeFixtures.add(operation)
  try {
    await operation
  } finally {
    activeFixtures.delete(operation)
  }
}

async function runRootFixture(
  run: (mount: () => ReturnType<typeof render>) => Promise<void>
) {
  assert.match(endpoint!, /^http:\/\/127\.0\.0\.1:[0-9]+$/)
  const login = await fetch(endpoint + '/plugin-test-login/root', {
    signal: AbortSignal.timeout(3000),
  })
  const cookie = login.headers.get('set-cookie')?.split(';')[0]
  assert.ok(cookie, 'isolated server must issue its own synthetic session')
  const original = api.defaults.baseURL,
    originalCookie = api.defaults.headers.common.Cookie,
    previous = useAuthStore.getState().auth.user
  api.defaults.baseURL = endpoint
  api.defaults.headers.common.Cookie = cookie
  useAuthStore
    .getState()
    .auth.setUser({ id: 59123102, username: 'plugin-root', role: 100 })
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  const mount = () => {
    const rootRoute = createRootRoute({ component: TaskPlugins })
    const router = createRouter({
      routeTree: rootRoute,
      history: createMemoryHistory(),
      defaultPendingMinMs: 0,
    })
    return render(
      <I18nextProvider i18n={env.i18n}>
        <QueryClientProvider client={client}>
          <RouterContextProvider router={router}>
            <TaskPlugins />
          </RouterContextProvider>
        </QueryClientProvider>
      </I18nextProvider>
    )
  }
  try {
    await run(mount)
  } finally {
    try {
      await waitFor(() => assert.equal(client.isMutating(), 0), {
        timeout: 3000,
      })
    } finally {
      try {
        await act(async () => {
          await client.cancelQueries()
          cleanup()
          client.clear()
          await new Promise<void>((resolve) => setTimeout(resolve, 0))
        })
      } finally {
        api.defaults.baseURL = original
        if (originalCookie === undefined)
          delete api.defaults.headers.common.Cookie
        else api.defaults.headers.common.Cookie = originalCookie
        useAuthStore.getState().auth.setUser(previous)
      }
    }
  }
}

function packageDocument(key: string, name: string, model: string) {
  return JSON.stringify({
    format_version: 1,
    manifest: {
      APIVersion: 1,
      Key: key,
      Name: name,
      Version: '1.0.0',
      Models: [model],
      UsageSchema: { units: { Type: 'number', Unit: 'credit' } },
    },
    source: 'opaque synthetic data; no JS execution',
  })
}

test(
  'the mounted UI imports without an external hash through actual Root API and SQLite catalog',
  { skip: !endpoint },
  async () => {
    await withRootFixture(async (mount) => {
      const before = await taskPluginAPI.read()
      const view = mount()
      const content = within(view.container)
      await content.findByRole('button', { name: 'Import API extension' })
      const importButton = content.getByRole('button', {
        name: 'Import API extension',
      }) as HTMLButtonElement
      await waitFor(() => assert.equal(importButton.disabled, false))
      fireEvent.click(importButton)
      const importDialog = await view.findByRole('dialog')
      const install = within(importDialog)
      const document =
        '\uFEFF' +
        packageDocument(
          'wire-import',
          'Wire Import Fixture',
          'wire-import-model'
        )
      const pin = createHash('sha256').update(document).digest('hex')
      fireEvent.change(install.getByLabelText('Package file'), {
        target: {
          files: [
            {
              name: 'wire.json',
              size: Buffer.byteLength(document),
              arrayBuffer: async () =>
                new TextEncoder().encode(document).buffer,
            },
          ],
        },
      })
      assert.equal(
        (install.getByLabelText('SHA-256 (optional)') as HTMLInputElement)
          .value,
        ''
      )
      const validate = install.getByRole('button', { name: 'Validate package' })
      await waitFor(() =>
        assert.equal((validate as HTMLButtonElement).disabled, false)
      )
      fireEvent.click(validate)
      await install.findByText(`Submitted content SHA-256: ${pin}`)
      fireEvent.click(
        install.getByRole('checkbox', {
          name: 'All catalog nodes support UTF-8 BOM packages. Older nodes may suspend their entire plugin host.',
        })
      )
      fireEvent.click(
        await install.findByRole('button', { name: 'Install and activate' })
      )
      await waitFor(() => assert.equal(importDialog.isConnected, false))
      await content.findByText('Wire Import Fixture')
      const stored = await taskPluginAPI.read()
      const entry = stored.entries.find((entry) => entry.key === 'wire-import')
      assert.ok(entry)
      assert.equal(stored.revision, String(BigInt(before.revision) + 1n))
      assert.equal(entry.enabled, true)
      assert.equal(entry.digest, pin)
      assert.equal(entry.execution_ready, false)
      assert.equal(entry.admission_ready, false)
    })
  }
)

test(
  'the mounted UI disables an installed version through actual Root API and SQLite catalog',
  { skip: !endpoint },
  async () => {
    await withRootFixture(async (mount) => {
      const before = await taskPluginAPI.read()
      const document = packageDocument(
        'wire-disable',
        'Wire Disable Fixture',
        'wire-disable-model'
      )
      const pin = createHash('sha256').update(document).digest('hex')
      const receipt = await taskPluginAPI.mutate({
        expected_revision: before.revision,
        action: 'install',
        document,
      })
      const view = mount()
      const content = within(view.container)
      const heading = await content.findByRole('heading', {
        name: 'Wire Disable Fixture',
      })
      const card = heading.closest('article')
      assert.ok(card)
      fireEvent.click(within(card).getByRole('button', { name: /^Details\b/ }))
      const details = within(await view.findByRole('dialog'))
      fireEvent.click(await details.findByRole('button', { name: 'Disable' }))
      fireEvent.click(details.getByRole('button', { name: 'Confirm' }))
      await details.findByRole('button', { name: 'Enable' })
      const stored = await taskPluginAPI.read()
      const entry = stored.entries.find((entry) => entry.key === 'wire-disable')
      assert.ok(entry)
      assert.equal(stored.revision, String(BigInt(receipt.revision) + 1n))
      assert.equal(entry.enabled, false)
      assert.equal(entry.digest, pin)
      assert.equal(entry.execution_ready, false)
      assert.equal(entry.admission_ready, false)
    })
  }
)
