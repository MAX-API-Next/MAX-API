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
import { act, type ComponentProps } from 'react'
import type { Root } from 'react-dom/client'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, afterEach, beforeEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { ConfirmPaymentComplianceResponse } from '../types'

// Import DOM-dependent components after installing the browser globals.
let createRoot: typeof import('react-dom/client').createRoot
let fireEvent: typeof import('@testing-library/react/pure').fireEvent
let within: typeof import('@testing-library/react/pure').within
let PaymentSettingsSection: typeof import('./payment-settings-section').PaymentSettingsSection
let api: typeof import('@/lib/api').api
let originalAdapter: typeof api.defaults.adapter

type PaymentProps = ComponentProps<typeof PaymentSettingsSection>
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const i18n = createInstance()
const previousGlobals = new Map<string, PropertyDescriptor | undefined>()
const requests: InternalAxiosRequestConfig[] = []
let respond: () => Promise<ConfirmPaymentComplianceResponse>
let container: HTMLDivElement
let root: Root
let queryClient: QueryClient

function defaults(): PaymentProps {
  return {
    defaultValues: {
      PayAddress: '',
      EpayId: '',
      EpayKey: '',
      Price: 1,
      MinTopUp: 1,
      CustomCallbackAddress: '',
      PayMethods: '[]',
      AmountOptions: '[]',
      AmountDiscount: '{}',
      StripeApiSecret: '',
      StripeWebhookSecret: '',
      StripePriceId: '',
      StripeUnitPrice: 1,
      StripeMinTopUp: 1,
      StripePromotionCodesEnabled: false,
      CreemApiKey: '',
      CreemWebhookSecret: '',
      CreemTestMode: false,
      CreemProducts: '[]',
    },
    waffoDefaultValues: {
      WaffoEnabled: false,
      WaffoApiKey: '',
      WaffoPrivateKey: '',
      WaffoPublicCert: '',
      WaffoSandboxPublicCert: '',
      WaffoSandboxApiKey: '',
      WaffoSandboxPrivateKey: '',
      WaffoSandbox: false,
      WaffoMerchantId: '',
      WaffoCurrency: 'USD',
      WaffoUnitPrice: 1,
      WaffoMinTopUp: 1,
      WaffoNotifyUrl: '',
      WaffoReturnUrl: '',
      WaffoPayMethods: '[]',
    },
    waffoPancakeDefaultValues: {
      WaffoPancakeMerchantID: '',
      WaffoPancakePrivateKey: '',
      WaffoPancakeReturnURL: '',
    },
    complianceDefaults: {
      confirmed: false,
      termsVersion: '',
      confirmedAt: 0,
      confirmedBy: 0,
    },
  }
}

async function renderPayment(props: PaymentProps = defaults()): Promise<void> {
  await act(async (): Promise<void> => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <PaymentSettingsSection {...props} />
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
}

async function flushUpdates(): Promise<void> {
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
}

async function click(element: HTMLElement): Promise<void> {
  await act(async (): Promise<void> => {
    fireEvent.click(element)
    await flushUpdates()
  })
}

async function setup(): Promise<void> {
  const window = dom.window
  const globals: Record<string, unknown> = {
    window,
    document: window.document,
    navigator: window.navigator,
    HTMLElement: window.HTMLElement,
    Element: window.Element,
    Node: window.Node,
    Event: window.Event,
    MouseEvent: window.MouseEvent,
    MutationObserver: window.MutationObserver,
    getComputedStyle: window.getComputedStyle.bind(window),
    requestAnimationFrame: window.requestAnimationFrame.bind(window),
    cancelAnimationFrame: window.cancelAnimationFrame.bind(window),
    localStorage: window.localStorage,
    IS_REACT_ACT_ENVIRONMENT: true,
  }
  for (const [key, value] of Object.entries(globals)) {
    previousGlobals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
    Object.defineProperty(globalThis, key, {
      configurable: true,
      writable: true,
      value,
    })
  }
  await i18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
  })
  ;({ createRoot } = await import('react-dom/client'))
  ;({ fireEvent, within } = await import('@testing-library/react/pure'))
  ;({ PaymentSettingsSection } = await import('./payment-settings-section'))
  ;({ api } = await import('@/lib/api'))
  originalAdapter = api.defaults.adapter
}

// Keep module loading outside the timed interactions, as in a loaded browser page.
await setup()

function complianceAlert(): ReturnType<typeof within> {
  const alert = within(container)
    .getByText(/^(Compliance confirmation required|Compliance confirmed)$/)
    .closest<HTMLElement>('[role="alert"]')
  assert.ok(alert)
  return within(alert)
}

beforeEach((): void => {
  container = dom.window.document.createElement('div')
  dom.window.document.body.append(container)
  root = createRoot(container)
  queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  })
  queryClient.setQueryData(['system-options'], [])
  requests.length = 0
  respond = async (): Promise<ConfirmPaymentComplianceResponse> => ({
    success: true,
    message: '',
  })
  api.defaults.adapter = async (
    config: InternalAxiosRequestConfig
  ): Promise<AxiosResponse> => {
    requests.push(config)
    assert.equal(config.url, '/api/option/payment_compliance')
    assert.equal(config.method, 'post')
    return {
      config,
      data: await respond(),
      status: 200,
      statusText: 'OK',
      headers: {},
    }
  }
})

afterEach(async (): Promise<void> => {
  await act(async (): Promise<void> => root.unmount())
  container.remove()
  queryClient.clear()
  api.defaults.adapter = originalAdapter
})

after((): void => {
  for (const [key, descriptor] of [...previousGlobals].reverse()) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
  dom.window.close()
})

test('shows the terms without typing and does not confirm on open or cancel', async (): Promise<void> => {
  await renderPayment()
  const screen = within(dom.window.document.body)
  await click(
    complianceAlert().getByRole('button', { name: 'Confirm compliance' })
  )
  const dialog = screen.getByRole('alertdialog', {
    name: 'Confirm compliance terms',
  })
  const dialogScreen = within(dialog)
  assert.equal(dialogScreen.getAllByRole('listitem').length, 6)
  assert.ok(
    dialogScreen.getByText(
      'You understand and independently bear legal responsibility arising from deployment, operation, and charging behavior.'
    )
  )
  assert.equal(dialogScreen.queryByRole('textbox'), null)
  assert.equal(dialogScreen.queryByRole('checkbox'), null)
  const confirm = dialogScreen.getByRole<HTMLButtonElement>('button', {
    name: 'Acknowledge and enable features',
  })
  assert.equal(confirm.disabled, false)
  assert.equal(requests.length, 0)
  await click(dialogScreen.getByRole('button', { name: 'Cancel' }))
  assert.equal(screen.queryByRole('alertdialog'), null)
  assert.equal(requests.length, 0)
})

test('a single click submits acknowledgment, disables pending actions and refreshes saved state', async (): Promise<void> => {
  let finish!: (value: ConfirmPaymentComplianceResponse) => void
  const pending = new Promise<ConfirmPaymentComplianceResponse>((resolve) => {
    finish = resolve
  })
  respond = (): Promise<ConfirmPaymentComplianceResponse> => pending
  await renderPayment()
  const screen = within(dom.window.document.body)
  await click(
    complianceAlert().getByRole('button', { name: 'Confirm compliance' })
  )
  const dialogScreen = within(screen.getByRole('alertdialog'))
  const confirm = dialogScreen.getByRole<HTMLButtonElement>('button', {
    name: 'Acknowledge and enable features',
  })
  const cancel = dialogScreen.getByRole<HTMLButtonElement>('button', {
    name: 'Cancel',
  })
  await click(confirm)
  assert.equal(confirm.disabled, true)
  assert.equal(cancel.disabled, true)
  await click(confirm)
  assert.equal(requests.length, 1)
  assert.deepEqual(JSON.parse(requests[0].data as string), { confirmed: true })
  await act(async (): Promise<void> => {
    finish({ success: true, message: '' })
    await flushUpdates()
  })
  assert.equal(screen.queryByRole('alertdialog'), null)
  assert.equal(
    queryClient.getQueryState(['system-options'])?.isInvalidated,
    true
  )

  const props = defaults()
  props.complianceDefaults = {
    confirmed: true,
    termsVersion: 'v1',
    confirmedAt: 1700000000,
    confirmedBy: 1,
  }
  await renderPayment(props)
  assert.ok(screen.getByText('Compliance confirmed'))
  assert.equal(
    complianceAlert().queryByRole('button', { name: 'Confirm compliance' }),
    null
  )
})

test('rejected acknowledgment keeps the dialog open without unlocking features', async (): Promise<void> => {
  respond = async (): Promise<ConfirmPaymentComplianceResponse> => ({
    success: false,
    message: 'Confirmation rejected',
  })
  await renderPayment()
  const screen = within(dom.window.document.body)
  await click(
    complianceAlert().getByRole('button', { name: 'Confirm compliance' })
  )
  const confirm = within(
    screen.getByRole('alertdialog')
  ).getByRole<HTMLButtonElement>('button', {
    name: 'Acknowledge and enable features',
  })
  await click(confirm)
  assert.equal(requests.length, 1)
  assert.ok(screen.getByRole('alertdialog'))
  assert.equal(confirm.disabled, false)
  assert.equal(screen.queryByText('Compliance confirmed'), null)
  assert.equal(
    queryClient.getQueryState(['system-options'])?.isInvalidated,
    false
  )
})

test('an outdated terms version still requires explicit acknowledgment', async (): Promise<void> => {
  const props = defaults()
  props.complianceDefaults = {
    confirmed: true,
    termsVersion: 'v0',
    confirmedAt: 1700000000,
    confirmedBy: 1,
  }
  await renderPayment(props)
  assert.ok(
    complianceAlert().getByRole('button', { name: 'Confirm compliance' })
  )
  assert.equal(requests.length, 0)
})
