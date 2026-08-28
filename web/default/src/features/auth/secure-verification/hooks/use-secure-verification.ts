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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  extractVerificationInfo,
  isVerificationRequiredError,
} from '@/lib/secure-verification'
import { checkVerificationMethods, verify } from '../api'
import { selectVerificationMethod } from '../method-selection'
import type {
  SecureVerificationState,
  StartVerificationOptions,
  UseSecureVerificationOptions,
  VerificationMethod,
  VerificationMethods,
  VerificationScope,
} from '../types'

type ApiCall = (() => Promise<unknown>) | null

interface PendingVerification {
  resolve: (value: unknown | null) => void
  reject: (reason?: unknown) => void
}

interface InternalState extends SecureVerificationState {
  apiCall: ApiCall
}

const defaultMethods: VerificationMethods = {
  has2FA: false,
  hasPasskey: false,
  hasPassword: false,
  passkeySupported: false,
}

const initialState: InternalState = {
  method: null,
  loading: false,
  code: '',
  title: undefined,
  description: undefined,
  apiCall: null,
}

export function useSecureVerification(
  options: UseSecureVerificationOptions = {}
) {
  const { onSuccess, onError, successMessage, autoReset = true } = options

  const [methods, setMethods] = useState<VerificationMethods>(defaultMethods)
  const [state, setState] = useState<InternalState>(initialState)
  const [open, setDialogOpen] = useState(false)
  const pendingVerificationRef = useRef<PendingVerification | null>(null)

  const fetchVerificationMethods = useCallback(
    async (scope?: VerificationScope) => {
      const result = await checkVerificationMethods(scope)
      setMethods(result)
      return result
    },
    []
  )

  const settlePendingVerification = useCallback(
    (value: unknown | null, error?: unknown): void => {
      const pending = pendingVerificationRef.current
      pendingVerificationRef.current = null
      if (!pending) return
      if (error !== undefined) {
        pending.reject(error)
      } else {
        pending.resolve(value)
      }
    },
    []
  )

  const reset = useCallback((): void => {
    settlePendingVerification(null)
    setState(initialState)
    setDialogOpen(false)
  }, [settlePendingVerification])

  useEffect(() => {
    return () => settlePendingVerification(null)
  }, [settlePendingVerification])

  const startVerification = useCallback(
    async (
      apiCall: () => Promise<unknown>,
      config: StartVerificationOptions = {}
    ) => {
      const { preferredMethod, title, description, scope } = config
      let availableMethods: VerificationMethods
      try {
        availableMethods = await fetchVerificationMethods(scope)
      } catch (error) {
        toast.error(i18next.t('Verification unavailable'))
        onError?.(error)
        throw error
      }

      if (
        !availableMethods.has2FA &&
        !availableMethods.hasPasskey &&
        !availableMethods.hasPassword
      ) {
        toast.error(
          i18next.t(
            'No verification method is available. Add a password or sign in again with a linked account.'
          )
        )
        onError?.(
          new Error(
            'No verification method is available. Add a password or sign in again with a linked account.'
          )
        )
        return false
      }

      const defaultMethod = selectVerificationMethod(
        availableMethods,
        preferredMethod
      )

      setState((prev) => ({
        ...prev,
        apiCall,
        method: defaultMethod,
        title,
        description,
        scope,
      }))
      setDialogOpen(true)
      return true
    },
    [fetchVerificationMethods, onError]
  )

  const executeVerification = useCallback(
    async (method?: VerificationMethod, code?: string) => {
      if (!state.apiCall) {
        toast.error(i18next.t('Verification is not configured properly'))
        return
      }

      const actualMethod = method ?? state.method
      if (!actualMethod) {
        toast.error(i18next.t('Select a verification method first'))
        return
      }

      setState((prev) => ({ ...prev, loading: true }))

      try {
        await verify(actualMethod, code ?? state.code, state.scope)
      } catch (error) {
        setState((prev) => ({ ...prev, loading: false }))
        const message =
          error instanceof Error
            ? error.message
            : i18next.t('Verification failed')
        toast.error(message)
        onError?.(error)
        throw error
      }

      try {
        const result = await state.apiCall()
        settlePendingVerification(result)

        if (successMessage) {
          toast.success(successMessage)
        }

        onSuccess?.(result, actualMethod)

        if (autoReset) {
          reset()
        }

        return result
      } catch (error) {
        settlePendingVerification(null, error)
        const message =
          error instanceof Error
            ? error.message
            : i18next.t('Verification failed')
        toast.error(message)
        onError?.(error)
        reset()
        throw error
      } finally {
        setState((prev) => ({ ...prev, loading: false }))
      }
    },
    [
      state,
      successMessage,
      onSuccess,
      onError,
      autoReset,
      reset,
      settlePendingVerification,
    ]
  )

  const setCode = useCallback((code: string) => {
    setState((prev) => ({ ...prev, code }))
  }, [])

  const switchMethod = useCallback((method: VerificationMethod) => {
    setState((prev) => ({ ...prev, method, code: '' }))
  }, [])

  const cancel = useCallback((): void => {
    reset()
  }, [reset])

  const setOpen = useCallback(
    (nextOpen: boolean): void => {
      if (!nextOpen) {
        cancel()
        return
      }
      setDialogOpen(true)
    },
    [cancel]
  )

  /**
   * Returns null when the user cancels. Callers must handle rejections from
   * method discovery, the original action, and the post-verification retry.
   */
  const withVerification = useCallback(
    async <T>(
      apiCall: () => Promise<T>,
      config: StartVerificationOptions = {}
    ): Promise<T | null> => {
      try {
        return await apiCall()
      } catch (error) {
        if (isVerificationRequiredError(error)) {
          const info = extractVerificationInfo(error)
          toast.info(info.message)
          const started = await startVerification(apiCall, config)
          if (!started) return null

          settlePendingVerification(null)
          return await new Promise<T | null>((resolve, reject) => {
            pendingVerificationRef.current = {
              resolve: (value) => resolve(value as T | null),
              reject,
            }
          })
        }
        throw error
      }
    },
    [settlePendingVerification, startVerification]
  )

  const canUseMethod = useCallback(
    (method: VerificationMethod) => {
      if (method === '2fa') return methods.has2FA
      if (method === 'passkey') {
        return methods.hasPasskey && methods.passkeySupported
      }
      return methods.hasPassword
    },
    [methods]
  )

  const recommendedMethod = useMemo<VerificationMethod | null>(() => {
    if (methods.hasPasskey && methods.passkeySupported) return 'passkey'
    if (methods.has2FA) return '2fa'
    if (methods.hasPassword) return 'password'
    return null
  }, [methods])

  return {
    open,
    setOpen,
    methods,
    state,
    startVerification,
    executeVerification,
    cancel,
    reset,
    setCode,
    switchMethod,
    withVerification,
    fetchVerificationMethods,
    canUseMethod,
    recommendedMethod,
    hasAnyMethod: methods.has2FA || methods.hasPasskey || methods.hasPassword,
    isLoading: state.loading,
    currentMethod: state.method,
    code: state.code,
  }
}
