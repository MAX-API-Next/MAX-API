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
import { useCallback, useState, type ReactElement } from 'react'
import { AlertTriangle, KeyRound, Loader2, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import { handleServerError } from '@/lib/handle-server-error'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusBadge } from '@/components/status-badge'
import { usePasskeyManagement } from '@/features/auth/passkey'
import {
  checkVerificationMethods,
  useSecureVerificationGate,
  type VerificationMethod,
  type VerificationMethods,
} from '@/features/auth/secure-verification'

interface PasskeyCardProps {
  loading: boolean
}

export function PasskeyCard(props: PasskeyCardProps): ReactElement {
  const { t } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)

  const {
    status,
    loading,
    registering,
    removing,
    supported,
    enabled,
    lastUsed,
    register,
    remove,
  } = usePasskeyManagement()

  const { withVerification } = useSecureVerificationGate()

  const handleRegister = useCallback(async (): Promise<void> => {
    if (!supported) {
      toast.info(t('This device does not support Passkey'))
      return
    }

    try {
      await withVerification(register, {
        scope: 'passkey_register',
        title: t('Security verification'),
        description: t('Confirm your identity before registering a Passkey.'),
      })
    } catch {
      // The action and verification hook own user-facing error messages. The
      // event handler still consumes the rejection to avoid an unhandled
      // promise when method discovery or the retried action fails.
    }
  }, [register, supported, t, withVerification])

  const handleRemove = useCallback(async (): Promise<void> => {
    let methods: VerificationMethods
    try {
      methods = await checkVerificationMethods('credentials')
    } catch (error) {
      handleServerError(error, {
        fallback: t('Verification unavailable'),
      })
      return
    }
    const required: VerificationMethod | null = methods.has2FA
      ? '2fa'
      : methods.hasPasskey
        ? 'passkey'
        : null

    if (!required) {
      toast.error(
        t(
          'Please enable Two-factor Authentication or Passkey before proceeding'
        )
      )
      return
    }

    if (required === 'passkey' && !methods.passkeySupported) {
      toast.info(t('This device does not support Passkey'))
      return
    }

    setConfirmOpen(false)
    try {
      await withVerification(remove, {
        scope: 'credentials',
        preferredMethod: required,
        allowedMethods: [required],
        forceVerification: true,
        title: t('Security verification'),
        description: t(
          'Confirm your identity before removing this Passkey from your account.'
        ),
      })
    } catch {
      // The shared verification provider owns user-facing error messages.
    }
  }, [remove, t, withVerification])

  if (props.loading || loading) {
    return (
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-3 sm:p-5'>
          <Skeleton className='h-6 w-48' />
          <Skeleton className='mt-2 h-4 w-64' />
        </CardHeader>
        <CardContent className='p-3 sm:p-5'>
          <Skeleton className='h-20 w-full' />
        </CardContent>
      </Card>
    )
  }

  const formattedLastUsed =
    lastUsed && !Number.isNaN(Date.parse(lastUsed))
      ? dayjs(lastUsed).fromNow()
      : t('Not used yet')

  const showUnsupportedNotice = !supported && !enabled

  return (
    <Card className='gap-0 overflow-hidden py-0'>
      <CardHeader className='p-3 sm:p-5'>
        <CardTitle className='text-lg tracking-tight sm:text-xl'>
          {t('Passkey Login')}
        </CardTitle>
        <CardDescription className='text-xs sm:text-sm'>
          {t('Use Passkey to sign in without entering your password.')}
        </CardDescription>
      </CardHeader>

      <CardContent className='p-3 sm:p-5'>
        <div className='space-y-6'>
          <div className='flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between xl:flex-col 2xl:flex-row'>
            <div className='flex items-start gap-4'>
              <div className='bg-muted rounded-md p-2'>
                <KeyRound className='h-5 w-5' />
              </div>
              <div className='space-y-1'>
                <div className='flex flex-wrap items-center gap-2'>
                  <p className='font-medium'>{t('Passkey Authentication')}</p>
                  <StatusBadge
                    label={enabled ? t('Enabled') : t('Disabled')}
                    variant={enabled ? 'success' : 'neutral'}
                    showDot
                    copyable={false}
                  />
                  {status?.backup_eligible !== undefined && (
                    <StatusBadge
                      label={
                        status.backup_eligible
                          ? status.backup_state
                            ? t('Backed up')
                            : t('Not backed up')
                          : t('No backup')
                      }
                      variant={
                        status.backup_eligible
                          ? status.backup_state
                            ? 'success'
                            : 'warning'
                          : 'neutral'
                      }
                      showDot
                      copyable={false}
                    />
                  )}
                </div>
                <p className='text-muted-foreground text-sm'>
                  {t('Last used:')} {formattedLastUsed}
                </p>
              </div>
            </div>

            {!enabled && (
              <Button
                className='w-full sm:w-auto xl:w-full 2xl:w-auto'
                onClick={handleRegister}
                disabled={!supported || registering}
              >
                {registering && (
                  <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                )}
                {t('Enable Passkey')}
              </Button>
            )}
          </div>

          {enabled && (
            <div className='flex flex-col gap-3 border-t pt-6 sm:flex-row xl:flex-col 2xl:flex-row'>
              <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                <AlertDialogTrigger
                  render={
                    <Button
                      variant='destructive'
                      className='flex-1'
                      disabled={removing}
                    />
                  }
                >
                  {removing ? (
                    <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  ) : (
                    <AlertTriangle className='mr-2 h-4 w-4' />
                  )}
                  {t('Remove Passkey')}
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>{t('Remove Passkey?')}</AlertDialogTitle>
                    <AlertDialogDescription>
                      {t(
                        'Removing Passkey will require you to sign in with your password next time. You can re-register anytime.'
                      )}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel disabled={removing}>
                      {t('Cancel')}
                    </AlertDialogCancel>
                    <AlertDialogAction
                      className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
                      disabled={removing}
                      onClick={(event) => {
                        event.preventDefault()
                        handleRemove()
                      }}
                    >
                      {t('Remove')}
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          )}

          {showUnsupportedNotice && (
            <div className='bg-muted/60 text-muted-foreground flex items-start gap-3 rounded-md p-4 text-sm'>
              <ShieldAlert className='mt-0.5 h-4 w-4 flex-shrink-0 text-amber-500' />
              <div>
                <p className='text-foreground font-medium'>
                  {t('Passkey not supported on this device')}
                </p>
                <p>
                  {t(
                    'Use a compatible browser or device with biometric authentication or a security key to register a Passkey.'
                  )}
                </p>
              </div>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
