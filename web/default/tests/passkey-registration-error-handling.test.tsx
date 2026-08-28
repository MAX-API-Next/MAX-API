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

const withVerification = mock(async () => {
  throw new Error('verification method lookup failed')
})
const consoleError = spyOn(console, 'error').mockImplementation(() => undefined)

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('sonner', () => ({
  toast: {
    error: mock(() => undefined),
    info: mock(() => undefined),
    success: mock(() => undefined),
  },
}))

mock.module('../src/features/auth/passkey', () => ({
  usePasskeyManagement: () => ({
    status: null,
    loading: false,
    registering: false,
    removing: false,
    supported: true,
    enabled: false,
    lastUsed: null,
    register: mock(async () => true),
    remove: mock(async () => true),
  }),
}))

mock.module('../src/features/auth/secure-verification', () => ({
  SecureVerificationDialog: () => null,
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
    startVerification: mock(async () => true),
    executeVerification: mock(async () => undefined),
    cancel: mock(() => undefined),
    setCode: mock(() => undefined),
    switchMethod: mock(() => undefined),
    fetchVerificationMethods: mock(async () => ({
      has2FA: false,
      hasPasskey: false,
      hasPassword: true,
      passkeySupported: true,
    })),
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
  withVerification.mockClear()
  consoleError.mockClear()
})
afterEach(() => cleanup())
afterAll(() => {
  consoleError.mockRestore()
  testEnv.teardown()
})

test('handles a rejected Passkey verification continuation', async () => {
  const view = render(<PasskeyCard loading={false} />)

  fireEvent.click(view.getByRole('button', { name: 'Enable Passkey' }))

  await waitFor(() => assert.equal(withVerification.mock.calls.length, 1))
  await waitFor(() => assert.equal(consoleError.mock.calls.length, 1))
  assert.match(String(consoleError.mock.calls[0]?.[1]), /method lookup failed/)
})
