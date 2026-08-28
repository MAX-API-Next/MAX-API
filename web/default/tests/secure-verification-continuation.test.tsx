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
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
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

const checkVerificationMethods = mock(async () => ({
  has2FA: false,
  hasPasskey: false,
  hasPassword: true,
  passkeySupported: false,
}))
const verify = mock(async () => undefined)
const toastError = mock((_message: string) => undefined)
const toastInfo = mock((_message: string) => undefined)
const toastSuccess = mock((_message: string) => undefined)

mock.module('i18next', () => ({
  default: { t: (key: string) => key },
}))

mock.module('sonner', () => ({
  toast: {
    error: toastError,
    info: toastInfo,
    success: toastSuccess,
  },
}))

mock.module('../src/features/auth/secure-verification/api', () => ({
  checkVerificationMethods,
  verify,
}))

const { useSecureVerification } = await import(
  '../src/features/auth/secure-verification/hooks/use-secure-verification'
)
const testEnv = createReactTestEnvironment()

const verificationRequiredError = {
  response: {
    status: 403,
    data: {
      code: 'VERIFICATION_REQUIRED',
      message: 'Additional verification is required',
    },
  },
}

beforeAll(() => testEnv.setup())

beforeEach(() => {
  checkVerificationMethods.mockClear()
  verify.mockClear()
  toastError.mockClear()
  toastInfo.mockClear()
  toastSuccess.mockClear()
})

afterEach(() => cleanup())

afterAll(() => testEnv.teardown())

describe('useSecureVerification', () => {
  test('continues the original API call after verification succeeds', async () => {
    let callCount = 0
    const apiCall = mock(async () => {
      callCount += 1
      if (callCount === 1) throw verificationRequiredError
      return { success: true }
    })
    const { result } = renderHook(() => useSecureVerification())

    let continuation: Promise<{ success: boolean } | null> | undefined
    act(() => {
      continuation = result.current.withVerification(apiCall, {
        scope: 'api_token',
      })
    })

    await waitFor(() => assert.equal(result.current.open, true))
    await act(async () => {
      await result.current.executeVerification('password', 'secret')
    })

    assert.deepEqual(await continuation, { success: true })
    assert.equal(apiCall.mock.calls.length, 2)
    assert.equal(verify.mock.calls.length, 1)
    assert.equal(result.current.open, false)
  })

  test('resolves null when the verification dialog is cancelled', async () => {
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())

    let continuation: Promise<unknown | null> | undefined
    act(() => {
      continuation = result.current.withVerification(apiCall, {
        scope: 'credentials',
      })
    })

    await waitFor(() => assert.equal(result.current.open, true))
    act(() => result.current.cancel())

    assert.equal(await continuation, null)
    assert.equal(result.current.open, false)
  })

  test('resolves a pending continuation when reset is called', async () => {
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())

    let continuation: Promise<unknown | null> | undefined
    act(() => {
      continuation = result.current.withVerification(apiCall, {
        scope: 'credentials',
      })
    })

    await waitFor(() => assert.equal(result.current.open, true))
    act(() => result.current.reset())

    assert.equal(await continuation, null)
    assert.equal(result.current.open, false)
  })

  test('reports verification method discovery failures before rejecting', async () => {
    checkVerificationMethods.mockImplementationOnce(async () => {
      throw new Error('method discovery unavailable')
    })
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())

    await assert.rejects(
      result.current.withVerification(apiCall, { scope: 'credentials' }),
      /method discovery unavailable/
    )

    assert.equal(toastError.mock.calls.length, 1)
    assert.equal(toastError.mock.calls[0]?.[0], 'Verification unavailable')
  })

  test('clears loading after a retryable verification failure', async () => {
    verify.mockImplementationOnce(async () => {
      throw new Error('Incorrect password')
    })
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())
    let continuation: Promise<unknown | null> | undefined

    await act(async () => {
      continuation = result.current.withVerification(apiCall, {
        scope: 'api_token',
      })
      await Promise.resolve()
    })
    await waitFor(() => assert.equal(result.current.open, true))

    let verificationError: unknown
    await act(async () => {
      try {
        await result.current.executeVerification('password', 'wrong')
      } catch (error) {
        verificationError = error
      }
    })

    assert.match(String(verificationError), /Incorrect password/)
    assert.equal(result.current.state.loading, false)
    assert.equal(result.current.open, true)
    act(() => result.current.cancel())
    assert.equal(await continuation, null)
  })
})
