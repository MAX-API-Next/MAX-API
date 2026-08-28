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
import { markSecureVerificationErrorReported } from '@/lib/secure-verification'
import { cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  mock,
  test,
} from 'bun:test'
import assert from 'node:assert/strict'

let verificationError = new Error('request failed')
const withApiTokenVerification = mock(async () => {
  throw verificationError
})
const useApiTokenVerification = mock(
  (_description?: string) => withApiTokenVerification
)
const handleServerError = mock(
  (_error: unknown, _options?: { fallback?: string }) => undefined
)

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('sonner', () => ({
  toast: {
    error: mock(() => undefined),
    success: mock(() => undefined),
  },
}))

mock.module('@/features/auth/secure-verification', () => ({
  useApiTokenVerification,
}))

mock.module('@/lib/handle-server-error', () => ({ handleServerError }))

const { RequestPreview } = await import(
  '../src/features/dashboard/components/overview/overview-dashboard'
)
const testEnv = createReactTestEnvironment()

beforeAll(() => testEnv.setup())
beforeEach(() => {
  verificationError = new Error('request failed')
  withApiTokenVerification.mockClear()
  useApiTokenVerification.mockClear()
  handleServerError.mockClear()
})
afterEach(() => cleanup())
afterAll(() => testEnv.teardown())

function renderRequestPreview(): ReturnType<typeof render> {
  return render(
    <RequestPreview
      example={{
        endpoint: '/v1/responses',
        model: 'gpt-4.1-mini',
        keyName: 'dashboard-key',
        keyId: 42,
        displayKey: 'sk-••••••••',
        ready: true,
        unavailable: false,
      }}
      signals={[]}
      isRetrying={false}
      onRetry={mock(() => undefined)}
    />
  )
}

async function waitForCopyToSettle(
  view: ReturnType<typeof render>
): Promise<void> {
  await waitFor(() => {
    const button = view.getByRole('button', {
      name: 'Copy ready-to-run curl',
    })
    assert.equal(button.hasAttribute('disabled'), false)
  })
}

test('reports an unreported request-copy rejection and consumes it', async () => {
  const view = renderRequestPreview()

  fireEvent.click(view.getByRole('button', { name: 'Copy ready-to-run curl' }))

  await waitFor(() => assert.equal(handleServerError.mock.calls.length, 1))
  await waitForCopyToSettle(view)
  assert.equal(useApiTokenVerification.mock.calls[0]?.[0],
    'Confirm your identity before copying a request with your API key.'
  )
  assert.equal(handleServerError.mock.calls[0]?.[0], verificationError)
  assert.deepEqual(handleServerError.mock.calls[0]?.[1], {
    fallback: 'Failed to copy to clipboard',
  })
})

test('does not duplicate a rejection already reported by verification', async () => {
  markSecureVerificationErrorReported(verificationError)
  const view = renderRequestPreview()

  fireEvent.click(view.getByRole('button', { name: 'Copy ready-to-run curl' }))

  await waitFor(() =>
    assert.equal(withApiTokenVerification.mock.calls.length, 1)
  )
  await waitForCopyToSettle(view)
  assert.equal(handleServerError.mock.calls.length, 0)
})
