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
import type { ReactElement, ReactNode } from 'react'
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

interface TelegramBindStateResponse {
  success: boolean
  data?: { state: string }
  message?: string
}

interface TelegramBindResponse {
  success: boolean
  message?: string
}

const createTelegramBindState = mock(async (): Promise<TelegramBindStateResponse> => ({
  success: true,
  data: { state: 'telegram-bind-state' },
}))
const bindTelegramAccount = mock(async (): Promise<TelegramBindResponse> => ({ success: true }))
const toastSuccess = mock((): void => undefined)
const toastError = mock((): void => undefined)
const handleServerError = mock((): void => undefined)
const translate = (key: string): string => key
const withVerification = <T,>(apiCall: () => Promise<T>): Promise<T> => apiCall()
let useUnstableVerificationGate = false

interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
}

function createDeferred<T>(): Deferred<T> {
  let resolve: ((value: T) => void) | undefined
  const promise = new Promise<T>((resolver) => {
    resolve = resolver
  })
  return {
    promise,
    resolve: (value) => resolve?.(value),
  }
}

mock.module('../src/features/auth/secure-verification', (): object => ({
  useSecureVerificationGate: (): object => ({
    withVerification: useUnstableVerificationGate
      ? <T,>(apiCall: () => Promise<T>): Promise<T> => apiCall()
      : withVerification,
  }),
}))

mock.module('../src/features/profile/api', (): object => ({
  bindTelegramAccount,
  createTelegramBindState,
}))

mock.module('../src/lib/handle-server-error', (): object => ({ handleServerError }))

mock.module('sonner', (): object => ({
  toast: { error: toastError, success: toastSuccess },
}))

mock.module('react-i18next', (): object => ({
  useTranslation: (): object => ({ t: translate }),
}))

function TestContainer(props: { children?: ReactNode }): ReactElement {
  return <div>{props.children}</div>
}

mock.module('../src/components/ui/alert', (): object => ({
  Alert: TestContainer,
  AlertDescription: TestContainer,
}))

mock.module('../src/components/ui/button', (): object => ({
  Button: (props: {
    children?: ReactNode
    onClick?: () => void
    type?: 'button' | 'submit' | 'reset'
  }): ReactElement => (
    <button type={props.type} onClick={props.onClick}>
      {props.children}
    </button>
  ),
}))

mock.module('../src/components/ui/dialog', (): object => ({
  Dialog: TestContainer,
  DialogContent: TestContainer,
  DialogDescription: TestContainer,
  DialogHeader: TestContainer,
  DialogTitle: TestContainer,
}))

mock.module('../src/components/ui/spinner', (): object => ({
  Spinner: (): ReactElement => <span />,
}))

const { TelegramBindDialog } = await import(
  '../src/features/profile/components/dialogs/telegram-bind-dialog'
)
const testEnv = createReactTestEnvironment()

beforeAll(async (): Promise<void> => testEnv.setup())
beforeEach((): void => {
  useUnstableVerificationGate = false
  createTelegramBindState.mockClear()
  bindTelegramAccount.mockClear()
  toastSuccess.mockClear()
  toastError.mockClear()
  handleServerError.mockClear()
  assert.equal(document.querySelector('script[data-onauth]'), null)
})
afterEach((): void => cleanup())
afterAll((): void => testEnv.teardown())

describe('TelegramBindDialog widget lifecycle', (): void => {
  test('renders the bot name with exactly one leading at-sign', (): void => {
    const prefixedView = render(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='@max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    assert.equal(
      prefixedView.getByText('@max_api_bot').textContent,
      '@max_api_bot'
    )
    prefixedView.unmount()

    const plainView = render(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    assert.equal(
      plainView.getByText('@max_api_bot').textContent,
      '@max_api_bot'
    )
  })

  test('ignores an obsolete bind-state response after close and reopen', async (): Promise<void> => {
    const firstAttempt = createDeferred<{
      success: boolean
      data: { state: string }
    }>()
    const secondAttempt = createDeferred<{
      success: boolean
      data: { state: string }
    }>()
    createTelegramBindState
      .mockImplementationOnce(() => firstAttempt.promise)
      .mockImplementationOnce(() => secondAttempt.promise)
    const onOpenChange = mock(() => undefined)
    const onSuccess = mock(() => undefined)
    const view = render(
      <TelegramBindDialog
        open
        onOpenChange={onOpenChange}
        botName='max_api_bot'
        onSuccess={onSuccess}
      />
    )

    await waitFor(() =>
      assert.equal(createTelegramBindState.mock.calls.length, 1)
    )
    view.rerender(
      <TelegramBindDialog
        open={false}
        onOpenChange={onOpenChange}
        botName='max_api_bot'
        onSuccess={onSuccess}
      />
    )
    view.rerender(
      <TelegramBindDialog
        open
        onOpenChange={onOpenChange}
        botName='max_api_bot'
        onSuccess={onSuccess}
      />
    )
    await waitFor(() =>
      assert.equal(createTelegramBindState.mock.calls.length, 2)
    )

    await act(async () => {
      firstAttempt.resolve({ success: true, data: { state: 'obsolete' } })
      await firstAttempt.promise
    })
    assert.equal(document.querySelector('script[data-onauth]'), null)

    await act(async () => {
      secondAttempt.resolve({ success: true, data: { state: 'current' } })
      await secondAttempt.promise
    })
    await waitFor(() =>
      assert.notEqual(document.querySelector('script[data-onauth]'), null)
    )
  })

  test('keeps the active widget mounted across callback-only rerenders', async (): Promise<void> => {
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

  test('keeps the active widget mounted across gate identity changes', async (): Promise<void> => {
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

  test('reports initialization failures through the shared server error handler', async (): Promise<void> => {
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

  test('preserves Telegram initialization business messages inline', async (): Promise<void> => {
    createTelegramBindState.mockImplementationOnce(async () => ({
      success: false,
      message: 'Telegram state is unavailable',
    }))

    const view = render(
      <TelegramBindDialog
        open
        onOpenChange={mock(() => undefined)}
        botName='max_api_bot'
        onSuccess={mock(() => undefined)}
      />
    )

    await waitFor(() => assert.equal(toastError.mock.calls.length, 1))
    assert.equal(view.getByRole('alert').textContent, 'Telegram state is unavailable')
    assert.equal(toastError.mock.calls[0]?.[0], 'Telegram state is unavailable')
    assert.equal(handleServerError.mock.calls.length, 0)
  })

  test('reports binding failures through the shared server error handler', async (): Promise<void> => {
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

  test('preserves Telegram binding business messages inline', async (): Promise<void> => {
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
    bindTelegramAccount.mockImplementationOnce(async () => ({
      success: false,
      message: 'Telegram account is already bound',
    }))

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

    assert.equal(
      view.getByRole('alert').textContent,
      'Telegram account is already bound'
    )
    assert.equal(toastError.mock.calls.length, 1)
    assert.equal(toastError.mock.calls[0]?.[0], 'Telegram account is already bound')
    assert.equal(handleServerError.mock.calls.length, 0)
  })
})
