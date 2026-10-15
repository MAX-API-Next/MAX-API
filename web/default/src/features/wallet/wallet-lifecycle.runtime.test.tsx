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
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import {
  subscriptionPlanSchema,
  type PlanRecord,
} from '@/features/subscriptions/types'

const env = createReactTestEnvironment()
await env.setup()
const storage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: env.dom.window.localStorage,
})
Object.defineProperty(env.dom.window.HTMLElement.prototype, 'scrollIntoView', {
  configurable: true,
  value() {},
})
const { fireEvent, waitFor, within } =
  await import('@testing-library/react/pure')
const { Wallet } = await import('./index')
const { SubscriptionPlansCard } =
  await import('./components/subscription-plans-card')
const { SubscriptionPurchaseDialog } =
  await import('@/features/subscriptions/components/dialogs/subscription-purchase-dialog')
after(() => {
  env.teardown()
  if (storage) Object.defineProperty(globalThis, 'localStorage', storage)
  else Reflect.deleteProperty(globalThis, 'localStorage')
})

test('wallet custom zero stays explicit instead of being initialized again', async () => {
  const adapter = api.defaults.adapter
  const quotes: number[] = [],
    mutations: string[] = []
  api.defaults.adapter = async (config) => {
    let data: unknown = { success: true, data: {} }
    if (config.url === '/api/user/self')
      data = {
        success: true,
        data: {
          id: 1,
          username: 'synthetic-user',
          quota: 0,
          used_quota: 0,
          request_count: 0,
          aff_quota: 0,
        },
      }
    else if (config.url === '/api/user/topup/info')
      data = {
        success: true,
        data: {
          enable_online_topup: true,
          min_topup: 10,
          pay_methods: [{ name: 'Synthetic method', type: 'alipay' }],
          amount_options: [],
          discount: {},
          stripe_min_topup: 10,
        },
      }
    else if (config.url === '/api/user/aff') data = { success: true, data: '' }
    else if (config.url === '/api/subscription/plans')
      data = { success: true, data: [] }
    else if (config.url === '/api/subscription/self')
      data = {
        success: true,
        data: {
          billing_preference: 'wallet_first',
          subscriptions: [],
          all_subscriptions: [],
        },
      }
    else if (config.url === '/api/user/amount') {
      const amount = JSON.parse(config.data).amount
      quotes.push(amount)
      data = { success: true, message: 'success', data: String(amount) }
    } else if (config.method !== 'get') mutations.push(config.url || '')
    return { config, status: 200, statusText: 'OK', headers: {}, data }
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  function Harness() {
    const [, setRevision] = useState(0)
    return (
      <>
        <button onClick={() => setRevision((value) => value + 1)}>
          Render wallet again
        </button>
        <Wallet />
      </>
    )
  }
  const view = await env.render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>
  )
  const fields = within(view.container)
  try {
    await waitFor(() =>
      assert.equal(
        (
          fields.getByRole('spinbutton', {
            name: 'Custom Amount',
          }) as HTMLInputElement
        ).value,
        '10'
      )
    )
    await act(async () => {
      fireEvent.change(
        fields.getByRole('spinbutton', { name: 'Custom Amount' }),
        { target: { value: '0' } }
      )
    })
    await act(async () => {
      fireEvent.change(
        fields.getByRole('spinbutton', { name: 'Custom Amount' }),
        { target: { value: '05' } }
      )
    })
    assert.equal(
      (
        fields.getByRole('spinbutton', {
          name: 'Custom Amount',
        }) as HTMLInputElement
      ).value,
      '05'
    )
    await act(async () => {
      fireEvent.change(
        fields.getByRole('spinbutton', { name: 'Custom Amount' }),
        { target: { value: '1.5' } }
      )
    })
    assert.equal(
      (
        fields.getByRole('spinbutton', {
          name: 'Custom Amount',
        }) as HTMLInputElement
      ).value,
      '1.5'
    )
    await act(async () => {
      fireEvent.change(
        fields.getByRole('spinbutton', { name: 'Custom Amount' }),
        { target: { value: '' } }
      )
    })
    assert.equal(
      (
        fields.getByRole('spinbutton', {
          name: 'Custom Amount',
        }) as HTMLInputElement
      ).value,
      ''
    )
    await view.click(fields.getByText('Render wallet again'))
    assert.equal(
      (
        fields.getByRole('spinbutton', {
          name: 'Custom Amount',
        }) as HTMLInputElement
      ).value,
      ''
    )
    assert.deepEqual(quotes, [10, 0, 5, 1, 0])
    assert.deepEqual(mutations, [])
  } finally {
    await view.unmount()
    client.clear()
    api.defaults.adapter = adapter
  }
})

test('subscription reads ignore stale responses after the auth session changes', async () => {
  const adapter = api.defaults.adapter
  const previousUser = useAuthStore.getState().auth.user
  const pending: Array<() => void> = []
  const subscription = (id: number) => ({
    id,
    user_id: id,
    plan_id: 1,
    status: 'active',
    start_time: 1710000000,
    end_time: 1910000000,
    amount_total: 100,
    amount_used: 0,
  })
  try {
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'first-user', role: 1 })
    api.defaults.adapter = async (config) => {
      assert.equal(config.method, 'get')
      if (config.url === '/api/subscription/plans') {
        return {
          config,
          status: 200,
          statusText: 'OK',
          headers: {},
          data: { success: true, data: [] },
        }
      }
      if (config.url === '/api/subscription/self') {
        const index = pending.length
        return new Promise((resolve) => {
          pending.push(() =>
            resolve({
              config,
              status: 200,
              statusText: 'OK',
              headers: {},
              data: {
                success: true,
                data: {
                  billing_preference:
                    index === 0 ? 'wallet_first' : 'wallet_only',
                  subscriptions: [
                    { subscription: subscription(index === 0 ? 1 : 2) },
                  ],
                  all_subscriptions: [
                    { subscription: subscription(index === 0 ? 1 : 2) },
                  ],
                },
              },
            })
          )
        })
      }
      throw new Error(`Unexpected ${config.url}`)
    }
    const view = await env.render(
      <SubscriptionPlansCard topupInfo={null} userQuota={0} />
    )
    try {
      await waitFor(() => assert.equal(pending.length, 1))
      await act(async () => {
        useAuthStore
          .getState()
          .auth.setUser({ id: 2, username: 'second-user', role: 1 })
      })
      await waitFor(() => assert.equal(pending.length, 2))
      await act(async () => pending[1]())
      await waitFor(() =>
        assert.match(view.container.textContent || '', /Subscription #2/)
      )
      await act(async () => pending[0]())
      await new Promise((resolve) => setTimeout(resolve, 0))
      assert.match(view.container.textContent || '', /Subscription #2/)
      assert.doesNotMatch(view.container.textContent || '', /Subscription #1/)
    } finally {
      pending.forEach((release) => release())
      await view.unmount()
    }
  } finally {
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(previousUser)
  }
})

test('epay selection survives a host rerender and an in-flight payment stays locked across reopen', async () => {
  const adapter = api.defaults.adapter
  const plan: PlanRecord = {
    plan: subscriptionPlanSchema.parse({
      id: 1,
      title: 'Synthetic plan',
      price_amount: 1,
      currency: 'USD',
      duration_unit: 'month',
      duration_value: 1,
      quota_reset_period: 'never',
      enabled: true,
      sort_order: 0,
      allow_balance_pay: false,
      max_purchase_per_user: 0,
      total_amount: 0,
    }),
  }
  const payments: unknown[] = []
  let release!: () => void
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'post')
    payments.push(JSON.parse(config.data))
    await new Promise<void>((resolve) => {
      release = resolve
    })
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: false, message: 'Synthetic payment declined' },
    }
  }
  function Harness() {
    const [open, setOpen] = useState(true)
    const [, setRevision] = useState(0)
    return (
      <>
        <button onClick={() => setRevision((value) => value + 1)}>
          Render purchase again
        </button>
        <button onClick={() => setOpen((value) => !value)}>
          Toggle purchase
        </button>
        <SubscriptionPurchaseDialog
          open={open}
          onOpenChange={setOpen}
          plan={plan}
          enableOnlineTopUp
          epayMethods={[
            { name: 'Synthetic Ali', type: 'alipay' },
            { name: 'Synthetic WeChat', type: 'wxpay' },
          ]}
          userQuota={0}
        />
      </>
    )
  }
  const view = await env.render(<Harness />)
  const body = within(env.dom.window.document.body)
  try {
    await view.click(body.getByRole('combobox'))
    await view.click(body.getByRole('option', { name: 'Synthetic WeChat' }))
    await view.click(within(view.container).getByText('Render purchase again'))
    assert.ok(
      body.getByRole('combobox').textContent?.includes('Synthetic WeChat')
    )
    await view.click(body.getByRole('button', { name: 'Pay' }))
    assert.equal(payments.length, 1)
    assert.deepEqual(payments[0], { plan_id: 1, payment_method: 'wxpay' })
    await view.click(within(view.container).getByText('Toggle purchase'))
    await view.click(within(view.container).getByText('Toggle purchase'))
    const pay = body.getByRole('button', {
      name: 'Pay',
    }) as HTMLButtonElement
    assert.equal(pay.disabled, true)
    await view.click(pay)
    assert.equal(payments.length, 1)
    await act(async () => release())
    assert.equal(
      (
        body.getByRole('button', {
          name: 'Pay',
        }) as HTMLButtonElement
      ).disabled,
      false
    )
  } finally {
    release?.()
    await view.unmount()
    api.defaults.adapter = adapter
  }
})

test('subscription expiry display advances without a refresh or any financial write', async () => {
  const adapter = api.defaults.adapter,
    originalNow = Date.now
  const interval = Object.getOwnPropertyDescriptor(globalThis, 'setTimeout')!,
    clear = Object.getOwnPropertyDescriptor(globalThis, 'clearTimeout')!
  let now = 1710000000000
  let tick: (() => void) | undefined
  const requests: string[] = []
  Date.now = () => now
  const nativeInterval = globalThis.setTimeout,
    nativeClear = globalThis.clearTimeout
  const clockHandle = Symbol('subscription display clock')
  Object.defineProperty(globalThis, 'setTimeout', {
    configurable: true,
    value: (callback: () => void, milliseconds: number) => {
      if (milliseconds !== 1001) return nativeInterval(callback, milliseconds)
      tick = callback
      return clockHandle
    },
  })
  Object.defineProperty(globalThis, 'clearTimeout', {
    configurable: true,
    value: (handle: unknown) => {
      if (handle === clockHandle) tick = undefined
      else nativeClear(handle as ReturnType<typeof setTimeout>)
    },
  })
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'get')
    requests.push(config.url || '')
    const subscription = {
      id: 7,
      user_id: 1,
      plan_id: 1,
      status: 'active',
      start_time: now / 1000 - 60,
      end_time: now / 1000 + 1,
      amount_total: 100,
      amount_used: 25,
    }
    const data =
      config.url === '/api/subscription/plans'
        ? []
        : {
            billing_preference: 'subscription_first',
            subscriptions: [{ subscription }],
            all_subscriptions: [{ subscription }],
          }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data },
    }
  }
  const view = await env.render(
    <SubscriptionPlansCard topupInfo={null} userQuota={0} />
  )
  const body = within(view.container)
  try {
    await waitFor(() => assert.ok(body.queryByText('Active')))
    await act(async () => {
      now += 2000
      tick?.()
    })
    assert.equal(body.queryByText('Active'), null)
    assert.ok(body.queryByText('Expired'))
    assert.equal(requests.length, 2)
  } finally {
    await view.unmount()
    assert.equal(tick, undefined)
    Date.now = originalNow
    Object.defineProperty(globalThis, 'setTimeout', interval)
    Object.defineProperty(globalThis, 'clearTimeout', clear)
    api.defaults.adapter = adapter
  }
})
