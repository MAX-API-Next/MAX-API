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
import { wasSecureVerificationErrorReported } from '@/lib/secure-verification'
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

const availableVerificationMethods = {
  has2FA: false,
  hasPasskey: false,
  hasPassword: true,
  passkeySupported: false,
}
const checkVerificationMethods = mock(
  async () => availableVerificationMethods
)
const verify = mock(async () => undefined)
const toastError = mock((_message: string) => undefined)
const toastInfo = mock((_message: string) => undefined)
const toastSuccess = mock((_message: string) => undefined)
const handleServerError = mock(
  (_error: unknown, _options?: { fallback?: string }) => undefined
)

interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason?: unknown) => void
}

function createDeferred<T>(): Deferred<T> {
  let resolve: ((value: T) => void) | undefined
  let reject: ((reason?: unknown) => void) | undefined
  const promise = new Promise<T>((resolver, rejecter) => {
    resolve = resolver
    reject = rejecter
  })
  return {
    promise,
    resolve: (value) => resolve?.(value),
    reject: (reason) => reject?.(reason),
  }
}

async function flushMacrotask(): Promise<void> {
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0)
  })
}

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

mock.module('../src/lib/handle-server-error', () => ({
  handleServerError,
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
  checkVerificationMethods.mockImplementation(
    async () => availableVerificationMethods
  )
  verify.mockClear()
  verify.mockImplementation(async () => undefined)
  toastError.mockClear()
  toastInfo.mockClear()
  toastSuccess.mockClear()
  handleServerError.mockClear()
})

afterEach(() => cleanup())

afterAll(() => testEnv.teardown())

describe('useSecureVerification', () => {
  test('returns an initial protected action success without discovery', async () => {
    const apiCall = mock(async () => ({ success: true }))
    const { result } = renderHook(() => useSecureVerification())

    assert.deepEqual(
      await result.current.withVerification(apiCall, {
        scope: 'credentials',
      }),
      { success: true }
    )
    assert.equal(apiCall.mock.calls.length, 1)
    assert.equal(checkVerificationMethods.mock.calls.length, 0)
    assert.equal(result.current.open, false)
  })

  test('rejects an initial protected action failure without discovery', async () => {
    const actionError = new Error('protected action failed')
    const apiCall = mock(async () => {
      throw actionError
    })
    const { result } = renderHook(() => useSecureVerification())

    await assert.rejects(
      result.current.withVerification(apiCall, { scope: 'credentials' }),
      /protected action failed/
    )
    assert.equal(apiCall.mock.calls.length, 1)
    assert.equal(checkVerificationMethods.mock.calls.length, 0)
    assert.equal(result.current.open, false)
    assert.equal(wasSecureVerificationErrorReported(actionError), false)
  })

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

  test('settles reset while the initial protected action is pending', async () => {
    const initialAction = createDeferred<never>()
    const apiCall = mock(() => initialAction.promise)
    const { result } = renderHook(() => useSecureVerification())

    const continuation = result.current.withVerification(apiCall, {
      scope: 'credentials',
    })
    await waitFor(() => assert.equal(apiCall.mock.calls.length, 1))

    act(() => result.current.reset())
    assert.equal(await continuation, null)

    initialAction.reject(verificationRequiredError)
    await flushMacrotask()
    assert.equal(checkVerificationMethods.mock.calls.length, 0)
    assert.equal(result.current.open, false)
  })

  test('does not retry the protected action after reset during verification', async () => {
    const verification = createDeferred<void>()
    verify.mockImplementationOnce(() => verification.promise)
    let callCount = 0
    const apiCall = mock(async () => {
      callCount += 1
      if (callCount === 1) throw verificationRequiredError
      return { success: true }
    })
    const { result } = renderHook(() => useSecureVerification())

    const continuation = result.current.withVerification(apiCall, {
      scope: 'credentials',
    })
    await waitFor(() => assert.equal(result.current.open, true))

    let execution: Promise<unknown> | undefined
    act(() => {
      execution = result.current.executeVerification('password', 'secret')
    })
    await waitFor(() => assert.equal(verify.mock.calls.length, 1))

    act(() => result.current.reset())
    assert.equal(await continuation, null)

    await act(async () => {
      verification.resolve()
      await execution
    })

    assert.equal(apiCall.mock.calls.length, 1)
    assert.equal(toastSuccess.mock.calls.length, 0)
  })

  test('settles reset before verification method discovery completes', async () => {
    const discovery = createDeferred<typeof availableVerificationMethods>()
    checkVerificationMethods.mockImplementationOnce(() => discovery.promise)
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())

    const continuation = result.current.withVerification(apiCall, {
      scope: 'credentials',
    })
    await waitFor(() =>
      assert.equal(checkVerificationMethods.mock.calls.length, 1)
    )

    act(() => result.current.reset())
    const outcome = await Promise.race([
      continuation.then((value) => ({ settled: true, value })),
      new Promise<{ settled: false }>((resolve) => {
        setTimeout(() => resolve({ settled: false }), 50)
      }),
    ])

    await act(async () => {
      discovery.resolve(availableVerificationMethods)
      await Promise.resolve()
    })

    assert.deepEqual(outcome, { settled: true, value: null })
    assert.equal(result.current.open, false)
  })

  test('settles unmount before verification method discovery completes', async () => {
    const discovery = createDeferred<typeof availableVerificationMethods>()
    checkVerificationMethods.mockImplementationOnce(() => discovery.promise)
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result, unmount } = renderHook(() => useSecureVerification())

    const continuation = result.current.withVerification(apiCall, {
      scope: 'credentials',
    })
    await waitFor(() =>
      assert.equal(checkVerificationMethods.mock.calls.length, 1)
    )

    unmount()
    const outcome = await Promise.race([
      continuation.then((value) => ({ settled: true, value })),
      new Promise<{ settled: false }>((resolve) => {
        setTimeout(() => resolve({ settled: false }), 50)
      }),
    ])
    discovery.resolve(availableVerificationMethods)
    await flushMacrotask()

    assert.deepEqual(outcome, { settled: true, value: null })
    assert.equal(apiCall.mock.calls.length, 1)
  })

  test('does not let an obsolete discovery settle a newer continuation', async () => {
    const firstDiscovery = createDeferred<typeof availableVerificationMethods>()
    const secondDiscovery = createDeferred<
      typeof availableVerificationMethods
    >()
    checkVerificationMethods
      .mockImplementationOnce(() => firstDiscovery.promise)
      .mockImplementationOnce(() => secondDiscovery.promise)
    const firstCall = mock(async () => {
      throw verificationRequiredError
    })
    const secondCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())

    const firstContinuation = result.current.withVerification(firstCall, {
      scope: 'credentials',
    })
    await waitFor(() =>
      assert.equal(checkVerificationMethods.mock.calls.length, 1)
    )
    const secondContinuation = result.current.withVerification(secondCall, {
      scope: 'api_token',
    })
    await waitFor(() =>
      assert.equal(checkVerificationMethods.mock.calls.length, 2)
    )

    assert.equal(await firstContinuation, null)
    await act(async () => {
      firstDiscovery.resolve(availableVerificationMethods)
      await Promise.resolve()
    })
    assert.equal(result.current.open, false)

    await act(async () => {
      secondDiscovery.resolve(availableVerificationMethods)
      await Promise.resolve()
    })
    await waitFor(() => assert.equal(result.current.open, true))
    assert.equal(result.current.state.scope, 'api_token')

    act(() => result.current.cancel())
    assert.equal(await secondContinuation, null)
  })

  test('reports verification method discovery failures before rejecting', async () => {
    const discoveryError = new Error('method discovery unavailable')
    checkVerificationMethods.mockImplementationOnce(async () => {
      throw discoveryError
    })
    const apiCall = mock(async () => {
      throw verificationRequiredError
    })
    const { result } = renderHook(() => useSecureVerification())

    await assert.rejects(
      result.current.withVerification(apiCall, { scope: 'credentials' }),
      /method discovery unavailable/
    )

    assert.equal(handleServerError.mock.calls.length, 1)
    assert.match(
      String(handleServerError.mock.calls[0]?.[0]),
      /method discovery unavailable/
    )
    assert.deepEqual(handleServerError.mock.calls[0]?.[1], {
      fallback: 'Verification unavailable',
    })
    assert.equal(wasSecureVerificationErrorReported(discoveryError), true)
    assert.equal(toastError.mock.calls.length, 0)
  })

  test('reports a protected action retry failure through the shared handler', async () => {
    const retryError = new Error('protected action retry failed')
    let callCount = 0
    const apiCall = mock(async () => {
      callCount += 1
      if (callCount === 1) throw verificationRequiredError
      throw retryError
    })
    const { result } = renderHook(() => useSecureVerification())

    const continuation = result.current.withVerification(apiCall, {
      scope: 'api_token',
    })
    const continuationOutcome = continuation.then(
      (value) => ({ status: 'resolved' as const, value }),
      (error: unknown) => ({ status: 'rejected' as const, error })
    )
    await waitFor(() => assert.equal(result.current.open, true))

    let executionError: unknown
    await act(async () => {
      try {
        await result.current.executeVerification('password', 'secret')
      } catch (error) {
        executionError = error
      }
    })
    assert.equal(executionError, retryError)

    const outcome = await continuationOutcome
    assert.equal(outcome.status, 'rejected')
    if (outcome.status === 'rejected') {
      assert.equal(outcome.error, retryError)
    }
    assert.equal(handleServerError.mock.calls.length, 1)
    assert.equal(handleServerError.mock.calls[0]?.[0], retryError)
    assert.deepEqual(handleServerError.mock.calls[0]?.[1], {
      fallback: 'protected action retry failed',
    })
    assert.equal(wasSecureVerificationErrorReported(retryError), true)
    assert.equal(toastError.mock.calls.length, 0)
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
