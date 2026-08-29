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
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  type ReactElement,
  type ReactNode,
} from 'react'
import { useSecureVerification } from '../hooks/use-secure-verification'
import type { StartVerificationOptions, VerificationMethod } from '../types'
import { SecureVerificationDialog } from './secure-verification-dialog'

type SecureVerificationProviderProps = {
  children: ReactNode
}

interface SecureVerificationContextValue {
  withVerification: <T>(
    apiCall: () => Promise<T>,
    config?: StartVerificationOptions
  ) => Promise<T | null>
}

export const SecureVerificationContext =
  createContext<SecureVerificationContextValue | null>(null)

export function useSecureVerificationGate(): SecureVerificationContextValue {
  const context = useContext(SecureVerificationContext)
  if (!context) {
    throw new Error(
      'useSecureVerificationGate must be used within SecureVerificationProvider'
    )
  }
  return context
}

export function SecureVerificationProvider(
  props: SecureVerificationProviderProps
): ReactElement {
  const {
    open,
    setOpen,
    methods,
    state,
    executeVerification,
    cancel,
    setCode,
    switchMethod,
    withVerification,
  } = useSecureVerification()

  const handleVerification = useCallback(
    async (method: VerificationMethod, code?: string): Promise<void> => {
      try {
        await executeVerification(method, code)
      } catch {
        // The shared verification hook reports the error and keeps retryable
        // verification failures inside the dialog.
      }
    },
    [executeVerification]
  )
  const contextValue = useMemo(() => ({ withVerification }), [withVerification])

  return (
    <SecureVerificationContext.Provider value={contextValue}>
      {props.children}
      <SecureVerificationDialog
        open={open}
        onOpenChange={setOpen}
        methods={methods}
        state={state}
        onVerify={handleVerification}
        onCancel={cancel}
        onCodeChange={setCode}
        onMethodChange={switchMethod}
      />
    </SecureVerificationContext.Provider>
  )
}
