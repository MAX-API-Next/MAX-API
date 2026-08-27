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
import { createContext, useContext } from 'react'
import type { StartVerificationOptions } from '../types'

interface SecureVerificationContextValue {
  withVerification: <T>(
    apiCall: () => Promise<T>,
    config?: StartVerificationOptions
  ) => Promise<T | null>
}

export const SecureVerificationContext =
  createContext<SecureVerificationContextValue | null>(null)

export function useSecureVerificationGate() {
  const context = useContext(SecureVerificationContext)
  if (!context) {
    throw new Error(
      'useSecureVerificationGate must be used within SecureVerificationProvider'
    )
  }
  return context
}
