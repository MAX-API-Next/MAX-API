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
import { useCallback, useEffect, useRef, useState } from 'react'
import { RefreshCw, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { useSecureVerificationGate } from '@/features/auth/secure-verification'
import {
  bindTelegramAccount,
  createTelegramBindState,
  type TelegramAuthorizationPayload,
} from '../../api'

// ============================================================================
// Telegram Bind Dialog Component
// ============================================================================

interface TelegramBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  botName: string
  onSuccess: () => void
}

export function TelegramBindDialog({
  open,
  onOpenChange,
  botName,
  onSuccess,
}: TelegramBindDialogProps) {
  const { t } = useTranslation()
  const { withVerification } = useSecureVerificationGate()
  const widgetContainerRef = useRef<HTMLDivElement>(null)
  const [callbackName] = useState(
    () => `telegramAuthCallback_${Math.random().toString(36).slice(2)}`
  )
  const [bindState, setBindState] = useState('')
  const [status, setStatus] = useState<
    'idle' | 'loading' | 'ready' | 'binding' | 'error'
  >('idle')
  const [errorMessage, setErrorMessage] = useState('')
  const [retryVersion, setRetryVersion] = useState(0)

  const initializeBinding = useCallback(async () => {
    setStatus('loading')
    setErrorMessage('')
    try {
      const response = await withVerification(createTelegramBindState, {
        scope: 'credentials',
        title: t('Security verification'),
        description: t(
          'Confirm your identity before binding a Telegram account.'
        ),
      })
      if (!response) {
        setStatus('idle')
        return
      }
      if (!response.success || !response.data?.state) {
        throw new Error(
          response.message || t('Failed to initialize Telegram binding')
        )
      }
      setBindState(response.data.state)
    } catch (error) {
      setErrorMessage(
        error instanceof Error
          ? error.message
          : t('Failed to initialize Telegram binding')
      )
      setStatus('error')
    }
  }, [t, withVerification])

  const resetBindingState = useCallback(() => {
    setBindState('')
    setStatus('idle')
    setErrorMessage('')
  }, [])

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      if (!nextOpen) resetBindingState()
      onOpenChange(nextOpen)
    },
    [onOpenChange, resetBindingState]
  )

  useEffect(() => {
    if (!open) return
    const timer = window.setTimeout(() => void initializeBinding(), 0)
    return () => window.clearTimeout(timer)
  }, [initializeBinding, open, retryVersion])

  useEffect(() => {
    if (!open || !bindState || !widgetContainerRef.current) return

    const container = widgetContainerRef.current
    const windowCallbacks = window as unknown as Record<string, unknown>
    container.replaceChildren()

    windowCallbacks[callbackName] = async (
      authorization: TelegramAuthorizationPayload
    ) => {
      setStatus('binding')
      setErrorMessage('')
      try {
        const response = await withVerification(
          () => bindTelegramAccount({ ...authorization, state: bindState }),
          {
            scope: 'credentials',
            title: t('Security verification'),
            description: t(
              'Confirm your identity before binding a Telegram account.'
            ),
          }
        )
        if (!response) {
          setStatus('ready')
          return
        }
        if (!response.success) {
          throw new Error(
            response.message || t('Failed to bind Telegram account')
          )
        }
        toast.success(t('Telegram account bound successfully'))
        onSuccess()
        handleOpenChange(false)
      } catch (error) {
        setErrorMessage(
          error instanceof Error
            ? error.message
            : t('Failed to bind Telegram account')
        )
        setStatus('error')
      }
    }

    const script = document.createElement('script')
    script.async = true
    script.src = 'https://telegram.org/js/telegram-widget.js?22'
    script.setAttribute('data-telegram-login', botName.replace(/^@/, ''))
    script.setAttribute('data-size', 'large')
    script.setAttribute('data-userpic', 'false')
    script.setAttribute('data-onauth', `${callbackName}(user)`)
    script.onload = () => setStatus('ready')
    script.onerror = () => {
      setErrorMessage(t('Failed to load Telegram Login Widget'))
      setStatus('error')
    }
    container.appendChild(script)

    return () => {
      delete windowCallbacks[callbackName]
      container.replaceChildren()
    }
  }, [
    bindState,
    botName,
    callbackName,
    handleOpenChange,
    onSuccess,
    open,
    t,
    withVerification,
  ])

  const retry = () => {
    setBindState('')
    setRetryVersion((version) => version + 1)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Bind Telegram Account')}</DialogTitle>
          <DialogDescription>
            {t('Click the button below to bind your Telegram account')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex flex-col gap-4 py-4'>
          <Alert>
            <Send />
            <AlertDescription>
              {t(
                'Telegram will ask you to confirm the account before it is bound.'
              )}
            </AlertDescription>
          </Alert>

          <div className='flex min-h-56 flex-col items-center justify-center gap-4 rounded-lg border p-6'>
            <div className='bg-muted flex size-12 items-center justify-center rounded-xl'>
              <Send className='text-muted-foreground size-6' />
            </div>

            <div className='text-center'>
              <p className='text-muted-foreground text-sm'>
                {t('Bot:')}{' '}
                <span className='font-mono font-semibold'>@{botName}</span>
              </p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t(
                  "After clicking the button, you'll be asked to authorize the bot"
                )}
              </p>
            </div>

            {(status === 'loading' || status === 'binding') && (
              <div className='text-muted-foreground flex items-center gap-2 text-sm'>
                <Spinner />
                {status === 'binding'
                  ? t('Binding Telegram account...')
                  : t('Preparing Telegram authorization...')}
              </div>
            )}

            {status === 'error' && (
              <div className='flex max-w-sm flex-col items-center gap-3 text-center'>
                <p className='text-destructive text-sm'>{errorMessage}</p>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={retry}
                >
                  <RefreshCw data-icon='inline-start' />
                  {t('Retry')}
                </Button>
              </div>
            )}

            <div
              ref={widgetContainerRef}
              className={cn(
                'min-h-10 justify-center',
                status === 'ready' ? 'flex' : 'hidden'
              )}
            />

            {status === 'idle' && (
              <Button type='button' variant='outline' onClick={retry}>
                {t('Start Telegram authorization')}
              </Button>
            )}
          </div>

          <p className='text-muted-foreground text-center text-xs'>
            {t('The binding will complete automatically after authorization')}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
