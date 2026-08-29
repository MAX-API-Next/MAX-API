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
import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { wasSecureVerificationErrorReported } from '@/lib/secure-verification'
import { useSecureVerificationGate } from '@/features/auth/secure-verification'
import {
  bindTelegramAccount,
  createTelegramBindState,
  type TelegramAuthorizationPayload,
} from '../api'

type TelegramWidgetStatus = 'idle' | 'loading' | 'ready' | 'binding' | 'error'

interface UseTelegramLoginWidgetOptions {
  open: boolean
  onOpenChange: (open: boolean) => void
  botName: string
  onSuccess: () => void
}

interface UseTelegramLoginWidgetResult {
  widgetContainerRef: RefObject<HTMLDivElement | null>
  status: TelegramWidgetStatus
  errorMessage: string
  retry: () => void
  handleOpenChange: (open: boolean) => void
}

export function useTelegramLoginWidget(
  options: UseTelegramLoginWidgetOptions
): UseTelegramLoginWidgetResult {
  const { t } = useTranslation()
  const { withVerification } = useSecureVerificationGate()
  const { onOpenChange } = options
  const widgetContainerRef = useRef<HTMLDivElement>(null)
  const [callbackName] = useState(
    () => `telegramAuthCallback_${Math.random().toString(36).slice(2)}`
  )
  const [bindState, setBindState] = useState('')
  const [status, setStatus] = useState<TelegramWidgetStatus>('idle')
  const [errorMessage, setErrorMessage] = useState('')
  const [retryVersion, setRetryVersion] = useState(0)
  const bindingAttemptRef = useRef(0)

  const invalidateBindingAttempt = useCallback((): void => {
    bindingAttemptRef.current += 1
  }, [])

  const resetBindingState = useCallback((): void => {
    invalidateBindingAttempt()
    setBindState('')
    setStatus('idle')
    setErrorMessage('')
  }, [invalidateBindingAttempt])

  const handleOpenChange = useCallback(
    (nextOpen: boolean): void => {
      if (!nextOpen) resetBindingState()
      onOpenChange(nextOpen)
    },
    [onOpenChange, resetBindingState]
  )

  const callbackHandlersRef = useRef({
    handleOpenChange,
    onSuccess: options.onSuccess,
    t,
    withVerification,
  })

  useEffect(() => {
    callbackHandlersRef.current = {
      handleOpenChange,
      onSuccess: options.onSuccess,
      t,
      withVerification,
    }
  }, [handleOpenChange, options.onSuccess, t, withVerification])

  const initializeBinding = useCallback(async (): Promise<void> => {
    const handlers = callbackHandlersRef.current
    const attemptId = bindingAttemptRef.current + 1
    bindingAttemptRef.current = attemptId
    setBindState('')
    setStatus('loading')
    setErrorMessage('')
    try {
      const response = await handlers.withVerification(
        createTelegramBindState,
        {
          scope: 'credentials',
          title: handlers.t('Security verification'),
          description: handlers.t(
            'Confirm your identity before binding a Telegram account.'
          ),
        }
      )
      if (bindingAttemptRef.current !== attemptId) return
      if (!response) {
        setStatus('idle')
        return
      }
      if (!response.success || !response.data?.state) {
        throw new Error(
          response.message ||
            handlers.t('Failed to initialize Telegram binding')
        )
      }
      setBindState(response.data.state)
    } catch (error) {
      if (bindingAttemptRef.current !== attemptId) return
      const fallback = handlers.t('Failed to initialize Telegram binding')
      if (!wasSecureVerificationErrorReported(error)) {
        handleServerError(error, { fallback })
      }
      setErrorMessage(fallback)
      setStatus('error')
    }
  }, [])

  useEffect(() => {
    if (!options.open) {
      invalidateBindingAttempt()
      return
    }
    const timer = window.setTimeout(() => void initializeBinding(), 0)
    return () => {
      window.clearTimeout(timer)
      invalidateBindingAttempt()
    }
  }, [initializeBinding, invalidateBindingAttempt, options.open, retryVersion])

  useEffect(() => {
    if (!options.open || !bindState || !widgetContainerRef.current) return

    const container = widgetContainerRef.current
    const windowCallbacks = window as unknown as Record<string, unknown>
    const attemptId = bindingAttemptRef.current
    let active = true
    container.replaceChildren()

    windowCallbacks[callbackName] = async (
      authorization: TelegramAuthorizationPayload
    ): Promise<void> => {
      const handlers = callbackHandlersRef.current
      if (!active || bindingAttemptRef.current !== attemptId) return
      setStatus('binding')
      setErrorMessage('')
      try {
        const response = await handlers.withVerification(
          () => bindTelegramAccount({ ...authorization, state: bindState }),
          {
            scope: 'credentials',
            title: handlers.t('Security verification'),
            description: handlers.t(
              'Confirm your identity before binding a Telegram account.'
            ),
          }
        )
        if (!active || bindingAttemptRef.current !== attemptId) return
        if (!response) {
          setStatus('ready')
          return
        }
        if (!response.success) {
          throw new Error(
            response.message || handlers.t('Failed to bind Telegram account')
          )
        }
        toast.success(handlers.t('Telegram account bound successfully'))
        callbackHandlersRef.current.onSuccess()
        callbackHandlersRef.current.handleOpenChange(false)
      } catch (error) {
        if (!active || bindingAttemptRef.current !== attemptId) return
        const fallback = handlers.t('Failed to bind Telegram account')
        if (!wasSecureVerificationErrorReported(error)) {
          handleServerError(error, { fallback })
        }
        setErrorMessage(fallback)
        setStatus('error')
      }
    }

    const script = document.createElement('script')
    script.async = true
    script.src = 'https://telegram.org/js/telegram-widget.js?22'
    script.setAttribute(
      'data-telegram-login',
      options.botName.replace(/^@/, '')
    )
    script.setAttribute('data-size', 'large')
    script.setAttribute('data-userpic', 'false')
    script.setAttribute('data-onauth', `${callbackName}(user)`)
    script.onload = () => {
      if (active && bindingAttemptRef.current === attemptId) setStatus('ready')
    }
    script.onerror = () => {
      if (!active || bindingAttemptRef.current !== attemptId) return
      setErrorMessage(
        callbackHandlersRef.current.t('Failed to load Telegram Login Widget')
      )
      setStatus('error')
    }
    container.appendChild(script)

    return () => {
      active = false
      delete windowCallbacks[callbackName]
      container.replaceChildren()
    }
  }, [bindState, callbackName, options.botName, options.open])

  const retry = useCallback((): void => {
    invalidateBindingAttempt()
    setBindState('')
    setStatus('idle')
    setErrorMessage('')
    setRetryVersion((version) => version + 1)
  }, [invalidateBindingAttempt])

  return {
    widgetContainerRef,
    status,
    errorMessage,
    retry,
    handleOpenChange,
  }
}
