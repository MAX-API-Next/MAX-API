import type { VerificationMethod, VerificationMethods } from './types'

export function selectVerificationMethod(
  methods: VerificationMethods,
  preferredMethod?: VerificationMethod
): VerificationMethod | null {
  const isAvailable = (method: VerificationMethod) => {
    if (method === '2fa') return methods.has2FA
    if (method === 'passkey') {
      return methods.hasPasskey && methods.passkeySupported
    }
    return methods.hasPassword
  }

  if (preferredMethod && isAvailable(preferredMethod)) return preferredMethod
  if (methods.hasPasskey && methods.passkeySupported) return 'passkey'
  if (methods.has2FA) return '2fa'
  if (methods.hasPassword) return 'password'
  return null
}
