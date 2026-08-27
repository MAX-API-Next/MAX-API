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
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterAll, afterEach, beforeAll, beforeEach, mock, test } from 'bun:test'
import assert from 'node:assert/strict'

const withVerification = mock(async () => null)

mock.module('../src/stores/auth-store', () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ auth: { user: { id: 77 } } }),
}))

mock.module('../src/features/auth/secure-verification', () => ({
  useSecureVerificationGate: () => ({ withVerification }),
}))

mock.module('../src/features/keys/api', () => ({
  getApiKeys: mock(async () => ({ success: true, data: { items: [] } })),
  fetchTokenKey: mock(async () => ({ success: false })),
}))

mock.module('i18next', () => ({
  default: { t: (key: string) => key },
}))

const { useActiveChatKey } = await import(
  '../src/features/chat/hooks/use-active-chat-key'
)
const testEnv = createReactTestEnvironment()

beforeAll(() => testEnv.setup())
beforeEach(() => withVerification.mockClear())
afterEach(() => cleanup())
afterAll(() => testEnv.teardown())

test('does not retry a cancelled chat API-key verification', async () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: 2, retryDelay: 0 },
    },
  })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )

  const { result } = renderHook(() => useActiveChatKey(true), { wrapper })

  await waitFor(() => assert.equal(result.current.isError, true))
  assert.equal(withVerification.mock.calls.length, 1)
  queryClient.clear()
})
