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
import { useCallback } from 'react'
import { useSecureVerification } from '../hooks/use-secure-verification'
import type { VerificationMethod } from '../types'
import { SecureVerificationContext } from './secure-verification-context'
import { SecureVerificationDialog } from './secure-verification-dialog'

export function SecureVerificationProvider({
  children,
}: {
  children: React.ReactNode
}) {
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
    async (method: VerificationMethod, code?: string) => {
      try {
        await executeVerification(method, code)
      } catch {
        // The shared verification hook reports the error and keeps retryable
        // verification failures inside the dialog.
      }
    },
    [executeVerification]
  )

  return (
    <SecureVerificationContext.Provider value={{ withVerification }}>
      {children}
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
