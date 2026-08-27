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

let resolveTokenKeyRequest:
  | ((value: {
      success: boolean
      data: { key: string }
    }) => void)
  | undefined

const fetchTokenKey = mock(
  () =>
    new Promise<{ success: boolean; data: { key: string } }>((resolve) => {
      resolveTokenKeyRequest = resolve
    })
)

mock.module('../src/features/auth/secure-verification', () => ({
  useSecureVerificationGate: () => ({
    withVerification: <T,>(apiCall: () => Promise<T>) => apiCall(),
  }),
}))

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('../src/features/keys/api', () => ({
  fetchTokenKey,
  fetchTokenKeysBatch: mock(async () => ({ success: true, data: { keys: {} } })),
}))

const { ApiKeysProvider, useApiKeys } = await import(
  '../src/features/keys/components/api-keys-provider'
)
const testEnv = createReactTestEnvironment()

function wrapper(props: { children: ReactNode }) {
  return <ApiKeysProvider>{props.children}</ApiKeysProvider>
}

beforeAll(() => testEnv.setup())

beforeEach(() => {
  fetchTokenKey.mockClear()
  resolveTokenKeyRequest = undefined
})

afterEach(() => cleanup())

afterAll(() => testEnv.teardown())

describe('API key plaintext lifecycle', () => {
  test('does not cache a key that resolves after its reveal popover closes', async () => {
    const { result } = renderHook(() => useApiKeys(), { wrapper })

    let request: Promise<string | null> | undefined
    act(() => {
      request = result.current.resolveRealKey(101, { cache: true })
    })

    await waitFor(() => assert.equal(fetchTokenKey.mock.calls.length, 1))
    act(() => result.current.clearResolvedKey(101))

    await act(async () => {
      resolveTokenKeyRequest?.({
        success: true,
        data: { key: 'plaintext-test-key' },
      })
      assert.equal(await request, 'sk-plaintext-test-key')
    })

    assert.equal(result.current.resolvedKeys[101], undefined)
  })
})
