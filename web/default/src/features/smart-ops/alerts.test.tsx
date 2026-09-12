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
import { act, useMemo, useState, type ReactElement } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import en from '@/i18n/locales/en.json'
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import vi from '@/i18n/locales/vi.json'
import zh from '@/i18n/locales/zh.json'
import { createReactTestEnvironment } from '@/test/react'
import { fireEvent, waitFor, within } from '@testing-library/react'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { after, describe, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { api } from '@/lib/api'
import {
  completeManualTaskBillingSettlement,
  completeManualTaskBillingSettlements,
} from './api'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
} from './types'

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
const { BillingSettlementEvidence } =
  await import('./components/billing-settlement-evidence')

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
    blocking_record_count: 0,
    blocked_user_count: 0,
    block_user_by_default: true,
    oldest_created_at: 0,
    truncated: false,
    generated_at: 1788106455,
    items: [],
  }
}

function ManualSettlementEvidenceHarness(): ReactElement {
  const [failed, setFailed] = useState(false)
  const data = useMemo<BillingSettlementReconciliationData>(
    () => ({
      ...emptyReconciliationData(),
      total_count: 1,
      manual_count: 1,
      open_alert_count: 1,
      items: [
        {
          id: 701,
          revision: 4,
          operation_key: 'task:9701:finalize',
          status: 'manual',
          source: 'wallet',
          user_id: 53,
          subscription_id: 0,
          token_id: 54,
          task_id: 9701,
          task_quota: 100,
          task_quota_target: 100,
          requires_manual_completion: true,
          zero_quota_eligible: false,
          funding_delta: 0,
          applied_funding_delta: 0,
          token_delta: 0,
          applied_token_delta: 0,
          attempts: 0,
          last_error: 'provider usage needs verification',
          next_attempt: 0,
          created_at: 1786032545,
          updated_at: 1786032545,
          reconciliation_reviewed_at: 0,
          reconciliation_reviewed_by: 0,
          reconciliation_review_note: '',
          user_blocking_override: null,
          record_blocks_user: false,
          blocks_user: false,
        },
      ],
    }),
    []
  )
  const error = useMemo(() => new Error('temporary reconciliation failure'), [])

  return (
    <>
      <button type='button' onClick={() => setFailed(true)}>
        Cause reconciliation error
      </button>
      <BillingSettlementEvidence
        canCompleteManualTask
        canUpdateBlockingPolicy
        data={data}
        error={failed ? error : null}
        loading={false}
        onRetry={() => undefined}
      />
    </>
  )
}

describe('SmartOps active alerts', () => {
  test('keeps zero-quota confirmation wording pluralized in every locale', async (): Promise<void> => {
    const key =
      'This will settle {{displayCount}} selected MiniMax-H3 task(s) with an explicit final quota of 0 and refund the unused reservation. Continue only after verifying the provider result.'
    const locales = { en, fr, ja, ru, vi, zh }
    for (const [lng, resource] of Object.entries(locales)) {
      const instance = i18next.createInstance()
      await instance.init({
        lng,
        fallbackLng: false,
        resources: { [lng]: resource },
        interpolation: { escapeValue: false },
      })
      const zeroMessage = instance.t(key, {
        count: 0,
        displayCount: '0',
      })
      const distinctMessage = instance.t(key, {
        count: 3,
        displayCount: '3',
      })
      assert.notEqual(zeroMessage, distinctMessage)
      assert.ok(zeroMessage.includes('0'))
      assert.ok(distinctMessage.includes('3'))
      for (const count of [1, 2]) {
        const message = instance.t(key, {
          count,
          displayCount: String(count),
        })
        assert.ok(message.includes(String(count)))
        assert.doesNotMatch(message, /task\(s\)/)
        if (lng === 'ru' && count === 2) {
          assert.match(message, /выбранных задач MiniMax-H3/)
        }
      }
    }
  })

  test('posts the exact manual task settlement contract', async (): Promise<void> => {
    const originalPost = api.post
    const writes: Array<{ url: string; data: unknown; config: unknown }> = []
    api.post = (async (
      url: string,
      data: unknown,
      config: unknown
    ): Promise<unknown> => {
      writes.push({ url: String(url), data, config })
      return { data: { success: true, data: { actual_quota: 0 } } }
    }) as typeof api.post

    try {
      const response = await completeManualTaskBillingSettlement(93, {
        revision: 2,
        actual_quota: 0,
      })

      assert.equal(response.success, true)
      assert.deepEqual(writes, [
        {
          url: '/api/smart-ops/billing-settlements/93/complete-task',
          data: {
            revision: 2,
            actual_quota: 0,
          },
          config: { skipBusinessError: true, skipErrorHandler: true },
        },
      ])
    } finally {
      api.post = originalPost
    }
  })

  test('passes a non-zero exact quota to the manual settlement endpoint', async (): Promise<void> => {
    const originalPost = api.post
    const writes: Array<{ url: string; data: unknown }> = []
    api.post = (async (url: string, data: unknown): Promise<unknown> => {
      writes.push({ url: String(url), data })
      return { data: { success: true, data: { actual_quota: 40 } } }
    }) as typeof api.post

    try {
      const response = await completeManualTaskBillingSettlement(93, {
        revision: 2,
        actual_quota: 40,
      })

      assert.equal(response.success, true)
      assert.deepEqual(writes, [
        {
          url: '/api/smart-ops/billing-settlements/93/complete-task',
          data: {
            revision: 2,
            actual_quota: 40,
          },
        },
      ])
    } finally {
      api.post = originalPost
    }
  })

  test('passes per-task exact quotas, including an explicit zero, to the batch endpoint', async (): Promise<void> => {
    const originalPost = api.post
    const writes: Array<{
      url: string
      data: unknown
      config: unknown
    }> = []
    api.post = (async (
      url: string,
      data: unknown,
      config: unknown
    ): Promise<unknown> => {
      writes.push({ url: String(url), data, config })
      return { data: { success: true, data: { completed_count: 2 } } }
    }) as typeof api.post

    try {
      const response = await completeManualTaskBillingSettlements({
        items: [
          { id: 93, revision: 2, actual_quota: 0 },
          { id: 94, revision: 7, actual_quota: 40 },
        ],
      })

      assert.equal(response.success, true)
      assert.deepEqual(writes, [
        {
          url: '/api/smart-ops/billing-settlements/complete-tasks',
          data: {
            items: [
              { id: 93, revision: 2, actual_quota: 0 },
              { id: 94, revision: 7, actual_quota: 40 },
            ],
          },
          config: { skipBusinessError: true, skipErrorHandler: true },
        },
      ])
    } finally {
      api.post = originalPost
    }
  })

  test('polls the administrator alert endpoint and renders active host pressure', async (): Promise<void> => {
    const originalGet = api.get
    const urls: string[] = []
    api.get = (async (url: string): Promise<unknown> => {
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

  test('formats percentages with the active locale', async (): Promise<void> => {
    const originalGet = api.get
    await testEnv.i18n.changeLanguage('fr')
    api.get = (async (url: string): Promise<unknown> => ({
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
        assert.ok((view.container.textContent ?? '').includes('95,3\u00a0%'))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
      await testEnv.i18n.changeLanguage('en')
    }
  })

  test('renders billing backlog values and read-only reconciliation evidence', async (): Promise<void> => {
    const originalGet = api.get
    const urls: string[] = []
    api.get = (async (url: string): Promise<unknown> => {
      urls.push(String(url))
      if (url === '/api/smart-ops/billing-settlements') {
        return {
          data: {
            success: true,
            data: {
              total_count: 24,
              pending_count: 1,
              manual_count: 23,
              open_alert_count: 24,
              blocking_record_count: 1,
              blocked_user_count: 9,
              block_user_by_default: true,
              oldest_created_at: 1786032544,
              truncated: false,
              generated_at: 1788106455,
              items: [
                {
                  id: 71,
                  revision: 3,
                  operation_key: 'request:billing-request-71:finalize',
                  status: 'manual',
                  source: 'wallet',
                  user_id: 42,
                  subscription_id: 0,
                  token_id: 84,
                  task_id: 0,
                  task_quota: 0,
                  task_quota_target: 0,
                  requires_manual_completion: false,
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
              current_value: 24,
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
        assert.ok(text.includes('24 records'))
        assert.ok(!text.includes('4,800.0%'))
        assert.ok(text.includes('Open alerts: 24'))
        assert.ok(!text.includes('Reviewed records'))
        assert.ok(text.includes('Open pending settlements: 1'))
        assert.ok(text.includes('Open manual settlements: 23'))
        assert.ok(!text.includes('Manual settlements: 50'))
        assert.ok(!text.includes('Pending: 1'))
        assert.ok(text.includes('Blocked users: 9'))
        assert.ok(text.includes('request:billing-request-71:finalize'))
        assert.ok(text.includes('user quota is not enough'))
        assert.ok(text.includes('User blocked'))
        assert.ok(text.includes('Record policy: allow'))
      })
      const reviewButton = within(view.container).getByRole('button', {
        name: 'Review and close',
      })
      assert.equal(reviewButton.hasAttribute('disabled'), false)
      assert.equal(within(view.container).queryByRole('textbox'), null)
      assert.equal(within(document.body).queryByRole('dialog'), null)
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('keeps the billing alert visible when reconciliation details fail', async (): Promise<void> => {
    const originalGet = api.get
    api.get = (async (url: string): Promise<unknown> => {
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

  test('keeps the manual settlement dialog mounted when reconciliation fails', async (): Promise<void> => {
    const htmlElementPrototype = window.HTMLElement
      .prototype as typeof window.HTMLElement.prototype & {
      attachEvent?: (name: string, listener: EventListener) => void
      detachEvent?: (name: string, listener: EventListener) => void
    }
    // React's async rendering path probes the legacy IE event API in this test
    // environment; emulate it on the shared prototype and remove it below.
    htmlElementPrototype.attachEvent = function (name, listener) {
      this.addEventListener(name.replace(/^on/, ''), listener)
    }
    htmlElementPrototype.detachEvent = function (name, listener) {
      this.removeEventListener(name.replace(/^on/, ''), listener)
    }
    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ManualSettlementEvidenceHarness />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        assert.ok(
          within(view.container).getByRole('button', {
            name: 'Enter exact quota',
          })
        )
      })
      const causeErrorButton = view.container.querySelector(
        'button'
      ) as HTMLButtonElement
      assert.equal(causeErrorButton.textContent, 'Cause reconciliation error')
      await view.click(
        within(view.container).getByRole('button', {
          name: 'Enter exact quota',
        })
      )

      const dialog = await waitFor(() => {
        const currentDialog = document.body.querySelector(
          '[role="dialog"]'
        ) as HTMLElement
        assert.ok(currentDialog)
        return currentDialog
      })
      assert.ok(dialog.querySelector('#manual-task-actual-quota'))

      await view.click(causeErrorButton)
      await waitFor(() => {
        assert.ok(
          (view.container.textContent ?? '').includes(
            'We could not load billing reconciliation details.'
          )
        )
      })

      const currentDialog = document.body.querySelector(
        '[role="dialog"]'
      ) as HTMLElement
      assert.ok(currentDialog)
      assert.equal(currentDialog, dialog)
      assert.ok(
        (currentDialog.textContent ?? '').includes(
          'This reconciliation record changed while the dialog was open.'
        )
      )
      const submitButton = currentDialog.querySelector(
        'button[type="submit"]'
      ) as HTMLButtonElement
      assert.ok(submitButton)
      assert.equal(submitButton.hasAttribute('disabled'), true)
    } finally {
      delete htmlElementPrototype.attachEvent
      delete htmlElementPrototype.detachEvent
      queryClient.clear()
      await view.unmount()
    }
  })

  test('blocks batch exact settlement after reconciliation refresh fails', async (): Promise<void> => {
    const originalUser = useAuthStore.getState().auth.user
    const originalSystemConfigLoading = useSystemConfigStore.getState().loading
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: 100,
    })
    useSystemConfigStore.getState().setLoading(false)
    const htmlElementPrototype = window.HTMLElement
      .prototype as typeof window.HTMLElement.prototype & {
      attachEvent?: (name: string, listener: EventListener) => void
      detachEvent?: (name: string, listener: EventListener) => void
    }
    htmlElementPrototype.attachEvent = function (name, listener) {
      this.addEventListener(name.replace(/^on/, ''), listener)
    }
    htmlElementPrototype.detachEvent = function (name, listener) {
      this.removeEventListener(name.replace(/^on/, ''), listener)
    }
    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ManualSettlementEvidenceHarness />
      </QueryClientProvider>
    )

    try {
      await view.click(
        await waitFor(() =>
          within(view.container).getByRole('checkbox', {
            name: 'Select billing reconciliation alert 701',
          })
        )
      )
      await view.click(
        within(view.container).getByRole('button', {
          name: 'Manually settle selected task quotas (1)',
        })
      )
      const input = await waitFor(() => {
        const element = document.getElementById(
          'manual-task-actual-quota-701'
        ) as HTMLInputElement | null
        assert.ok(element)
        return element
      })
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        'value'
      )?.set
      assert.ok(setter)
      await act(async () => {
        setter.call(input, '40')
        fireEvent.input(input)
        fireEvent.change(input)
      })
      const submitButton = await waitFor(() => {
        const button = within(document.body).getByRole('button', {
          name: 'Apply exact settlements',
        })
        assert.equal((button as HTMLButtonElement).disabled, false)
        return button
      })

      const causeErrorButton = view.container.querySelector(
        'button'
      ) as HTMLButtonElement
      assert.equal(causeErrorButton.textContent, 'Cause reconciliation error')
      await view.click(causeErrorButton)
      await waitFor(() => {
        assert.ok(
          (view.container.textContent ?? '').includes(
            'We could not load billing reconciliation details.'
          )
        )
        assert.ok(
          (
            document.body.querySelector('[role="dialog"]')?.textContent ?? ''
          ).includes(
            'This reconciliation record changed while the dialog was open.'
          )
        )
      })
      assert.equal((submitButton as HTMLButtonElement).disabled, true)
    } finally {
      delete htmlElementPrototype.attachEvent
      delete htmlElementPrototype.detachEvent
      queryClient.clear()
      await view.unmount()
      useAuthStore.getState().auth.setUser(originalUser)
      useSystemConfigStore.getState().setLoading(originalSystemConfigLoading)
    }
  })

  test('batch closes selected active reconciliation alerts without requesting notes', async (): Promise<void> => {
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
    api.get = (async (url: string): Promise<unknown> => {
      if (url === '/api/smart-ops/billing-settlements') {
        reconciliationRequests += 1
        return {
          data: {
            success: true,
            data: {
              ...emptyReconciliationData(),
              total_count: 2,
              pending_count: 1,
              manual_count: 1,
              open_alert_count: 2,
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
                  task_quota: 0,
                  task_quota_target: 0,
                  requires_manual_completion: false,
                  funding_delta: 100,
                  applied_funding_delta: 0,
                  token_delta: 100,
                  applied_token_delta: 0,
                  attempts: 2,
                  last_error: 'quota changed',
                  next_attempt: 1788106500,
                  created_at: 1786032544,
                  updated_at: 1786032544,
                  revision: 4,
                  reconciliation_reviewed_at: 0,
                  reconciliation_reviewed_by: 0,
                  reconciliation_review_note: '',
                  user_blocking_override: null,
                  record_blocks_user: true,
                  blocks_user: true,
                },
                {
                  id: 92,
                  operation_key: 'request:billing-request-92:finalize',
                  status: 'manual',
                  source: 'wallet',
                  user_id: 52,
                  subscription_id: 0,
                  token_id: 0,
                  task_id: 0,
                  task_quota: 0,
                  task_quota_target: 0,
                  requires_manual_completion: false,
                  funding_delta: 200,
                  applied_funding_delta: 0,
                  token_delta: 200,
                  applied_token_delta: 0,
                  attempts: 3,
                  last_error: 'manual reconciliation required',
                  next_attempt: 0,
                  created_at: 1786032545,
                  updated_at: 1786032545,
                  revision: 5,
                  reconciliation_reviewed_at: 0,
                  reconciliation_reviewed_by: 0,
                  reconciliation_review_note: '',
                  user_blocking_override: null,
                  record_blocks_user: true,
                  blocks_user: true,
                },
              ],
            },
          },
        }
      }
      return { data: { success: true, data: [] } }
    }) as typeof api.get
    api.put = (async (url: string, data: unknown): Promise<unknown> => {
      writes.push({ method: 'PUT', url: String(url), data })
      blockUserByDefault = (data as { block_user_by_default: boolean })
        .block_user_by_default
      return { data: { success: true } }
    }) as typeof api.put
    api.post = (async (url: string, data: unknown): Promise<unknown> => {
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
      let closeSelectedButton: HTMLElement | undefined
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
          !(view.container.textContent ?? '').includes('Reviewed records')
        )
        policySwitch = screen.getByRole('switch', {
          name: 'Block affected users by default',
        })
        closeSelectedButton = screen.getByRole('button', {
          name: 'Review and close selected (0)',
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

      assert.ok(closeSelectedButton)
      assert.equal(closeSelectedButton.hasAttribute('disabled'), true)
      const firstRowSelection = within(view.container).getByRole('checkbox', {
        name: 'Select billing reconciliation alert 91',
      })
      await view.click(firstRowSelection)
      const selectAll = within(view.container).getByRole('checkbox', {
        name: 'Select all billing reconciliation alerts',
      })
      assert.equal(selectAll.hasAttribute('data-indeterminate'), true)
      assert.ok(
        selectAll.querySelector('[data-checkbox-indicator="indeterminate"]')
      )
      await view.click(selectAll)
      closeSelectedButton = within(view.container).getByRole('button', {
        name: 'Review and close selected (2)',
      })
      assert.equal(closeSelectedButton.hasAttribute('disabled'), false)
      assert.equal(within(view.container).queryByRole('textbox'), null)
      const reconciliationRequestsBeforeReview = reconciliationRequests
      await view.click(closeSelectedButton)

      await waitFor(() => {
        assert.deepEqual(writes[1], {
          method: 'POST',
          url: '/api/smart-ops/billing-settlements/reviews',
          data: {
            items: [
              { id: 91, revision: 4 },
              { id: 92, revision: 5 },
            ],
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

  test('separates manual task completion from ordinary batch review', async (): Promise<void> => {
    const originalUser = useAuthStore.getState().auth.user
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: 100,
    })
    const originalGet = api.get
    const originalPost = api.post
    const writes: Array<{ url: string; data: unknown }> = []
    const htmlElementPrototype = window.HTMLElement
      .prototype as typeof window.HTMLElement.prototype & {
      attachEvent?: (name: string, listener: EventListener) => void
      detachEvent?: (name: string, listener: EventListener) => void
    }
    // React's async rendering path probes the legacy IE event API in this test
    // environment; emulate it on the shared prototype and remove it below.
    htmlElementPrototype.attachEvent = function (name, listener) {
      this.addEventListener(name.replace(/^on/, ''), listener)
    }
    htmlElementPrototype.detachEvent = function (name, listener) {
      this.removeEventListener(name.replace(/^on/, ''), listener)
    }
    const manualRevision = 2
    const manualCompletionRequired = true
    const includeManualItem = true
    api.get = (async (url: string): Promise<unknown> => {
      return {
        data:
          url === '/api/smart-ops/billing-settlements'
            ? {
                success: true,
                data: {
                  ...emptyReconciliationData(),
                  total_count: includeManualItem ? 3 : 0,
                  manual_count: includeManualItem ? 2 : 0,
                  open_alert_count: includeManualItem ? 3 : 0,
                  items: includeManualItem
                    ? [
                        {
                          id: 93,
                          revision: manualRevision,
                          operation_key: 'task:7001:finalize',
                          status: 'manual',
                          source: 'wallet',
                          user_id: 53,
                          subscription_id: 0,
                          token_id: 54,
                          task_id: 7001,
                          task_quota: 100,
                          task_quota_target: 100,
                          requires_manual_completion: manualCompletionRequired,
                          zero_quota_eligible: manualCompletionRequired,
                          funding_delta: 0,
                          applied_funding_delta: 0,
                          token_delta: 0,
                          applied_token_delta: 0,
                          attempts: 0,
                          last_error: 'provider usage needs verification',
                          next_attempt: 0,
                          created_at: 1786032545,
                          updated_at: 1786032545,
                          reconciliation_reviewed_at: 0,
                          reconciliation_reviewed_by: 0,
                          reconciliation_review_note: '',
                          user_blocking_override: null,
                          record_blocks_user: false,
                          blocks_user: false,
                        },
                        {
                          id: 94,
                          revision: manualRevision,
                          operation_key: 'task:7002:finalize',
                          status: 'manual',
                          source: 'wallet',
                          user_id: 53,
                          subscription_id: 0,
                          token_id: 54,
                          task_id: 7002,
                          task_quota: 120,
                          task_quota_target: 120,
                          requires_manual_completion: manualCompletionRequired,
                          zero_quota_eligible: manualCompletionRequired,
                          funding_delta: 0,
                          applied_funding_delta: 0,
                          token_delta: 0,
                          applied_token_delta: 0,
                          attempts: 0,
                          last_error: 'provider usage needs verification',
                          next_attempt: 0,
                          created_at: 1786032545,
                          updated_at: 1786032545,
                          reconciliation_reviewed_at: 0,
                          reconciliation_reviewed_by: 0,
                          reconciliation_review_note: '',
                          user_blocking_override: null,
                          record_blocks_user: false,
                          blocks_user: false,
                        },
                        {
                          id: 95,
                          revision: 1,
                          operation_key: 'request:billing-request-95:finalize',
                          status: 'pending',
                          source: 'wallet',
                          user_id: 54,
                          subscription_id: 0,
                          token_id: 0,
                          task_id: 0,
                          task_quota: 0,
                          task_quota_target: 0,
                          requires_manual_completion: false,
                          zero_quota_eligible: false,
                          funding_delta: 100,
                          applied_funding_delta: 0,
                          token_delta: 100,
                          applied_token_delta: 0,
                          attempts: 1,
                          last_error: 'ordinary alert needs review',
                          next_attempt: 0,
                          created_at: 1786032546,
                          updated_at: 1786032546,
                          reconciliation_reviewed_at: 0,
                          reconciliation_reviewed_by: 0,
                          reconciliation_review_note: '',
                          user_blocking_override: null,
                          record_blocks_user: true,
                          blocks_user: true,
                        },
                      ]
                    : [],
                },
              }
            : { success: true, data: [] },
      }
    }) as typeof api.get
    api.post = (async (url: string, data: unknown): Promise<unknown> => {
      writes.push({ url: String(url), data })
      return {
        data: {
          success: true,
          data: {
            completed_count: 1,
            failed_count: 1,
            settlement_ids: [93],
            failed: [
              {
                settlement_id: 94,
                code: 'record_conflict',
                message:
                  'record changed or could not be applied safely; refresh and reconcile it',
              },
            ],
          },
        },
      }
    }) as typeof api.post
    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        const screen = within(view.container)
        assert.ok(
          screen.getAllByRole('button', {
            name: 'Confirm zero-quota settlement',
          }).length > 0
        )
        assert.equal(
          screen
            .getByRole('checkbox', {
              name: 'Select billing reconciliation alert 93',
            })
            .hasAttribute('data-disabled'),
          false
        )
        assert.equal(
          screen
            .getByRole('button', {
              name: 'Review and close selected (0)',
            })
            .hasAttribute('disabled'),
          true
        )
      })
      await view.click(
        within(view.container).getAllByRole('button', {
          name: 'Confirm zero-quota settlement',
        })[0]
      )
      await view.click(
        within(document.body).getByRole('button', { name: 'Cancel' })
      )
      assert.deepEqual(writes, [])
      await view.click(
        within(view.container).getAllByRole('button', {
          name: 'Confirm zero-quota settlement',
        })[0]
      )
      await view.click(
        within(document.body).getByRole('button', {
          name: 'Apply zero-quota settlements',
        })
      )
      await waitFor(() => {
        assert.deepEqual(writes[0], {
          url: '/api/smart-ops/billing-settlements/complete-tasks-zero',
          data: { items: [{ id: 93, revision: manualRevision }] },
        })
      })
      const taskCheckbox = within(view.container).getByRole('checkbox', {
        name: 'Select billing reconciliation alert 93',
      })
      await view.click(taskCheckbox)
      await view.click(
        within(view.container).getByRole('checkbox', {
          name: 'Select billing reconciliation alert 94',
        })
      )
      assert.ok(
        within(view.container).getByRole('button', {
          name: 'Manually settle selected task quotas (2)',
        })
      )
      const zeroBatchButton = within(view.container).getByRole('button', {
        name: 'Confirm zero-quota settlements (2)',
      })
      assert.equal(zeroBatchButton.hasAttribute('disabled'), false)
      await view.click(zeroBatchButton)
      await view.click(
        within(document.body).getByRole('button', {
          name: 'Apply zero-quota settlements',
        })
      )
      await waitFor(() => {
        assert.deepEqual(writes[1], {
          url: '/api/smart-ops/billing-settlements/complete-tasks-zero',
          data: {
            items: [
              { id: 93, revision: manualRevision },
              { id: 94, revision: manualRevision },
            ],
          },
        })
        assert.ok(
          (view.container.textContent ?? '').includes('Settlement #94:')
        )
      })
      assert.equal(within(document.body).queryByRole('dialog'), null)
      assert.ok(
        within(view.container).getAllByRole('button', {
          name: 'Enter exact quota',
        }).length > 0
      )

      // The H3 zero-quota shortcut must not hide the exact settlement path.
      // Open the real dialog and verify that the exact input remains available
      // instead of being replaced by the zero-quota batch action.
      await view.click(
        within(view.container).getAllByRole('button', {
          name: 'Enter exact quota',
        })[0]
      )
      const exactQuotaInput = document.getElementById(
        'manual-task-actual-quota'
      ) as HTMLInputElement
      assert.ok(exactQuotaInput)
      assert.equal(exactQuotaInput.type, 'number')
      assert.equal(exactQuotaInput.min, '0')
      assert.equal(exactQuotaInput.max, '100')

      await view.click(
        within(document.body).getByRole('button', { name: 'Cancel' })
      )
      // The exact dialog was opened from the manual-task rows above. After it
      // closes, select only the ordinary alert to verify that manual task
      // completion remains separate from ordinary batch review.
      await view.click(
        within(view.container).getByRole('checkbox', {
          name: 'Select billing reconciliation alert 93',
        })
      )
      await view.click(
        within(view.container).getByRole('checkbox', {
          name: 'Select billing reconciliation alert 95',
        })
      )
      const mixedSelectionButton = within(view.container).getByRole('button', {
        name: 'Select either task settlements or ordinary alerts, not both.',
      }) as HTMLButtonElement
      assert.equal(mixedSelectionButton.disabled, true)
      assert.equal(writes.length, 2)
      await view.click(
        within(view.container).getByRole('checkbox', {
          name: 'Select billing reconciliation alert 93',
        })
      )
      await view.click(
        within(view.container).getByRole('button', {
          name: 'Review and close selected (1)',
        })
      )
      await waitFor(() => {
        assert.deepEqual(writes[2], {
          url: '/api/smart-ops/billing-settlements/reviews',
          data: { items: [{ id: 95, revision: 1 }] },
        })
        assert.ok(
          (view.container.textContent ?? '').includes('Settlement #94:')
        )
      })

      // Opening the exact-settlement path must not issue another zero-quota
      // request. The exact endpoint contract is covered by the API test.
      assert.equal(writes.length, 3)
    } finally {
      api.get = originalGet
      api.post = originalPost
      delete htmlElementPrototype.attachEvent
      delete htmlElementPrototype.detachEvent
      await view.unmount()
      queryClient.clear()
      useAuthStore.getState().auth.setUser(originalUser)
    }
  })

  test('submits per-task exact quotas from the selected batch dialog', async (): Promise<void> => {
    const originalUser = useAuthStore.getState().auth.user
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: 'root',
      role: 100,
    })
    const originalSystemConfigLoading = useSystemConfigStore.getState().loading
    useSystemConfigStore.getState().setLoading(false)
    const htmlElementPrototype = window.HTMLElement
      .prototype as typeof window.HTMLElement.prototype & {
      attachEvent?: (name: string, listener: EventListener) => void
      detachEvent?: (name: string, listener: EventListener) => void
    }
    htmlElementPrototype.attachEvent = function (name, listener) {
      this.addEventListener(name.replace(/^on/, ''), listener)
    }
    htmlElementPrototype.detachEvent = function (name, listener) {
      this.removeEventListener(name.replace(/^on/, ''), listener)
    }
    const originalGet = api.get
    const originalPost = api.post
    const writes: Array<{ url: string; data: unknown }> = []
    type TaskItemSeed = {
      id: number
      revision: number
      operation_key: string
      task_id: number
      task_quota: number
      zero_quota_eligible: boolean
    }
    const taskItems = (
      [
        {
          id: 101,
          revision: 2,
          operation_key: 'task:7101:finalize',
          task_id: 7101,
          task_quota: 100,
          zero_quota_eligible: true,
        },
        {
          id: 102,
          revision: 3,
          operation_key: 'task:7102:finalize',
          task_id: 7102,
          task_quota: 80,
          zero_quota_eligible: true,
        },
      ] satisfies TaskItemSeed[]
    ).map(
      (item: TaskItemSeed): BillingSettlementReconciliationItem => ({
        ...item,
        status: 'manual' as const,
        source: 'wallet' as const,
        user_id: 53,
        subscription_id: 0,
        token_id: 54,
        task_quota_target: item.task_quota,
        requires_manual_completion: true,
        funding_delta: 0,
        applied_funding_delta: 0,
        token_delta: 0,
        applied_token_delta: 0,
        attempts: 0,
        last_error: 'provider usage needs verification',
        next_attempt: 0,
        created_at: 1786032545,
        updated_at: 1786032545,
        reconciliation_reviewed_at: 0,
        reconciliation_reviewed_by: 0,
        reconciliation_review_note: '',
        user_blocking_override: null,
        record_blocks_user: false,
        blocks_user: false,
      })
    )
    api.get = (async (url: string): Promise<unknown> => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? {
              success: true,
              data: {
                ...emptyReconciliationData(),
                total_count: taskItems.length,
                manual_count: taskItems.length,
                open_alert_count: taskItems.length,
                items: taskItems,
              },
            }
          : { success: true, data: [] },
    })) as typeof api.get
    api.post = (async (url: string, data: unknown): Promise<unknown> => {
      writes.push({ url: String(url), data })
      return {
        data: {
          success: true,
          data: {
            completed_count: 2,
            failed_count: 0,
            settlement_ids: [101, 102],
            failed: [],
          },
        },
      }
    }) as typeof api.post

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      await waitFor(() => {
        assert.ok(
          within(view.container).getByRole('button', {
            name: 'Review and close selected (0)',
          })
        )
      })
      const checkboxes = taskItems.map((item) =>
        within(view.container).getByRole('checkbox', {
          name: `Select billing reconciliation alert ${item.id}`,
        })
      )
      await view.click(checkboxes[0])
      await view.click(checkboxes[1])
      await view.click(
        within(view.container).getByRole('button', {
          name: 'Manually settle selected task quotas (2)',
        })
      )

      const firstInput = await waitFor(() => {
        const input = document.getElementById(
          'manual-task-actual-quota-101'
        ) as HTMLInputElement | null
        assert.ok(input)
        return input
      })
      const secondInput = document.getElementById(
        'manual-task-actual-quota-102'
      ) as HTMLInputElement
      assert.ok(secondInput)
      assert.equal(useSystemConfigStore.getState().loading, false)
      assert.equal(firstInput.disabled, false)
      assert.equal(secondInput.disabled, false)
      assert.equal(within(document.body).queryByText('Loading...'), null)
      assert.equal(
        within(document.body).queryByText(
          'This reconciliation record changed while the dialog was open.'
        ),
        null
      )
      const setInputValue = (input: HTMLInputElement, value: string): void => {
        const setter = Object.getOwnPropertyDescriptor(
          window.HTMLInputElement.prototype,
          'value'
        )?.set
        assert.ok(setter)
        setter.call(input, value)
        fireEvent.input(input)
        fireEvent.change(input)
      }
      await act(async () => {
        setInputValue(firstInput, '101')
      })
      await waitFor(() => {
        assert.ok(
          within(document.body).getByText(
            'Final quota cannot exceed the reserved quota.'
          )
        )
        const submitButton = within(document.body).getByRole('button', {
          name: 'Apply exact settlements',
        }) as HTMLButtonElement
        assert.equal(submitButton.disabled, true)
      })
      await act(async () => {
        setInputValue(firstInput, '0')
        setInputValue(secondInput, '40')
      })
      await waitFor(() => {
        assert.equal(firstInput.value, '0')
        assert.equal(secondInput.value, '40')
        const submitButton = within(document.body).getByRole('button', {
          name: 'Apply exact settlements',
        }) as HTMLButtonElement
        assert.equal(submitButton.disabled, false)
      })
      await view.click(
        within(document.body).getByRole('button', {
          name: 'Apply exact settlements',
        })
      )

      await waitFor(() => {
        assert.deepEqual(writes, [
          {
            url: '/api/smart-ops/billing-settlements/complete-tasks',
            data: {
              items: [
                { id: 101, revision: 2, actual_quota: 0 },
                { id: 102, revision: 3, actual_quota: 40 },
              ],
            },
          },
        ])
      })
    } finally {
      api.get = originalGet
      api.post = originalPost
      await view.unmount()
      queryClient.clear()
      useAuthStore.getState().auth.setUser(originalUser)
      useSystemConfigStore.getState().setLoading(originalSystemConfigLoading)
      delete htmlElementPrototype.attachEvent
      delete htmlElementPrototype.detachEvent
    }
  })

  test('blocks zero-quota settlement when the confirmation revision becomes stale', async (): Promise<void> => {
    const originalPost = api.post
    const writes: Array<{ url: string; data: unknown }> = []
    api.post = (async (url: string, data: unknown): Promise<unknown> => {
      writes.push({ url: String(url), data })
      return {
        data: {
          success: true,
          data: {
            completed_count: 1,
            failed_count: 0,
            settlement_ids: [111],
            failed: [],
          },
        },
      }
    }) as typeof api.post

    const taskItem = (
      revision: number
    ): BillingSettlementReconciliationItem => ({
      id: 111,
      revision,
      operation_key: 'task:7111:finalize',
      status: 'manual' as const,
      source: 'wallet' as const,
      user_id: 53,
      subscription_id: 0,
      token_id: 54,
      task_id: 7111,
      task_quota: 100,
      task_quota_target: 100,
      requires_manual_completion: true,
      zero_quota_eligible: true,
      funding_delta: 0,
      applied_funding_delta: 0,
      token_delta: 0,
      applied_token_delta: 0,
      attempts: 0,
      last_error: 'provider usage needs verification',
      next_attempt: 0,
      created_at: 1786032545,
      updated_at: 1786032545,
      reconciliation_reviewed_at: 0,
      reconciliation_reviewed_by: 0,
      reconciliation_review_note: '',
      user_blocking_override: null,
      record_blocks_user: false,
      blocks_user: false,
    })

    function Harness(): ReactElement {
      const [revision, setRevision] = useState(2)
      const data = useMemo<BillingSettlementReconciliationData>(
        () => ({
          ...emptyReconciliationData(),
          total_count: 1,
          manual_count: 1,
          open_alert_count: 1,
          items: [taskItem(revision)],
        }),
        [revision]
      )
      return (
        <>
          <button type='button' onClick={() => setRevision(3)}>
            Refresh reconciliation
          </button>
          <BillingSettlementEvidence
            canCompleteManualTask
            canUpdateBlockingPolicy
            data={data}
            error={null}
            loading={false}
            onRetry={() => undefined}
          />
        </>
      )
    }

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <Harness />
      </QueryClientProvider>
    )

    try {
      const checkbox = within(view.container).getByRole('checkbox', {
        name: 'Select billing reconciliation alert 111',
      })
      await view.click(checkbox)
      await waitFor(() => {
        assert.ok(
          within(view.container).getByRole('button', {
            name: 'Confirm zero-quota settlements (1)',
          })
        )
      })
      const exactQuotaBatchButton = within(view.container).getByRole('button', {
        name: 'Manually settle selected task quotas (1)',
      })
      const zeroQuotaBatchButton = within(view.container).getByRole('button', {
        name: 'Confirm zero-quota settlements (1)',
      })
      const actionButtons = Array.from(
        view.container.querySelectorAll('button')
      ).filter((button) =>
        [exactQuotaBatchButton, zeroQuotaBatchButton].includes(button)
      )
      assert.equal(actionButtons[0], exactQuotaBatchButton)
      await view.click(exactQuotaBatchButton)
      await waitFor(() => {
        assert.ok(document.getElementById('manual-task-actual-quota-111'))
      })
      await view.click(
        within(document.body).getByRole('button', { name: 'Cancel' })
      )
      await view.click(zeroQuotaBatchButton)
      await waitFor(() => {
        assert.ok(
          within(document.body).getByRole('button', {
            name: 'Apply zero-quota settlements',
          })
        )
      })

      const refreshButton = view.container.querySelector(
        'button'
      ) as HTMLButtonElement | null
      assert.ok(refreshButton)
      fireEvent.click(refreshButton)
      await waitFor(() => {
        const confirm = within(document.body).getByRole('button', {
          name: 'Apply zero-quota settlements',
        }) as HTMLButtonElement
        assert.equal(confirm.disabled, true)
        assert.ok(
          within(document.body).getByText(
            'This reconciliation record changed while the dialog was open.'
          )
        )
      })
      await view.click(
        within(document.body).getByRole('button', {
          name: 'Apply zero-quota settlements',
        })
      )
      assert.deepEqual(writes, [])

      await view.click(
        within(document.body).getByRole('button', { name: 'Cancel' })
      )
      await view.click(
        within(view.container).getByRole('checkbox', {
          name: 'Select billing reconciliation alert 111',
        })
      )
      await waitFor(() => {
        assert.ok(
          within(view.container).getByRole('button', {
            name: 'Confirm zero-quota settlements (1)',
          })
        )
      })
      await view.click(
        within(view.container).getByRole('button', {
          name: 'Confirm zero-quota settlements (1)',
        })
      )
      await view.click(
        within(document.body).getByRole('button', {
          name: 'Apply zero-quota settlements',
        })
      )
      await waitFor(() => {
        assert.deepEqual(writes, [
          {
            url: '/api/smart-ops/billing-settlements/complete-tasks-zero',
            data: { items: [{ id: 111, revision: 3 }] },
          },
        ])
      })
    } finally {
      api.post = originalPost
      await view.unmount()
      queryClient.clear()
    }
  })

  test('clears a selected alert when refresh changes its financial revision', async (): Promise<void> => {
    const originalGet = api.get
    const originalPost = api.post
    const writes: Array<{ url: string; data: unknown }> = []
    let revision = 4
    api.get = (async (url: string): Promise<unknown> => {
      if (url !== '/api/smart-ops/billing-settlements') {
        return { data: { success: true, data: [] } }
      }
      return {
        data: {
          success: true,
          data: {
            ...emptyReconciliationData(),
            total_count: 1,
            pending_count: 1,
            open_alert_count: 1,
            items: [
              {
                id: 91,
                revision,
                operation_key: 'request:billing-request-91:finalize',
                status: 'pending',
                source: 'wallet',
                user_id: 51,
                subscription_id: 0,
                token_id: 0,
                task_id: 0,
                task_quota: 0,
                task_quota_target: 0,
                requires_manual_completion: false,
                funding_delta: 100,
                applied_funding_delta: 0,
                token_delta: 100,
                applied_token_delta: 0,
                attempts: 2,
                last_error: 'quota changed',
                next_attempt: 1788106500,
                created_at: 1786032544,
                updated_at: 1786032544,
                reconciliation_reviewed_at: 0,
                reconciliation_reviewed_by: 0,
                reconciliation_review_note: '',
                user_blocking_override: null,
                record_blocks_user: false,
                blocks_user: false,
              },
            ],
          },
        },
      }
    }) as typeof api.get
    api.post = (async (url: string, data: unknown): Promise<unknown> => {
      writes.push({ url: String(url), data })
      return { data: { success: true } }
    }) as typeof api.post

    const queryClient = createQueryClient()
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ActiveAlerts />
      </QueryClientProvider>
    )

    try {
      let rowSelection: HTMLElement | undefined
      await waitFor(() => {
        rowSelection = within(view.container).getByRole('checkbox', {
          name: 'Select billing reconciliation alert 91',
        })
      })
      assert.ok(rowSelection)
      await view.click(rowSelection)
      await waitFor(() => {
        assert.ok(
          within(view.container).getByRole('button', {
            name: 'Review and close selected (1)',
          })
        )
      })

      revision = 6
      await queryClient.invalidateQueries()
      await waitFor(() => {
        const closeSelectedButton = within(view.container).getByRole('button', {
          name: 'Review and close selected (0)',
        })
        assert.equal(closeSelectedButton.hasAttribute('disabled'), true)
        assert.equal(
          within(view.container)
            .getByRole('checkbox', {
              name: 'Select billing reconciliation alert 91',
            })
            .getAttribute('data-checked'),
          null
        )
      })
      assert.equal(writes.length, 0)

      rowSelection = within(view.container).getByRole('checkbox', {
        name: 'Select billing reconciliation alert 91',
      })
      await view.click(rowSelection)
      await view.click(
        within(view.container).getByRole('button', {
          name: 'Review and close selected (1)',
        })
      )
      await waitFor(() => {
        assert.deepEqual(writes, [
          {
            url: '/api/smart-ops/billing-settlements/reviews',
            data: { items: [{ id: 91, revision: 6 }] },
          },
        ])
      })
    } finally {
      api.get = originalGet
      api.post = originalPost
      await view.unmount()
      queryClient.clear()
    }
  })

  test('keeps the global blocking policy read only for non-root administrators', async (): Promise<void> => {
    const originalUser = useAuthStore.getState().auth.user
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'admin',
      role: 10,
    })
    const originalGet = api.get
    const originalPut = api.put
    let policyWrites = 0
    api.get = (async (url: string): Promise<unknown> => ({
      data:
        url === '/api/smart-ops/billing-settlements'
          ? { success: true, data: emptyReconciliationData() }
          : { success: true, data: [] },
    })) as typeof api.get
    api.put = (async (): Promise<unknown> => {
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

  test('shows a healthy empty state when no incident is active', async (): Promise<void> => {
    const originalGet = api.get
    api.get = (async (url: string): Promise<unknown> => ({
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

  test('shows a localized fallback for an unsuccessful response', async (): Promise<void> => {
    const originalGet = api.get
    await testEnv.i18n.changeLanguage('fr')
    api.get = (async (url: string): Promise<unknown> => ({
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

  test('shows a retryable error state when the alert endpoint fails', async (): Promise<void> => {
    const originalGet = api.get
    let requestCount = 0
    api.get = (async (url: string): Promise<unknown> => {
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
