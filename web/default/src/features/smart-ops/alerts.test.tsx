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
import { after, describe, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import type { BillingSettlementReconciliationData } from './types'

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

await testEnv.setup()
const { ActiveAlerts } = await import('./alerts')

after(() => testEnv.teardown())

function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function emptyReconciliationData(): BillingSettlementReconciliationData {
  return {
    total_count: 0,
    pending_count: 0,
    manual_count: 0,
    open_alert_count: 0,
    reviewed_count: 0,
    blocking_record_count: 0,
    blocked_user_count: 0,
    block_user_by_default: true,
    oldest_created_at: 0,
    truncated: false,
    generated_at: 1788106455,
    items: [],
  }
}

describe('SmartOps active alerts', () => {
  test('polls the administrator alert endpoint and renders active host pressure', async () => {
    const originalGet = api.get
    const urls: string[] = []
    api.get = (async (url) => {
      urls.push(String(url))
      if (url === '/api/smart-ops/billing-settlements') {
        return { data: { success: true, data: emptyReconciliationData() } }
      }
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
    api.get = (async (url) => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? { success: true, data: emptyReconciliationData() }
          : {
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

  test('renders billing backlog values and read-only reconciliation evidence', async () => {
    const originalGet = api.get
    const urls: string[] = []
    api.get = (async (url) => {
      urls.push(String(url))
      if (url === '/api/smart-ops/billing-settlements') {
        return {
          data: {
            success: true,
            data: {
              total_count: 48,
              pending_count: 17,
              manual_count: 31,
              open_alert_count: 1,
              reviewed_count: 47,
              blocking_record_count: 1,
              blocked_user_count: 9,
              block_user_by_default: true,
              oldest_created_at: 1786032544,
              truncated: false,
              generated_at: 1788106455,
              items: [
                {
                  id: 71,
                  operation_key: 'request:billing-request-71:finalize',
                  status: 'manual',
                  source: 'wallet',
                  user_id: 42,
                  subscription_id: 0,
                  token_id: 84,
                  task_id: 0,
                  funding_delta: 2500,
                  applied_funding_delta: 0,
                  token_delta: 2500,
                  applied_token_delta: 0,
                  attempts: 1,
                  last_error: 'user quota is not enough',
                  next_attempt: 0,
                  created_at: 1786032544,
                  updated_at: 1786032544,
                  reconciliation_reviewed_at: 0,
                  reconciliation_reviewed_by: 0,
                  reconciliation_review_note: '',
                  user_blocking_override: false,
                  record_blocks_user: false,
                  blocks_user: true,
                },
              ],
            },
          },
        }
      }
      return {
        data: {
          success: true,
          data: [
            {
              key: 'billing_settlement_backlog',
              status: 'firing',
              severity: 'warning',
              component: 'billing',
              node: 'XG',
              current_value: 48,
              threshold: 2121911,
              observed_at: '2026-08-30T16:14:15Z',
              message: 'billing backlog',
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
        const text = view.container.textContent ?? ''
        assert.ok(urls.includes('/api/smart-ops/alerts'))
        assert.ok(urls.includes('/api/smart-ops/billing-settlements'))
        assert.ok(text.includes('Billing reconciliation backlog'))
        assert.ok(text.includes('48 records'))
        assert.ok(!text.includes('4,800.0%'))
        assert.ok(text.includes('Pending: 17'))
        assert.ok(text.includes('Manual: 31'))
        assert.ok(text.includes('Blocked users: 9'))
        assert.ok(text.includes('request:billing-request-71:finalize'))
        assert.ok(text.includes('user quota is not enough'))
        assert.ok(text.includes('User blocked'))
        assert.ok(text.includes('Record policy: allow'))
      })
      const reviewButton = within(view.container).getByRole('button', {
        name: 'Review and close',
      })
      await view.click(reviewButton)
      let dialog: HTMLElement | undefined
      await waitFor(() => {
        dialog = within(document.body).getByRole('dialog', {
          name: 'Review billing reconciliation',
        })
      })
      assert.ok(dialog)
      const noteInput = within(dialog).getByRole('textbox', {
        name: 'Review note',
      })
      assert.equal(noteInput.getAttribute('aria-invalid'), 'true')
      assert.ok(
        (dialog.textContent ?? '').includes(
          'Review note must contain between 3 and 1000 characters.'
        )
      )
      assert.equal(
        within(dialog)
          .getByRole('button', { name: 'Close alert' })
          .hasAttribute('disabled'),
        true
      )
      assert.equal(
        within(dialog)
          .getByRole('button', { name: 'Allow user to continue' })
          .getAttribute('aria-pressed'),
        'true'
      )
      await view.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('keeps the billing alert visible when reconciliation details fail', async () => {
    const originalGet = api.get
    api.get = (async (url) => {
      if (url === '/api/smart-ops/billing-settlements') {
        throw new Error('temporary reconciliation projection failure')
      }
      return {
        data: {
          success: true,
          data: [
            {
              key: 'billing_settlement_backlog',
              status: 'firing',
              severity: 'warning',
              component: 'billing',
              node: 'XG',
              current_value: 48,
              threshold: 2121911,
              observed_at: '2026-08-30T16:14:15Z',
              message: 'billing backlog',
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
        const text = view.container.textContent ?? ''
        assert.ok(text.includes('Billing reconciliation backlog'))
        assert.ok(text.includes('48 records'))
        assert.ok(
          text.includes('We could not load billing reconciliation details.')
        )
        assert.ok(text.includes('temporary reconciliation projection failure'))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('routes malformed reconciliation payloads to the existing error state', async (): Promise<void> => {
    const originalGet = api.get
    api.get = (async (url: string): Promise<unknown> => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? {
              success: true,
              data: { ...emptyReconciliationData(), items: [null] },
            }
          : { success: true, data: [] },
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
        assert.ok(
          text.includes('We could not load billing reconciliation details.')
        )
        assert.ok(!text.includes('No unresolved reconciliation records.'))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('loads reviewed reconciliation without an active alert and submits administrator controls', async () => {
    const originalUser = useAuthStore.getState().auth.user
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: 100,
    })
    const originalGet = api.get
    const originalPut = api.put
    const originalPost = api.post
    const writes: Array<{ method: string; url: string; data: unknown }> = []
    let reconciliationRequests = 0
    let blockUserByDefault = true
    api.get = (async (url) => {
      if (url === '/api/smart-ops/billing-settlements') {
        reconciliationRequests += 1
        return {
          data: {
            success: true,
            data: {
              ...emptyReconciliationData(),
              total_count: 1,
              pending_count: 1,
              open_alert_count: 0,
              reviewed_count: 2,
              blocking_record_count: 0,
              blocked_user_count: 0,
              block_user_by_default: blockUserByDefault,
              items: [
                {
                  id: 91,
                  operation_key: 'request:billing-request-91:finalize',
                  status: 'pending',
                  source: 'wallet',
                  user_id: 51,
                  subscription_id: 0,
                  token_id: 0,
                  task_id: 0,
                  funding_delta: 100,
                  applied_funding_delta: 0,
                  token_delta: 100,
                  applied_token_delta: 0,
                  attempts: 2,
                  last_error: 'quota changed',
                  next_attempt: 1788106500,
                  created_at: 1786032544,
                  updated_at: 1786032544,
                  reconciliation_reviewed_at: 1788106400,
                  reconciliation_reviewed_by: 7,
                  reconciliation_review_note: '🙂🙂🙂',
                  user_blocking_override: false,
                  record_blocks_user: false,
                  blocks_user: false,
                },
              ],
            },
          },
        }
      }
      return { data: { success: true, data: [] } }
    }) as typeof api.get
    api.put = (async (url, data) => {
      writes.push({ method: 'PUT', url: String(url), data })
      blockUserByDefault = (data as { block_user_by_default: boolean })
        .block_user_by_default
      return { data: { success: true } }
    }) as typeof api.put
    api.post = (async (url, data) => {
      writes.push({ method: 'POST', url: String(url), data })
      return { data: { success: true } }
    }) as typeof api.post

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      let policySwitch: HTMLElement | undefined
      let reviewButton: HTMLElement | undefined
      await waitFor(() => {
        const screen = within(view.container)
        assert.ok(
          (view.container.textContent ?? '').includes('No active alerts.')
        )
        assert.ok(
          (view.container.textContent ?? '').includes(
            'request:billing-request-91:finalize'
          )
        )
        assert.ok(
          (view.container.textContent ?? '').includes('Reviewed records: 2')
        )
        policySwitch = screen.getByRole('switch', {
          name: 'Block affected users by default',
        })
        reviewButton = screen.getByRole('button', {
          name: 'Edit review',
        })
      })

      assert.ok(policySwitch)
      await view.click(policySwitch)
      await waitFor(() => {
        assert.deepEqual(writes[0], {
          method: 'PUT',
          url: '/api/smart-ops/billing-settlements/blocking-policy',
          data: { block_user_by_default: false },
        })
        assert.ok(reconciliationRequests > 1)
      })

      reviewButton = within(view.container).getByRole('button', {
        name: 'Edit review',
      })
      await view.click(reviewButton)
      let dialog: HTMLElement | undefined
      await waitFor(() => {
        dialog = within(document.body).getByRole('dialog')
      })
      assert.ok(dialog)
      const noteInput = within(dialog).getByRole('textbox', {
        name: 'Review note',
      }) as HTMLTextAreaElement
      assert.equal(noteInput.value, '🙂🙂🙂')
      assert.ok((dialog.textContent ?? '').includes('3 / 1000 characters'))
      const closeAlertButton = within(dialog).getByRole('button', {
        name: 'Save review',
      })
      await waitFor(() =>
        assert.equal(closeAlertButton.hasAttribute('disabled'), false)
      )
      const reconciliationRequestsBeforeReview = reconciliationRequests
      await view.click(closeAlertButton)

      await waitFor(() => {
        assert.deepEqual(writes[1], {
          method: 'POST',
          url: '/api/smart-ops/billing-settlements/91/review',
          data: {
            block_user: false,
            note: '🙂🙂🙂',
          },
        })
        assert.ok(reconciliationRequests > reconciliationRequestsBeforeReview)
      })
    } finally {
      api.get = originalGet
      api.put = originalPut
      api.post = originalPost
      await view.unmount()
      queryClient.clear()
      useAuthStore.getState().auth.setUser(originalUser)
    }
  })

  test('keeps the global blocking policy read only for non-root administrators', async () => {
    const originalUser = useAuthStore.getState().auth.user
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'admin',
      role: 10,
    })
    const originalGet = api.get
    const originalPut = api.put
    let policyWrites = 0
    api.get = (async (url) => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? { success: true, data: emptyReconciliationData() }
          : { success: true, data: [] },
    })) as typeof api.get
    api.put = (async () => {
      policyWrites += 1
      return { data: { success: true } }
    }) as typeof api.put

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      let policySwitch: HTMLElement | undefined
      await waitFor(() => {
        policySwitch = within(view.container).getByRole('switch', {
          name: 'Block affected users by default',
        })
        assert.ok(
          (view.container.textContent ?? '').includes(
            'Only root administrators can change the default blocking policy.'
          )
        )
      })
      assert.ok(policySwitch)
      assert.equal(policySwitch.hasAttribute('data-disabled'), true)
      assert.equal(policyWrites, 0)
    } finally {
      api.get = originalGet
      api.put = originalPut
      await view.unmount()
      queryClient.clear()
      useAuthStore.getState().auth.setUser(originalUser)
    }
  })

  test('shows a healthy empty state when no incident is active', async () => {
    const originalGet = api.get
    api.get = (async (url) => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? { success: true, data: emptyReconciliationData() }
          : { success: true, data: [] },
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
    api.get = (async (url) => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? { success: true, data: emptyReconciliationData() }
          : { success: false, data: [] },
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
    api.get = (async (url) => {
      if (url === '/api/smart-ops/billing-settlements') {
        return { data: { success: true, data: emptyReconciliationData() } }
      }
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
