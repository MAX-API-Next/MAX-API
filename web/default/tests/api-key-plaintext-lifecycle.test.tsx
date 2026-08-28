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
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  waitFor,
} from '@testing-library/react'
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
const copyToClipboard = mock(async () => false)
const toastError = mock(() => undefined)

mock.module('../src/features/auth/secure-verification', () => ({
  useSecureVerificationGate: () => ({
    withVerification: <T,>(apiCall: () => Promise<T>) => apiCall(),
  }),
}))

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('sonner', () => ({
  toast: { error: toastError },
}))

mock.module('../src/lib/copy-to-clipboard', () => ({
  copyToClipboard,
}))

mock.module('../src/features/keys/api', () => ({
  fetchTokenKey,
  fetchTokenKeysBatch: mock(async () => ({ success: true, data: { keys: {} } })),
}))

const { ApiKeysProvider, useApiKeys } = await import(
  '../src/features/keys/components/api-keys-provider'
)
const { ApiKeyCell } = await import(
  '../src/features/keys/components/api-keys-cells'
)
const testEnv = createReactTestEnvironment()

function wrapper(props: { children: ReactNode }) {
  return <ApiKeysProvider>{props.children}</ApiKeysProvider>
}

beforeAll(() => testEnv.setup())

beforeEach(() => {
  fetchTokenKey.mockClear()
  copyToClipboard.mockClear()
  toastError.mockClear()
  resolveTokenKeyRequest = undefined
})

afterEach(async () => {
  cleanup()
  await new Promise((resolve) => setTimeout(resolve, 50))
})

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

  test('reports clipboard failures without marking the API key as copied', async () => {
    fetchTokenKey.mockImplementationOnce(async () => ({
      success: true,
      data: { key: 'plaintext-copy-key' },
    }))

    const view = render(
      <ApiKeysProvider>
        <ApiKeyCell
          apiKey={{
            id: 102,
            name: 'copy-test',
            key: 'masked-key',
            status: 1,
            remain_quota: 100,
            used_quota: 0,
            unlimited_quota: false,
            expired_time: -1,
            created_time: 0,
            accessed_time: 0,
            group: 'default',
            cross_group_retry: false,
            routing: null,
            model_limits_enabled: false,
            model_limits: '',
            allow_ips: '',
          }}
        />
      </ApiKeysProvider>
    )

    fireEvent.click(view.getByRole('button', { name: 'Copy API key' }))

    await waitFor(() => {
      assert.equal(copyToClipboard.mock.calls.length, 1)
      assert.equal(toastError.mock.calls.length, 1)
      assert.equal(toastError.mock.calls[0]?.[0], 'Failed to copy to clipboard')
    })
  })
})
