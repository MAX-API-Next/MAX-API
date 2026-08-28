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
import { afterAll, beforeEach, mock, spyOn, test } from 'bun:test'
import assert from 'node:assert/strict'

const apiGet = mock(async () => ({
  data: {
    success: true,
    data: {
      has_2fa: false,
      has_passkey: false,
      has_password: true,
    },
  },
}))
const consoleError = spyOn(console, 'error').mockImplementation(() => undefined)

const passkeyManagementStub = {
  status: null,
  loading: false,
  registering: false,
  removing: false,
  supported: true,
  enabled: false,
  lastUsed: null,
  register: mock(async () => true),
  remove: mock(async () => true),
}

mock.module('../src/lib/api', () => ({
  api: {
    get: apiGet,
  },
}))

mock.module('../src/lib/passkey', () => ({
  buildAssertionResult: mock(() => null),
  prepareCredentialRequestOptions: mock(() => ({})),
  isPasskeySupported: mock(async () => false),
}))

mock.module('../src/features/auth/passkey', () => ({
  beginPasskeyVerification: mock(async () => ({})),
  finishPasskeyVerification: mock(async () => ({})),
  usePasskeyManagement: () => passkeyManagementStub,
}))

const { checkVerificationMethods } = await import(
  '../src/features/auth/secure-verification/api'
)

beforeEach(() => {
  apiGet.mockClear()
  consoleError.mockClear()
})

afterAll(() => consoleError.mockRestore())

test('propagates verification method request failures', async () => {
  apiGet.mockImplementationOnce(async () => {
    throw new Error('verification methods unavailable')
  })

  await assert.rejects(
    checkVerificationMethods('credentials'),
    /verification methods unavailable/
  )
  assert.equal(consoleError.mock.calls.length, 1)
})

test('rejects unsuccessful verification method responses', async () => {
  apiGet.mockImplementationOnce(async () => ({
    data: {
      success: false,
      message: 'failed to load verification methods',
    },
  }))

  await assert.rejects(
    checkVerificationMethods('credentials'),
    /failed to load verification methods/
  )
  assert.equal(consoleError.mock.calls.length, 1)
})

test('preserves a successful response with no available methods', async () => {
  apiGet.mockImplementationOnce(async () => ({
    data: {
      success: true,
      data: {
        has_2fa: false,
        has_passkey: false,
        has_password: false,
      },
    },
  }))

  assert.deepEqual(await checkVerificationMethods('credentials'), {
    has2FA: false,
    hasPasskey: false,
    hasPassword: false,
    passkeySupported: false,
  })
  assert.equal(consoleError.mock.calls.length, 0)
})
