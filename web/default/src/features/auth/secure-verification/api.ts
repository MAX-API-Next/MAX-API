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
import i18next from 'i18next'
import { api } from '@/lib/api'
import {
  buildAssertionResult,
  prepareCredentialRequestOptions,
  isPasskeySupported as detectPasskeySupport,
} from '@/lib/passkey'
import { beginPasskeyVerification, finishPasskeyVerification } from '../passkey'
import type {
  VerificationMethod,
  VerificationMethods,
  VerificationScope,
} from './types'

interface VerificationMethodsApiResponse {
  success: boolean
  message?: string
  data?: {
    has_2fa: boolean
    has_passkey: boolean
    has_password: boolean
  }
}

/**
 * Fetch available verification methods for the current user.
 */
export async function checkVerificationMethods(
  scope?: VerificationScope
): Promise<VerificationMethods> {
  try {
    const [methodsResponse, passkeySupported] = await Promise.all([
      api.get<VerificationMethodsApiResponse>('/api/verify/methods', {
        params: scope ? { scope } : undefined,
        skipBusinessError: true,
        skipErrorHandler: true,
      }),
      detectPasskeySupport(),
    ])
    const methods = methodsResponse.data?.data
    if (!methodsResponse.data?.success || !methods) {
      throw new Error(
        methodsResponse.data?.message || i18next.t('Verification unavailable')
      )
    }

    return {
      has2FA: Boolean(methods.has_2fa),
      hasPasskey: Boolean(methods.has_passkey),
      hasPassword: Boolean(methods.has_password),
      passkeySupported,
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('[Secure Verification] Failed to check methods', error)
    throw error
  }
}

/**
 * Execute a verification flow based on the method type.
 */
export async function verify(
  method: VerificationMethod,
  code?: string,
  scope?: VerificationScope
): Promise<void> {
  switch (method) {
    case '2fa':
      return verifyTwoFA(code, scope)
    case 'passkey':
      return verifyPasskey(scope)
    case 'password':
      return verifyPassword(code, scope)
    default:
      throw new Error(`Unsupported verification method: ${method}`)
  }
}

async function verifyPassword(
  password?: string | null,
  scope?: VerificationScope
): Promise<void> {
  if (!password) {
    throw new Error(i18next.t('Please enter your password'))
  }

  const res = await api.post('/api/verify', {
    method: 'password',
    password,
    scope,
  })

  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Verification failed')
  }
}

/**
 * Perform 2FA verification flow.
 */
async function verifyTwoFA(
  code?: string | null,
  scope?: VerificationScope
): Promise<void> {
  const trimmed = code?.trim()
  if (!trimmed) {
    throw new Error('Please enter the verification code or backup code')
  }

  const res = await api.post('/api/verify', {
    method: '2fa',
    code: trimmed,
    scope,
  })

  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Verification failed')
  }
}

/**
 * Perform Passkey verification flow.
 */
async function verifyPasskey(scope?: VerificationScope): Promise<void> {
  if (typeof navigator === 'undefined' || !navigator.credentials) {
    throw new Error('Passkey verification is not supported in this environment')
  }

  try {
    const beginResponse = await beginPasskeyVerification()
    if (!beginResponse.success) {
      throw new Error(beginResponse.message || 'Failed to start verification')
    }

    const publicKey = prepareCredentialRequestOptions(
      beginResponse.data?.options ?? beginResponse.data
    )

    const credential = (await navigator.credentials.get({
      publicKey,
    })) as PublicKeyCredential | null

    if (!credential) {
      throw new Error('Passkey verification was cancelled')
    }

    const assertion = buildAssertionResult(credential)
    if (!assertion) {
      throw new Error('Unable to build Passkey assertion')
    }

    const finishResponse = await finishPasskeyVerification(assertion)
    if (!finishResponse.success) {
      throw new Error(finishResponse.message || 'Passkey verification failed')
    }

    const verifyResponse = await api.post('/api/verify', {
      method: 'passkey',
      scope,
    })

    if (!verifyResponse.data?.success) {
      throw new Error(
        verifyResponse.data?.message || 'Failed to complete verification'
      )
    }
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'NotAllowedError') {
      throw new Error('Passkey verification was cancelled or timed out', {
        cause: error,
      })
    }
    if (error instanceof DOMException && error.name === 'InvalidStateError') {
      throw new Error(
        'Passkey verification is not available in the current state',
        { cause: error }
      )
    }
    throw error
  }
}
