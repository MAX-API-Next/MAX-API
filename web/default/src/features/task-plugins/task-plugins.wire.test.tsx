// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { after, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'

const endpoint = process.env.TASK_PLUGIN_ADMIN_UI_FIXTURE_URL
const env = createReactTestEnvironment()
await env.setup()
const { render, cleanup, fireEvent, waitFor } =
  await import('@testing-library/react')
const { TaskPlugins } = await import('./index')
const { api } = await import('@/lib/api')
const { useAuthStore } = await import('@/stores/auth-store')
after(() => {
  cleanup()
  env.teardown()
})

test(
  'the mounted UI installs and disables through actual Root API and SQLite catalog',
  { skip: !endpoint },
  async () => {
    assert.match(endpoint!, /^http:\/\/127\.0\.0\.1:[0-9]+$/)
    const login = await fetch(endpoint + '/plugin-test-login/root')
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
    try {
      const view = render(
        <I18nextProvider i18n={env.i18n}>
          <QueryClientProvider client={client}>
            <TaskPlugins />
          </QueryClientProvider>
        </I18nextProvider>
      )
      await view.findByText('No task plugin packages found')
      fireEvent.click(
        view.getByRole('button', { name: 'Install plugin package' })
      )
      const document = JSON.stringify({
        format_version: 1,
        manifest: {
          APIVersion: 1,
          Key: 'wire-fixture',
          Name: 'Wire Fixture',
          Version: '1.0.0',
          Models: ['wire-model'],
          UsageSchema: { units: { Type: 'number', Unit: 'credit' } },
        },
        source: 'opaque synthetic data; no JS execution',
      })
      const pin = createHash('sha256').update(document).digest('hex')
      fireEvent.change(view.getByLabelText('Package file'), {
        target: {
          files: [
            {
              name: 'wire.json',
              size: Buffer.byteLength(document),
              text: async () => document,
            },
          ],
        },
      })
      fireEvent.change(view.getByLabelText('Trusted package SHA-256'), {
        target: { value: pin },
      })
      const validate = view.getByRole('button', { name: 'Validate package' })
      await waitFor(() =>
        assert.equal((validate as HTMLButtonElement).disabled, false)
      )
      fireEvent.click(validate)
      fireEvent.click(
        await view.findByRole('button', { name: 'Install and activate' })
      )
      await view.findByText('Wire Fixture')
      fireEvent.click(view.getByRole('button', { name: 'Details' }))
      fireEvent.click(await view.findByRole('button', { name: 'Disable' }))
      fireEvent.click(view.getByRole('button', { name: 'Confirm' }))
      await view.findByRole('button', { name: 'Enable' })
      const { taskPluginAPI } = await import('./api')
      const stored = await taskPluginAPI.read()
      assert.equal(stored.revision, '2')
      assert.equal(stored.entries[0].enabled, false)
      assert.equal(stored.entries[0].digest, pin)
      assert.equal(stored.entries[0].execution_ready, false)
      assert.equal(stored.entries[0].admission_ready, false)
    } finally {
      cleanup()
      api.defaults.baseURL = original
      if (originalCookie === undefined)
        delete api.defaults.headers.common.Cookie
      else api.defaults.headers.common.Cookie = originalCookie
      useAuthStore.getState().auth.setUser(previous)
      await fetch(endpoint + '/plugin-test-stop', { method: 'POST' })
    }
  }
)
