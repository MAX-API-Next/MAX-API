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
import { Trans, useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
} from '@/components/ui/field'
import type { SystemStatus } from '../types'

interface LegalConsentProps {
  status: SystemStatus | null
  checked: boolean
  onCheckedChange: (nextValue: boolean) => void
  invalid?: boolean
  errorMessage?: string
  className?: string
}

export function LegalConsent({
  status,
  checked,
  onCheckedChange,
  invalid = false,
  errorMessage,
  className,
}: LegalConsentProps) {
  const { t } = useTranslation()
  const hasUserAgreement = Boolean(status?.user_agreement_enabled)
  const hasPrivacyPolicy = Boolean(status?.privacy_policy_enabled)
  const showError = invalid && !checked
  const errorId = showError ? 'legal-consent-error' : undefined

  if (!hasUserAgreement && !hasPrivacyPolicy) {
    return null
  }

  const handleChange = (value: boolean) => {
    onCheckedChange(value === true)
  }

  const linkClassName =
    'text-primary font-medium underline-offset-4 hover:underline'

  let consentKey =
    'I have read and agree to the <agreement>User Agreement</agreement>.'
  if (hasUserAgreement && hasPrivacyPolicy) {
    consentKey =
      'I have read and agree to the <agreement>User Agreement</agreement> and <privacy>Privacy Policy</privacy>.'
  } else if (hasPrivacyPolicy) {
    consentKey =
      'I have read and agree to the <privacy>Privacy Policy</privacy>.'
  }

  return (
    <Field
      data-invalid={showError ? true : undefined}
      orientation='horizontal'
      className={cn(
        'border-primary/30 bg-primary/5 rounded-lg border p-3 shadow-sm transition-colors',
        showError && 'border-destructive/70 bg-destructive/10',
        checked && 'border-primary/50 bg-primary/10',
        className
      )}
    >
      <Checkbox
        id='legal-consent'
        checked={checked}
        onCheckedChange={handleChange}
        aria-invalid={showError ? true : undefined}
        aria-describedby={errorId}
        className='mt-0.5 size-5'
      />
      <FieldContent className='gap-1'>
        <FieldLabel
          htmlFor='legal-consent'
          className='w-full cursor-pointer text-left text-sm leading-5 font-medium'
        >
          <span>
            <Trans
              i18nKey={consentKey}
              components={{
                agreement: (
                  <a
                    href='/user-agreement'
                    target='_blank'
                    rel='noopener noreferrer'
                    className={linkClassName}
                  />
                ),
                privacy: (
                  <a
                    href='/privacy-policy'
                    target='_blank'
                    rel='noopener noreferrer'
                    className={linkClassName}
                  />
                ),
              }}
            />
          </span>
        </FieldLabel>
        <FieldDescription className='text-xs leading-5'>
          {t('Required before you continue.')}
        </FieldDescription>
        <FieldError id={errorId}>{showError ? errorMessage : null}</FieldError>
      </FieldContent>
    </Field>
  )
}
