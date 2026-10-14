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
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import { waitFor } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { api } from '@/lib/api'

const errorKey = 'We could not load the channel health policy.'
const frenchError = 'Impossible de charger la politique des canaux.'
const testEnv = createReactTestEnvironment({
  lng: 'fr',
  fallbackLng: 'en',
  resources: {
    en: { translation: { [errorKey]: errorKey } },
    fr: { translation: { [errorKey]: frenchError } },
  },
})
await testEnv.setup()
const { ChannelHealth } = await import('./index')
after(() => testEnv.teardown())

test('policy errors translate without changing the data cache or repeating the policy request', async () => {
  const originalAdapter = api.defaults.adapter
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  let policyRequests = 0
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'get')
    const policy = config.url === '/api/smart-ops/channel-health/policy'
    if (policy) policyRequests += 1
    return {
      data: { success: true, data: [] },
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
    }
  }
  const queryKey = ['smart-ops', 'channel-health-policy']
  const view = await testEnv.render(
    <QueryClientProvider client={client}>
      <ChannelHealth />
    </QueryClientProvider>
  )
  try {
    await waitFor(() => {
      assert.equal(client.getQueryState(queryKey)?.status, 'error')
      assert.ok(view.container.textContent?.includes(frenchError))
    })
    const cachedError = client.getQueryState(queryKey)?.error
    assert.ok(cachedError instanceof Error)
    assert.equal(cachedError.message, errorKey)
    await act(async () => {
      await testEnv.i18n.changeLanguage('en')
    })
    await waitFor(() => {
      assert.ok(view.container.textContent?.includes(errorKey))
    })
    assert.equal(client.getQueryState(queryKey)?.error, cachedError)
    assert.equal(policyRequests, 1)
    assert.equal(client.getQueryCache().findAll({ queryKey }).length, 1)
  } finally {
    await view.unmount()
    client.clear()
    api.defaults.adapter = originalAdapter
  }
})
