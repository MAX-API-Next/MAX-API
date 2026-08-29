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
import type { ReactNode } from 'react'
import { createReactTestEnvironment } from '@/test/react'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  mock,
  test,
} from 'bun:test'
import assert from 'node:assert/strict'

const createTelegramBindState = mock(async () => ({
  success: true,
  data: { state: 'telegram-bind-state' },
}))
const bindTelegramAccount = mock(async () => ({ success: true }))
const toastSuccess = mock(() => undefined)
const toastError = mock(() => undefined)
const handleServerError = mock(() => undefined)
const translate = (key: string) => key
const withVerification = <T,>(apiCall: () => Promise<T>) => apiCall()
let useUnstableVerificationGate = false

mock.module('../src/features/auth/secure-verification', () => ({
  useSecureVerificationGate: () => ({
    withVerification: useUnstableVerificationGate
      ? <T,>(apiCall: () => Promise<T>) => apiCall()
      : withVerification,
  }),
}))

mock.module('../src/features/profile/api', () => ({
  bindTelegramAccount,
  createTelegramBindState,
}))

mock.module('../src/lib/handle-server-error', () => ({ handleServerError }))

mock.module('sonner', () => ({
  toast: { error: toastError, success: toastSuccess },
}))

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: translate }),
}))

function TestContainer(props: { children?: ReactNode }) {
  return <div>{props.children}</div>
}

mock.module('../src/components/ui/alert', () => ({
  Alert: TestContainer,
  AlertDescription: TestContainer,
}))

mock.module('../src/components/ui/button', () => ({
  Button: (props: {
    children?: ReactNode
    onClick?: () => void
    type?: 'button' | 'submit' | 'reset'
  }) => (
    <button type={props.type} onClick={props.onClick}>
      {props.children}
    </button>
  ),
}))

mock.module('../src/components/ui/dialog', () => ({
  Dialog: TestContainer,
  DialogContent: TestContainer,
  DialogDescription: TestContainer,
  DialogHeader: TestContainer,
  DialogTitle: TestContainer,
}))

mock.module('../src/components/ui/spinner', () => ({
  Spinner: () => <span />,
}))

const { TelegramBindDialog } = await import(
  '../src/features/profile/components/dialogs/telegram-bind-dialog'
)
const testEnv = createReactTestEnvironment()

beforeAll(() => testEnv.setup())
beforeEach(() => {
  useUnstableVerificationGate = false
  createTelegramBindState.mockClear()
  bindTelegramAccount.mockClear()
  toastSuccess.mockClear()
  toastError.mockClear()
  handleServerError.mockClear()
  assert.equal(document.querySelector('script[data-onauth]'), null)
})
afterEach(() => cleanup())
afterAll(() => testEnv.teardown())

describe('TelegramBindDialog widget lifecycle', () => {
  test('keeps the active widget mounted across callback-only rerenders', async () => {
    const firstOpenChange = mock(() => undefined)
    const secondOpenChange = mock(() => undefined)
    const firstSuccess = mock(() => undefined)
    const secondSuccess = mock(() => undefined)

    const view = render(
      <TelegramBindDialog
        open
        onOpenChange={firstOpenChange}
        botName='max_api_bot'
        onSuccess={firstSuccess}
      />
    )

    await waitFor(() =>
      assert.equal(createTelegramBindState.mock.calls.length, 1)
    )
    const initialScript = await waitFor(() => {
      const script = document.querySelector<HTMLScriptElement>(
        'script[data-onauth]'
      )
      assert.ok(script)
      return script
    })
    const callbackName = initialScript
      .getAttribute('data-onauth')
      ?.replace(/\(user\)$/, '')
    assert.ok(callbackName)

    view.rerender(
      <TelegramBindDialog
        open
        onOpenChange={secondOpenChange}
        botName='max_api_bot'
        onSuccess={secondSuccess}
      />
    )

    await act(async () => Promise.resolve())
    assert.equal(
      document.querySelector<HTMLScriptElement>('script[data-onauth]'),
      initialScript
    )

    const callback = (
      window as unknown as Record<
        string,
        (authorization: {
          id: number
          auth_date: number
          hash: string
        }) => Promise<void>
      >
    )[callbackName]
    assert.equal(typeof callback, 'function')

    await act(async () => {
      await callback({ id: 123, auth_date: 456, hash: 'telegram-hash' })
    })

    assert.equal(firstSuccess.mock.calls.length, 0)
    assert.equal(firstOpenChange.mock.calls.length, 0)
    assert.equal(secondSuccess.mock.calls.length, 1)
    assert.deepEqual(secondOpenChange.mock.calls[0], [false])
  })

  test('keeps the active widget mounted across gate identity changes', async () => {
    useUnstableVerificationGate = true
    const view = render(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    const initialScript = await waitFor(() => {
      const script = document.querySelector<HTMLScriptElement>(
        'script[data-onauth]'
      )
      assert.ok(script)
      return script
    })
    const callbackName = initialScript
      .getAttribute('data-onauth')
      ?.replace(/\(user\)$/, '')
    assert.ok(callbackName)
    const initialCallback = (window as unknown as Record<string, unknown>)[
      callbackName
    ]
    assert.equal(typeof initialCallback, 'function')

    view.rerender(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    await act(async () => Promise.resolve())
    assert.equal(
      document.querySelector<HTMLScriptElement>('script[data-onauth]'),
      initialScript
    )
    assert.equal(
      (window as unknown as Record<string, unknown>)[callbackName],
      initialCallback
    )
    assert.equal(createTelegramBindState.mock.calls.length, 1)
  })

  test('reports initialization failures through the shared server error handler', async () => {
    const initializationError = new Error('backend unavailable')
    createTelegramBindState.mockImplementationOnce(async () => {
      throw initializationError
    })

    const view = render(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    await waitFor(() => assert.equal(handleServerError.mock.calls.length, 1))
    assert.deepEqual(handleServerError.mock.calls[0], [initializationError, {
      fallback: 'Failed to initialize Telegram binding',
    }])
    const alert = view.getByRole('alert')
    assert.equal(alert.textContent, 'Failed to initialize Telegram binding')
    const retryIcon = view.getByRole('button', { name: 'Retry' }).querySelector('svg')
    assert.equal(retryIcon?.getAttribute('aria-hidden'), 'true')
  })

  test('reports binding failures through the shared server error handler', async () => {
    const view = render(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    const script = await waitFor(() => {
      const element = document.querySelector<HTMLScriptElement>(
        'script[data-onauth]'
      )
      assert.ok(element)
      return element
    })
    const callbackName = script
      .getAttribute('data-onauth')
      ?.replace(/\(user\)$/, '')
    assert.ok(callbackName)
    bindTelegramAccount.mockImplementationOnce(async () => {
      throw new Error('binding failed')
    })

    const callback = (window as unknown as Record<string, unknown>)[
      callbackName
    ] as (authorization: {
      id: number
      auth_date: number
      hash: string
    }) => Promise<void>
    await act(async () => {
      await callback({ id: 123, auth_date: 456, hash: 'telegram-hash' })
    })

    await waitFor(() => assert.equal(handleServerError.mock.calls.length, 1))
    assert.equal(view.getByText('Failed to bind Telegram account') !== null, true)
  })
})
