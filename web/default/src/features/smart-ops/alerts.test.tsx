/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import { waitFor, within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { api } from '@/lib/api'
import { ActiveAlerts } from './alerts'

const LOAD_ERROR_KEY = 'We could not load active alerts.'
const testEnv = createReactTestEnvironment({
  resources: {
    en: { translation: { [LOAD_ERROR_KEY]: LOAD_ERROR_KEY } },
    fr: {
      translation: {
        [LOAD_ERROR_KEY]: 'Impossible de charger les alertes actives.',
      },
    },
  },
})

before(() => testEnv.setup())

after(() => testEnv.teardown())

function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

describe('SmartOps active alerts', () => {
  test('polls the administrator alert endpoint and renders active host pressure', async () => {
    const originalGet = api.get
    const urls: string[] = []
    api.get = (async (url) => {
      urls.push(String(url))
      return {
        data: {
          success: true,
          data: [
            {
              key: 'system_cpu',
              status: 'firing',
              severity: 'warning',
              component: 'system',
              node: 'node-a',
              current_value: 95.25,
              threshold: 90,
              observed_at: '2026-08-22T08:00:00Z',
              message: 'CPU usage exceeded the threshold',
            },
          ],
        },
      }
    }) as typeof api.get

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        assert.equal(urls[0], '/api/smart-ops/alerts')
        const text = view.container.textContent ?? ''
        assert.ok(text.includes('CPU usage'))
        assert.ok(text.includes('node-a'))
        assert.ok(text.includes('95.3%'))
        assert.ok(text.includes('90.0%'))
        assert.ok(text.includes('Firing'))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('formats percentages with the active locale', async () => {
    const originalGet = api.get
    await testEnv.i18n.changeLanguage('fr')
    api.get = (async () => ({
      data: {
        success: true,
        data: [
          {
            key: 'system_memory',
            status: 'firing',
            severity: 'warning',
            component: 'system',
            node: 'node-fr',
            current_value: 95.25,
            threshold: 90,
            observed_at: '2026-08-22T08:00:00Z',
            message: 'Memory usage exceeded the threshold',
          },
        ],
      },
    })) as typeof api.get

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        assert.ok((view.container.textContent ?? '').includes('95,3%'))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
      await testEnv.i18n.changeLanguage('en')
    }
  })

  test('shows a healthy empty state when no incident is active', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: { success: true, data: [] },
    })) as typeof api.get

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        assert.ok(
          (view.container.textContent ?? '').includes('No active alerts.')
        )
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('shows a localized fallback for an unsuccessful response', async () => {
    const originalGet = api.get
    await testEnv.i18n.changeLanguage('fr')
    api.get = (async () => ({
      data: { success: false, data: [] },
    })) as typeof api.get

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        const text = view.container.textContent ?? ''
        assert.ok(text.includes('Impossible de charger les alertes actives.'))
        assert.ok(!text.includes(LOAD_ERROR_KEY))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
      await testEnv.i18n.changeLanguage('en')
    }
  })

  test('shows a retryable error state when the alert endpoint fails', async () => {
    const originalGet = api.get
    let requestCount = 0
    api.get = (async () => {
      requestCount += 1
      throw new Error('temporary alert endpoint failure')
    }) as typeof api.get

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      let retryButton: HTMLElement | undefined
      await waitFor(() => {
        const text = view.container.textContent ?? ''
        assert.ok(text.includes('We could not load active alerts.'))
        assert.ok(text.includes('temporary alert endpoint failure'))
        retryButton = within(view.container).getByRole('button', {
          name: 'Retry',
        })
      })
      assert.ok(retryButton)
      const requestsBeforeRetry = requestCount
      await view.click(retryButton)
      await waitFor(() => assert.ok(requestCount > requestsBeforeRetry))
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })
})
