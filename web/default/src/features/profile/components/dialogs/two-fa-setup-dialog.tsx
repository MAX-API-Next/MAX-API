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
import { useState, useEffect, useCallback, useRef, type ReactNode } from 'react'
import { Loader2 } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { setup2FA, enable2FA } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { wasSecureVerificationErrorReported } from '@/lib/secure-verification'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { CopyButton } from '@/components/copy-button'
import { useSecureVerificationGate } from '@/features/auth/secure-verification'
import type { TwoFASetupData } from '../../types'

// ============================================================================
// Two-FA Setup Dialog Component
// ============================================================================

interface TwoFASetupDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

interface TwoFASetupStepsProps {
  setupData: TwoFASetupData
  step: number
  code: string
  loading: boolean
  onCodeChange: (code: string) => void
}

function TwoFASetupSteps(props: TwoFASetupStepsProps): ReactNode {
  const { t } = useTranslation()

  if (props.step === 0) {
    return (
      <div className='space-y-4'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Scan this QR code with your authenticator app (Google Authenticator, Microsoft Authenticator, etc.)'
          )}
        </p>
        <div className='flex justify-center rounded-lg bg-white p-4'>
          <QRCodeSVG value={props.setupData.qr_code_data} size={200} />
        </div>
        <div className='bg-muted rounded-lg p-3'>
          <div className='flex items-center justify-between'>
            <div>
              <p className='text-muted-foreground text-xs'>
                {t('Or enter this key manually:')}
              </p>
              <code className='font-mono text-sm'>
                {props.setupData.secret}
              </code>
            </div>
            <CopyButton
              value={props.setupData.secret}
              variant='ghost'
              tooltip={t('Copy secret key')}
              aria-label={t('Copy secret key')}
            />
          </div>
        </div>
      </div>
    )
  }

  if (props.step === 1) {
    return (
      <div className='space-y-4'>
        <Alert>
          <AlertDescription>
            {t(
              'Save these backup codes in a safe place. Each code can only be used once.'
            )}
          </AlertDescription>
        </Alert>
        <div className='rounded-lg border p-4'>
          <div className='grid grid-cols-2 gap-2'>
            {props.setupData.backup_codes.map((backupCode, index) => (
              <div
                key={index}
                className='bg-muted rounded-md p-2 text-center font-mono text-sm'
              >
                {backupCode}
              </div>
            ))}
          </div>
        </div>
        <CopyButton
          value={props.setupData.backup_codes.join('\n')}
          variant='outline'
          size='default'
          className='w-full'
          iconClassName='mr-2 size-4'
          tooltip={t('Copy all backup codes')}
          aria-label={t('Copy all backup codes')}
        >
          {t('Copy All Codes')}
        </CopyButton>
      </div>
    )
  }

  if (props.step === 2) {
    return (
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label htmlFor='code'>{t('Verification Code')}</Label>
          <Input
            id='code'
            value={props.code}
            onChange={(event) => props.onCodeChange(event.target.value)}
            placeholder={t('Enter 6-digit code')}
            maxLength={6}
            disabled={props.loading}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Enter the 6-digit code from your authenticator app')}
          </p>
        </div>
      </div>
    )
  }

  return null
}

export function TwoFASetupDialog({
  open,
  onOpenChange,
  onSuccess,
}: TwoFASetupDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [initializing, setInitializing] = useState(false)
  const [setupAttempted, setSetupAttempted] = useState(false)
  const [step, setStep] = useState(0)
  const [setupData, setSetupData] = useState<TwoFASetupData | null>(null)
  const [code, setCode] = useState('')
  const setupInFlightRef = useRef(false)
  const { withVerification } = useSecureVerificationGate()
  const stepLabels = [
    t('Scan QR Code'),
    t('Save Backup Codes'),
    t('Verify Setup'),
  ]

  const handleSetup = useCallback(async (): Promise<void> => {
    if (setupInFlightRef.current) return
    setupInFlightRef.current = true
    setSetupAttempted(false)
    setInitializing(true)
    let shouldMarkSetupAttempted = true
    try {
      const response = await withVerification(setup2FA, {
        scope: 'credentials',
        title: t('Security verification'),
        description: t(
          'Confirm your identity before setting up Two-factor Authentication.'
        ),
      })
      if (!response) {
        shouldMarkSetupAttempted = false
        setSetupAttempted(false)
        onOpenChange(false)
        return
      }
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to setup 2FA'))
      }
      setSetupData(response.data)
      setStep(0)
    } catch (error) {
      if (!wasSecureVerificationErrorReported(error)) {
        handleServerError(error, { fallback: t('Failed to setup 2FA') })
      }
      shouldMarkSetupAttempted = false
      setSetupAttempted(false)
      onOpenChange(false)
    } finally {
      setupInFlightRef.current = false
      setInitializing(false)
      setSetupAttempted(shouldMarkSetupAttempted)
    }
  }, [onOpenChange, t, withVerification])

  const handleEnable = async () => {
    if (!code) {
      toast.error(t('Please enter the verification code'))
      return
    }

    try {
      setLoading(true)
      const response = await withVerification(() => enable2FA(code), {
        scope: 'credentials',
        title: t('Security verification'),
        description: t(
          'Confirm your identity before enabling Two-factor Authentication.'
        ),
      })
      if (!response) return

      if (response.success) {
        toast.success(t('Two-factor authentication enabled successfully!'))
        onOpenChange(false)
        onSuccess()
        // Reset
        setStep(0)
        setCode('')
        setSetupData(null)
        setSetupAttempted(false)
      } else {
        toast.error(response.message || t('Failed to enable 2FA'))
      }
    } catch (error) {
      if (!wasSecureVerificationErrorReported(error)) {
        handleServerError(error, { fallback: t('Failed to enable 2FA') })
      }
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean): void => {
    if (!loading && !initializing) {
      if (!open) {
        setStep(0)
        setCode('')
        setSetupData(null)
        setSetupAttempted(false)
      }
      onOpenChange(open)
    }
  }

  // Initialize when dialog opens
  useEffect(() => {
    if (!open || setupData) return
    const timer = window.setTimeout(() => void handleSetup(), 0)
    return () => window.clearTimeout(timer)
  }, [open, setupData, handleSetup])

  const renderSetupState = (): ReactNode => {
    if (initializing || !setupAttempted) {
      return (
        <div className='flex flex-col items-center justify-center gap-3 py-8'>
          <div className='border-primary h-8 w-8 animate-spin rounded-full border-4 border-t-transparent' />
          <div className='text-muted-foreground text-sm'>
            {t('Setting up 2FA...')}
          </div>
        </div>
      )
    }
    if (!setupData) {
      return (
        <div className='flex justify-center py-8'>
          <div className='text-muted-foreground'>
            {t('Failed to load setup data')}
          </div>
        </div>
      )
    }
    return null
  }

  const setupState = renderSetupState()

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('Setup Two-Factor Authentication')}</DialogTitle>
            <DialogDescription>
              {t('Step')} {step + 1} {t('of 3:')} {stepLabels[step]}
            </DialogDescription>
          </DialogHeader>

          <div className='space-y-4 py-4'>
            {setupState ??
              (setupData && (
                <TwoFASetupSteps
                  setupData={setupData}
                  step={step}
                  code={code}
                  loading={loading}
                  onCodeChange={setCode}
                />
              ))}
          </div>

          <DialogFooter>
            {step > 0 && (
              <Button
                variant='outline'
                onClick={() => setStep(step - 1)}
                disabled={initializing || loading}
              >
                {t('Back')}
              </Button>
            )}
            {step < 2 ? (
              <Button
                onClick={() => setStep(step + 1)}
                disabled={initializing || !setupData}
              >
                {t('Next')}
              </Button>
            ) : (
              <Button
                onClick={handleEnable}
                disabled={initializing || loading || !code}
              >
                {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
                {loading ? t('Enabling...') : t('Enable 2FA')}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
