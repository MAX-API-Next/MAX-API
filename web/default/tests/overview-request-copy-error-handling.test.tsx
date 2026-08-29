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
import { api } from '@/lib/api'
import { markSecureVerificationErrorReported } from '@/lib/secure-verification'
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

interface VerificationMethodsRequestConfig {
  params?: {
    scope?: string
  }
  skipBusinessError?: boolean
  skipErrorHandler?: boolean
}

type VerificationMethodsResponse =
  | {
      data: {
        success: true
        data: {
          has_2fa: boolean
          has_passkey: boolean
          has_password: boolean
        }
      }
    }
  | {
      data: {
        success: false
        message: string
      }
    }

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
const navigate = mock(() => undefined)
const copyToClipboard = mock(async () => true)
const resetPasswordPost = mock(async () => ({
  data: {
    success: true,
    data: 'NewPassword123',
    api_tokens_revoked: true,
  },
}))
const consoleError = spyOn(console, 'error').mockImplementation(() => undefined)

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('@tanstack/react-router', () => ({
  Link: (props: { children?: ReactNode }) => <a>{props.children}</a>,
  Outlet: () => null,
  useNavigate: () => navigate,
  useRouterState: (options?: {
    select?: (state: { location: { pathname: string } }) => unknown
  }) =>
    options?.select?.({ location: { pathname: '/' } }) ?? {
      location: { pathname: '/' },
    },
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

mock.module('@/lib/copy-to-clipboard', () => ({ copyToClipboard }))

const { RequestPreview } = await import(
  '../src/features/dashboard/components/overview/overview-dashboard'
)
const { ResetPasswordConfirm } = await import(
  '../src/features/auth/reset-password-confirm'
)
const { checkVerificationMethods } = await import(
  '../src/features/auth/secure-verification/api'
)
const testEnv = createReactTestEnvironment()

beforeAll(() => testEnv.setup())
beforeEach(() => {
  verificationError = new Error('request failed')
  withApiTokenVerification.mockClear()
  useApiTokenVerification.mockClear()
  handleServerError.mockClear()
  navigate.mockClear()
  copyToClipboard.mockClear()
  resetPasswordPost.mockClear()
  consoleError.mockClear()
})
afterEach(() => cleanup())
afterAll(() => {
  consoleError.mockRestore()
  testEnv.teardown()
})

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

test('shows that password recovery revokes existing API tokens', async () => {
  const originalPost = api.post
  api.post = resetPasswordPost as typeof api.post
  try {
    const view = render(
      <ResetPasswordConfirm
        email='recovery@example.com'
        token='recovery-token'
      />
    )

    fireEvent.click(
      view.getByRole('button', {
        name: 'auth.resetPasswordConfirm.confirm',
      })
    )

    await waitFor(() => {
      assert.equal(resetPasswordPost.mock.calls.length, 1)
      assert.ok(
        (view.container.textContent ?? '').includes(
          'auth.resetPasswordConfirm.apiTokensRevoked'
        )
      )
    })
    assert.equal(
      view.getByDisplayValue('NewPassword123').hasAttribute('disabled'),
      true
    )
  } finally {
    api.post = originalPost
  }
})

test('propagates verification method request failures', async () => {
  const originalGet = api.get
  api.get = mock(async () => {
    throw new Error('verification methods unavailable')
  }) as typeof api.get
  try {
    await assert.rejects(
      checkVerificationMethods('credentials'),
      /verification methods unavailable/
    )
    assert.equal(consoleError.mock.calls.length, 1)
  } finally {
    api.get = originalGet
  }
})

test('rejects unsuccessful verification method responses', async () => {
  const originalGet = api.get
  api.get = mock(async () => ({
    data: {
      success: false,
      message: 'failed to load verification methods',
    },
  })) as typeof api.get
  try {
    await assert.rejects(
      checkVerificationMethods('credentials'),
      /failed to load verification methods/
    )
    assert.equal(consoleError.mock.calls.length, 1)
  } finally {
    api.get = originalGet
  }
})

test('preserves a successful response with no available methods', async () => {
  const originalGet = api.get
  const methodsGet = mock(
    async (
      _url: string,
      _config?: VerificationMethodsRequestConfig
    ): Promise<VerificationMethodsResponse> => ({
      data: {
        success: true,
        data: {
          has_2fa: false,
          has_passkey: false,
          has_password: false,
        },
      },
    })
  )
  api.get = methodsGet as typeof api.get
  try {
    assert.deepEqual(await checkVerificationMethods('credentials'), {
      has2FA: false,
      hasPasskey: false,
      hasPassword: false,
      passkeySupported: false,
    })
    assert.equal(methodsGet.mock.calls[0]?.[0], '/api/verify/methods')
    assert.deepEqual(methodsGet.mock.calls[0]?.[1]?.params, {
      scope: 'credentials',
    })
    assert.equal(methodsGet.mock.calls[0]?.[1]?.skipBusinessError, true)
    assert.equal(methodsGet.mock.calls[0]?.[1]?.skipErrorHandler, true)
    assert.equal(consoleError.mock.calls.length, 0)
  } finally {
    api.get = originalGet
  }
})
