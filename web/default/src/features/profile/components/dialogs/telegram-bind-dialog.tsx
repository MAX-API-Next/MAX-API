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
import { RefreshCw, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
import { useTelegramLoginWidget } from '../../hooks/use-telegram-login-widget'

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
  const { widgetContainerRef, status, errorMessage, retry, handleOpenChange } =
    useTelegramLoginWidget({ open, onOpenChange, botName, onSuccess })

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
            <Send aria-hidden='true' />
            <AlertDescription>
              {t(
                'Telegram will ask you to confirm the account before it is bound.'
              )}
            </AlertDescription>
          </Alert>

          <div className='flex min-h-56 flex-col items-center justify-center gap-4 rounded-lg border p-6'>
            <div className='bg-muted flex size-12 items-center justify-center rounded-xl'>
              <Send
                className='text-muted-foreground size-6'
                aria-hidden='true'
              />
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
