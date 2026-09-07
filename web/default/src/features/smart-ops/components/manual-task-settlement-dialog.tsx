/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { useMemo, useState, type ReactElement } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { BillingSettlementReconciliationItem } from '../types'

interface ManualTaskSettlementDialogProps {
  item: BillingSettlementReconciliationItem | null
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (
    item: BillingSettlementReconciliationItem,
    actualQuota: number,
    note: string
  ) => void
}

export function ManualTaskSettlementDialog(
  props: ManualTaskSettlementDialogProps
): ReactElement {
  const { t } = useTranslation()
  const [actualQuotaInput, setActualQuotaInput] = useState('')
  const [note, setNote] = useState('')

  const actualQuota = Number(actualQuotaInput)
  const quotaError = useMemo((): string => {
    if (!props.item || actualQuotaInput.trim() === '') {
      return t('Enter the exact final quota.')
    }
    if (!Number.isSafeInteger(actualQuota) || actualQuota < 0) {
      return t('Final quota must be a non-negative safe integer.')
    }
    if (actualQuota > props.item.task_quota) {
      return t('Final quota cannot exceed the reserved quota.')
    }
    return ''
  }, [actualQuota, actualQuotaInput, props.item, t])
  const trimmedNote = note.trim()
  const noteError = useMemo((): string => {
    const length = Array.from(trimmedNote).length
    if (length < 3 || length > 1000) {
      return t('Audit note must contain between 3 and 1000 characters.')
    }
    return ''
  }, [t, trimmedNote])
  const canSubmit = Boolean(props.item) && quotaError === '' && noteError === ''

  return (
    <Dialog
      open={props.item !== null}
      onOpenChange={(open) => {
        if (!props.pending) props.onOpenChange(open)
      }}
    >
      <DialogContent className='sm:max-w-lg' showCloseButton={!props.pending}>
        <DialogHeader>
          <DialogTitle>{t('Complete manual task billing')}</DialogTitle>
          <DialogDescription>
            {t(
              'Enter the exact provider-backed final quota. This action may refund the unused reservation and then unlock the task terminal state.'
            )}
          </DialogDescription>
        </DialogHeader>

        {props.item && (
          <>
            <Alert>
              <AlertTitle>
                {t('Task #{{id}} · reserved {{quota}}', {
                  id: props.item.task_id,
                  quota: formatQuota(props.item.task_quota),
                })}
              </AlertTitle>
              <AlertDescription>
                {t(
                  'Only an exact quota from verified provider evidence is allowed. This workflow cannot add a charge above the original reservation.'
                )}
              </AlertDescription>
            </Alert>

            <FieldGroup>
              <Field data-invalid={quotaError !== ''}>
                <FieldLabel htmlFor='manual-task-actual-quota'>
                  {t('Exact final quota')}
                </FieldLabel>
                <Input
                  id='manual-task-actual-quota'
                  type='number'
                  inputMode='numeric'
                  min={0}
                  max={props.item.task_quota}
                  step={1}
                  value={actualQuotaInput}
                  onChange={(event) => setActualQuotaInput(event.target.value)}
                  disabled={props.pending}
                  aria-invalid={quotaError !== ''}
                />
                <FieldDescription>
                  {t('Allowed range: 0 to {{quota}}.', {
                    quota: formatQuota(props.item.task_quota),
                  })}
                </FieldDescription>
                {actualQuotaInput !== '' && quotaError !== '' && (
                  <FieldError>{quotaError}</FieldError>
                )}
              </Field>

              <Field data-invalid={noteError !== ''}>
                <FieldLabel htmlFor='manual-task-audit-note'>
                  {t('Audit note')}
                </FieldLabel>
                <Textarea
                  id='manual-task-audit-note'
                  value={note}
                  onChange={(event) => setNote(event.target.value)}
                  placeholder={t(
                    'Describe the provider evidence and calculation used.'
                  )}
                  maxLength={1000}
                  disabled={props.pending}
                  aria-invalid={noteError !== ''}
                />
                <FieldDescription>
                  {t(
                    'This note is bound to the idempotent financial operation.'
                  )}
                </FieldDescription>
                {note !== '' && noteError !== '' && (
                  <FieldError>{noteError}</FieldError>
                )}
              </Field>
            </FieldGroup>
          </>
        )}

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={props.pending}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={() => {
              if (props.item && canSubmit) {
                props.onSubmit(props.item, actualQuota, trimmedNote)
              }
            }}
            disabled={!canSubmit || props.pending}
          >
            {props.pending && (
              <Loader2
                data-icon='inline-start'
                className='animate-spin'
                aria-hidden='true'
              />
            )}
            {t('Apply exact settlement')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
