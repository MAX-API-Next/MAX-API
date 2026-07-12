import assert from 'node:assert/strict'
import { test } from 'bun:test'

import { selectVerificationMethod } from '../src/features/auth/secure-verification/method-selection'

test('falls back to 2FA when preferred Passkey is unavailable', () => {
  const method = selectVerificationMethod(
    {
      has2FA: true,
      hasPasskey: false,
      hasPassword: false,
      passkeySupported: false,
    },
    'passkey'
  )

  assert.equal(method, '2fa')
})

test('uses password only when it is the available fallback', () => {
  const method = selectVerificationMethod(
    {
      has2FA: false,
      hasPasskey: false,
      hasPassword: true,
      passkeySupported: false,
    },
    'passkey'
  )

  assert.equal(method, 'password')
})
