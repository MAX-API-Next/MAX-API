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
import { handleServerError } from '@/lib/handle-server-error'
import {
  extractVerificationInfo,
  isVerificationRequiredError,
  markSecureVerificationErrorReported,
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
  attemptId: number
  resolve: (value: unknown | null) => void
  reject: (reason?: unknown) => void
}

interface InternalState extends SecureVerificationState {
  apiCall: ApiCall
  attemptId: number | null
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
  attemptId: null,
}

export function useSecureVerification(
  options: UseSecureVerificationOptions = {}
) {
  const { onSuccess, onError, successMessage, autoReset = true } = options

  const [methods, setMethods] = useState<VerificationMethods>(defaultMethods)
  const [state, setState] = useState<InternalState>(initialState)
  const [open, setDialogOpen] = useState(false)
  const pendingVerificationRef = useRef<PendingVerification | null>(null)
  const verificationAttemptRef = useRef(0)
  const mountedRef = useRef(true)

  const loadVerificationMethods = useCallback(
    async (scope?: VerificationScope): Promise<VerificationMethods> =>
      checkVerificationMethods(scope),
    []
  )

  const fetchVerificationMethods = useCallback(
    async (scope?: VerificationScope): Promise<VerificationMethods> => {
      const result = await loadVerificationMethods(scope)
      if (mountedRef.current) {
        setMethods(result)
      }
      return result
    },
    [loadVerificationMethods]
  )

  const settlePendingVerification = useCallback(
    (value: unknown | null, error?: unknown, attemptId?: number): void => {
      const pending = pendingVerificationRef.current
      if (attemptId !== undefined && pending?.attemptId !== attemptId) return
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

  const isVerificationAttemptActive = useCallback(
    (attemptId: number): boolean =>
      mountedRef.current && verificationAttemptRef.current === attemptId,
    []
  )

  const beginVerificationAttempt = useCallback((): number | null => {
    if (!mountedRef.current) return null
    verificationAttemptRef.current += 1
    settlePendingVerification(null)
    setState(initialState)
    setDialogOpen(false)
    return verificationAttemptRef.current
  }, [settlePendingVerification])

  const reset = useCallback((): void => {
    verificationAttemptRef.current += 1
    settlePendingVerification(null)
    if (!mountedRef.current) return
    setState(initialState)
    setDialogOpen(false)
  }, [settlePendingVerification])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      verificationAttemptRef.current += 1
      settlePendingVerification(null)
    }
  }, [settlePendingVerification])

  const startVerificationAttempt = useCallback(
    async (
      apiCall: () => Promise<unknown>,
      config: StartVerificationOptions,
      attemptId: number
    ): Promise<boolean> => {
      const { preferredMethod, title, description, scope } = config
      let availableMethods: VerificationMethods
      try {
        availableMethods = await loadVerificationMethods(scope)
      } catch (error) {
        if (!isVerificationAttemptActive(attemptId)) return false
        handleServerError(error, {
          fallback: i18next.t('Verification unavailable'),
        })
        markSecureVerificationErrorReported(error)
        onError?.(error)
        throw error
      }

      if (!isVerificationAttemptActive(attemptId)) return false
      setMethods(availableMethods)

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
        attemptId,
        method: defaultMethod,
        title,
        description,
        scope,
      }))
      setDialogOpen(true)
      return true
    },
    [isVerificationAttemptActive, loadVerificationMethods, onError]
  )

  const startVerification = useCallback(
    async (
      apiCall: () => Promise<unknown>,
      config: StartVerificationOptions = {}
    ): Promise<boolean> => {
      const attemptId = beginVerificationAttempt()
      if (attemptId === null) return false
      return startVerificationAttempt(apiCall, config, attemptId)
    },
    [beginVerificationAttempt, startVerificationAttempt]
  )

  const executeVerification = useCallback(
    async (
      method?: VerificationMethod,
      code?: string
    ): Promise<unknown | undefined> => {
      if (!state.apiCall || state.attemptId === null) {
        toast.error(i18next.t('Verification is not configured properly'))
        return
      }

      const attemptId = state.attemptId
      const apiCall = state.apiCall
      const actualMethod = method ?? state.method
      if (!actualMethod) {
        toast.error(i18next.t('Select a verification method first'))
        return
      }
      if (!isVerificationAttemptActive(attemptId)) return

      setState((prev) => ({ ...prev, loading: true }))

      try {
        await verify(actualMethod, code ?? state.code, state.scope)
      } catch (error) {
        if (!isVerificationAttemptActive(attemptId)) return
        setState((prev) => ({ ...prev, loading: false }))
        const message =
          error instanceof Error
            ? error.message
            : i18next.t('Verification failed')
        toast.error(message)
        markSecureVerificationErrorReported(error)
        onError?.(error)
        throw error
      }

      if (!isVerificationAttemptActive(attemptId)) return

      try {
        const result = await apiCall()
        if (!isVerificationAttemptActive(attemptId)) return
        settlePendingVerification(result, undefined, attemptId)

        if (successMessage) {
          toast.success(successMessage)
        }

        onSuccess?.(result, actualMethod)

        if (autoReset) {
          reset()
        }

        return result
      } catch (error) {
        if (!isVerificationAttemptActive(attemptId)) return
        settlePendingVerification(null, error, attemptId)
        const message =
          error instanceof Error
            ? error.message
            : i18next.t('Verification failed')
        handleServerError(error, { fallback: message })
        markSecureVerificationErrorReported(error)
        onError?.(error)
        reset()
        throw error
      } finally {
        if (isVerificationAttemptActive(attemptId)) {
          setState((prev) =>
            prev.attemptId === attemptId ? { ...prev, loading: false } : prev
          )
        }
      }
    },
    [
      state,
      isVerificationAttemptActive,
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
      const attemptId = beginVerificationAttempt()
      if (attemptId === null) return null
      const continuation = new Promise<T | null>((resolve, reject) => {
        pendingVerificationRef.current = {
          attemptId,
          resolve: (value) => resolve(value as T | null),
          reject,
        }
      })

      void (async (): Promise<void> => {
        try {
          const result = await apiCall()
          if (!isVerificationAttemptActive(attemptId)) return
          settlePendingVerification(result, undefined, attemptId)
          return
        } catch (error) {
          if (!isVerificationAttemptActive(attemptId)) return
          if (!isVerificationRequiredError(error)) {
            settlePendingVerification(null, error, attemptId)
            return
          }

          const info = extractVerificationInfo(error)
          toast.info(info.message)
        }

        try {
          const started = await startVerificationAttempt(
            apiCall,
            config,
            attemptId
          )
          if (!started) {
            settlePendingVerification(null, undefined, attemptId)
          }
        } catch (error) {
          settlePendingVerification(null, error, attemptId)
        }
      })()

      return await continuation
    },
    [
      beginVerificationAttempt,
      isVerificationAttemptActive,
      settlePendingVerification,
      startVerificationAttempt,
    ]
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
