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
import { createReactTestEnvironment } from '@/test/react'
import { cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  mock,
  spyOn,
  test,
} from 'bun:test'
import assert from 'node:assert/strict'
import type { MouseEventHandler, ReactElement, ReactNode } from 'react'

const withVerification = mock(
  async <T,>(
    _apiCall: () => Promise<T>,
    _options?: { scope?: string }
  ): Promise<T | null> => {
    throw new Error('verification method lookup failed')
  }
)
const fetchVerificationMethods = mock(async () => ({
  has2FA: false,
  hasPasskey: false,
  hasPassword: true,
  passkeySupported: true,
}))
const startVerification = mock(async () => true)
const register = mock(async () => true)
const remove = mock(async () => true)
const toastError = mock((_message: string) => undefined)
const handleServerError = mock(
  (_error: unknown, _options?: { fallback?: string }) => undefined
)
const consoleError = spyOn(console, 'error').mockImplementation(() => undefined)
let passkeyEnabled = false

interface DialogPartProps {
  children?: ReactNode
}

interface DialogButtonProps extends DialogPartProps {
  disabled?: boolean
  onClick?: MouseEventHandler<HTMLButtonElement>
}

function DialogPart(props: DialogPartProps): ReactElement {
  return <div>{props.children}</div>
}

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('sonner', () => ({
  toast: {
    error: toastError,
    info: mock(() => undefined),
    success: mock(() => undefined),
  },
}))

mock.module('../src/lib/handle-server-error', () => ({
  handleServerError,
}))

mock.module('../src/components/ui/alert-dialog', () => ({
  AlertDialog: (props: DialogPartProps): ReactElement => <>{props.children}</>,
  AlertDialogAction: (props: DialogButtonProps): ReactElement => (
    <button
      type='button'
      disabled={props.disabled}
      onClick={props.onClick}
    >
      {props.children}
    </button>
  ),
  AlertDialogCancel: (props: DialogButtonProps): ReactElement => (
    <button type='button' disabled={props.disabled}>
      {props.children}
    </button>
  ),
  AlertDialogContent: DialogPart,
  AlertDialogDescription: DialogPart,
  AlertDialogFooter: DialogPart,
  AlertDialogHeader: DialogPart,
  AlertDialogTitle: DialogPart,
  AlertDialogTrigger: (props: DialogPartProps): ReactElement => (
    <button type='button'>{props.children}</button>
  ),
}))

mock.module('../src/features/auth/passkey', () => ({
  beginPasskeyVerification: mock(async () => ({})),
  finishPasskeyVerification: mock(async () => ({})),
  usePasskeyManagement: () => ({
    status: null,
    loading: false,
    registering: false,
    removing: false,
    supported: true,
    enabled: passkeyEnabled,
    lastUsed: null,
    register,
    remove,
  }),
}))

mock.module('../src/features/auth/secure-verification', () => ({
  SecureVerificationDialog: (): ReactElement | null => null,
  useSecureVerification: () => ({
    open: false,
    setOpen: mock(() => undefined),
    methods: {
      has2FA: false,
      hasPasskey: false,
      hasPassword: true,
      passkeySupported: true,
    },
    state: { method: null, loading: false, code: '' },
    startVerification,
    executeVerification: mock(async () => undefined),
    cancel: mock(() => undefined),
    setCode: mock(() => undefined),
    switchMethod: mock(() => undefined),
    fetchVerificationMethods,
    withVerification,
  }),
}))

const { PasskeyCard } = await import(
  '../src/features/profile/components/passkey-card'
)
const testEnv = createReactTestEnvironment()

beforeAll(() => {
  testEnv.setup()
})
beforeEach(() => {
  passkeyEnabled = false
  withVerification.mockClear()
  withVerification.mockImplementation(async () => {
    throw new Error('verification method lookup failed')
  })
  fetchVerificationMethods.mockClear()
  fetchVerificationMethods.mockImplementation(async () => ({
    has2FA: false,
    hasPasskey: false,
    hasPassword: true,
    passkeySupported: true,
  }))
  startVerification.mockClear()
  register.mockClear()
  remove.mockClear()
  toastError.mockClear()
  handleServerError.mockClear()
  consoleError.mockClear()
})
afterEach(() => cleanup())
afterAll(() => {
  consoleError.mockRestore()
  testEnv.teardown()
})

test('registers the first Passkey when a fresh OAuth reauthentication already satisfies the server gate', async () => {
  withVerification.mockImplementationOnce(async (apiCall) => apiCall())
  const view = render(<PasskeyCard loading={false} />)

  fireEvent.click(view.getByRole('button', { name: 'Enable Passkey' }))

  await waitFor(() => assert.equal(register.mock.calls.length, 1))
  assert.equal(withVerification.mock.calls.length, 1)
  assert.equal(withVerification.mock.calls[0]?.[1]?.scope, 'credentials')
})

test('handles a rejected Passkey verification continuation', async () => {
  const view = render(<PasskeyCard loading={false} />)

  fireEvent.click(view.getByRole('button', { name: 'Enable Passkey' }))

  await waitFor(() => assert.equal(withVerification.mock.calls.length, 1))
  await waitFor(() => assert.equal(consoleError.mock.calls.length, 1))
  assert.match(String(consoleError.mock.calls[0]?.[1]), /method lookup failed/)
  assert.equal(toastError.mock.calls.length, 0)
  assert.equal(handleServerError.mock.calls.length, 0)
})

test('stops Passkey removal when verification method discovery fails', async () => {
  passkeyEnabled = true
  fetchVerificationMethods.mockImplementationOnce(async () => {
    throw new Error('verification methods unavailable')
  })
  const view = render(<PasskeyCard loading={false} />)

  fireEvent.click(view.getByRole('button', { name: 'Remove' }))

  await waitFor(() =>
    assert.equal(fetchVerificationMethods.mock.calls.length, 1)
  )
  await waitFor(() => assert.equal(handleServerError.mock.calls.length, 1))
  assert.match(
    String(handleServerError.mock.calls[0]?.[0]),
    /verification methods unavailable/
  )
  assert.deepEqual(handleServerError.mock.calls[0]?.[1], {
    fallback: 'Verification unavailable',
  })
  assert.equal(toastError.mock.calls.length, 0)
  assert.equal(startVerification.mock.calls.length, 0)
  assert.equal(remove.mock.calls.length, 0)
})
