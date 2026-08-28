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
import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, beforeEach, mock, test } from 'bun:test'
import assert from 'node:assert/strict'
import type { ReactNode } from 'react'

const setup2FA = mock(async () => ({
  success: true,
  data: {
    qr_code_data: 'otpauth://totp/MAX-API:test',
    secret: 'TESTSECRET',
    backup_codes: ['ABCD-EFGH'],
  },
}))
const enable2FA = mock(async () => ({ success: true }))
const withVerification = mock(<T,>(apiCall: () => Promise<T>) => apiCall())
const handleServerError = mock(() => undefined)

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('sonner', () => ({
  toast: {
    error: mock(() => undefined),
    success: mock(() => undefined),
  },
}))

mock.module('qrcode.react', () => ({
  QRCodeSVG: () => <div data-testid='qr-code' />,
}))

function TestContainer(props: { children?: ReactNode }) {
  return <div>{props.children}</div>
}

mock.module('../src/components/ui/alert', () => ({
  Alert: TestContainer,
  AlertDescription: TestContainer,
}))

mock.module('../src/components/ui/button', () => ({
  Button: (props: { children?: ReactNode; onClick?: () => void }) => (
    <button type='button' onClick={props.onClick}>
      {props.children}
    </button>
  ),
}))

mock.module('../src/components/ui/dialog', () => ({
  Dialog: TestContainer,
  DialogContent: TestContainer,
  DialogDescription: TestContainer,
  DialogFooter: TestContainer,
  DialogHeader: TestContainer,
  DialogTitle: TestContainer,
}))

mock.module('../src/components/ui/input', () => ({
  Input: (props: {
    value?: string
    onChange?: (event: { target: { value: string } }) => void
  }) => <input value={props.value} onInput={props.onChange} />,
}))

mock.module('../src/components/ui/label', () => ({
  Label: TestContainer,
}))

mock.module('../src/components/copy-button', () => ({
  CopyButton: TestContainer,
}))

mock.module('../src/lib/api', () => ({
  setup2FA,
  enable2FA,
}))

mock.module('../src/lib/handle-server-error', () => ({
  handleServerError,
}))

mock.module('../src/lib/secure-verification', () => ({
  wasSecureVerificationErrorReported: () => false,
}))

mock.module('../src/features/auth/secure-verification', () => ({
  useSecureVerificationGate: () => ({ withVerification }),
}))

const { TwoFASetupDialog } = await import(
  '../src/features/profile/components/dialogs/two-fa-setup-dialog'
)
const testEnv = createReactTestEnvironment()

beforeAll(() => testEnv.setup())
beforeEach(() => {
  setup2FA.mockClear()
  enable2FA.mockClear()
  withVerification.mockClear()
  handleServerError.mockClear()
})
afterEach(() => cleanup())
afterAll(() => testEnv.teardown())

test('uses the authenticated layout verification gate for 2FA setup', async () => {
  const view = render(
    <TwoFASetupDialog
      open
      onOpenChange={mock(() => undefined)}
      onSuccess={mock(() => undefined)}
    />
  )

  assert.ok(view.getByText('Setting up 2FA...'))
  await waitFor(() => assert.equal(setup2FA.mock.calls.length, 1))
  assert.equal(withVerification.mock.calls.length, 1)
})

test('does not restart 2FA setup while initialization is pending', async () => {
  let resolveSetup:
    | ((value: Awaited<ReturnType<typeof setup2FA>>) => void)
    | undefined
  setup2FA.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveSetup = resolve
      })
  )

  const view = render(
    <TwoFASetupDialog
      open
      onOpenChange={mock(() => undefined)}
      onSuccess={mock(() => undefined)}
    />
  )
  await waitFor(() => assert.equal(setup2FA.mock.calls.length, 1))

  view.rerender(
    <TwoFASetupDialog
      open
      onOpenChange={mock(() => undefined)}
      onSuccess={mock(() => undefined)}
    />
  )
  await act(async () => {
    await new Promise<void>((resolve) => setTimeout(resolve, 0))
  })
  assert.equal(setup2FA.mock.calls.length, 1)

  await act(async () => {
    resolveSetup?.({
      success: true,
      data: {
        qr_code_data: 'otpauth://totp/MAX-API:test',
        secret: 'TESTSECRET',
        backup_codes: ['ABCD-EFGH'],
      },
    })
    await Promise.resolve()
  })
})

test('reports setup failures with the server error message path', async () => {
  const error = new Error('setup request failed')
  setup2FA.mockImplementationOnce(async () => {
    throw error
  })

  render(
    <TwoFASetupDialog
      open
      onOpenChange={mock(() => undefined)}
      onSuccess={mock(() => undefined)}
    />
  )

  await waitFor(() => assert.equal(handleServerError.mock.calls.length, 1))
  assert.deepEqual(handleServerError.mock.calls[0], [
    error,
    { fallback: 'Failed to setup 2FA' },
  ])
})

test('reports enable failures with the server error message path', async () => {
  const error = new Error('enable request failed')
  enable2FA.mockImplementationOnce(async () => {
    throw error
  })

  const view = render(
    <TwoFASetupDialog
      open
      onOpenChange={mock(() => undefined)}
      onSuccess={mock(() => undefined)}
    />
  )

  await waitFor(() => assert.equal(setup2FA.mock.calls.length, 1))
  await waitFor(() => assert.ok(view.getByRole('button', { name: 'Next' })))
  fireEvent.click(view.getByRole('button', { name: 'Next' }))
  fireEvent.click(view.getByRole('button', { name: 'Next' }))
  const input = view.getByRole('textbox')
  await act(async () => {
    fireEvent.input(input, { target: { value: '123456' } })
    await Promise.resolve()
  })
  assert.equal(input.getAttribute('value'), '123456')
  fireEvent.click(view.getByRole('button', { name: 'Enable 2FA' }))

  await waitFor(() => assert.equal(enable2FA.mock.calls.length, 1))
  await waitFor(() => assert.equal(handleServerError.mock.calls.length, 1))
  assert.deepEqual(handleServerError.mock.calls[0], [
    error,
    { fallback: 'Failed to enable 2FA' },
  ])
})
