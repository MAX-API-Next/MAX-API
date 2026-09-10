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
import { type ReactElement } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatNumber, formatQuota } from '@/lib/format'
import { useSystemConfig } from '@/hooks/use-system-config'
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
import { Form, FormField } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  getManualTaskSettlementSchema,
  type ManualTaskSettlementFormValues,
} from '../lib/manual-task-settlement'
import type { BillingSettlementReconciliationItem } from '../types'

interface ManualTaskSettlementDialogProps {
  item: BillingSettlementReconciliationItem | null
  pending: boolean
  stale: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (
    item: BillingSettlementReconciliationItem,
    actualQuota: number
  ) => void
}

export function ManualTaskSettlementDialog(
  props: ManualTaskSettlementDialogProps
): ReactElement {
  const { t } = useTranslation()
  const { currency } = useSystemConfig()
  const item = props.item
  const form = useForm<ManualTaskSettlementFormValues>({
    resolver: zodResolver(
      getManualTaskSettlementSchema(t, props.item?.task_quota ?? 0)
    ),
    defaultValues: { actualQuota: '' },
    mode: 'onChange',
  })
  const canSubmit =
    Boolean(props.item) && !props.stale && form.formState.isValid

  const handleSubmit = form.handleSubmit((values) => {
    if (props.item && !props.stale) {
      props.onSubmit(props.item, Number(values.actualQuota))
    }
  })

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

        {item && (
          <>
            <Alert>
              <AlertTitle>
                {t('Task #{{id}} · reserved {{quota}}', {
                  id: item.task_id,
                  quota: formatQuota(item.task_quota),
                })}
              </AlertTitle>
              <AlertDescription>
                {t(
                  'Only an exact quota from verified provider evidence is allowed. This workflow cannot add a charge above the original reservation.'
                )}
              </AlertDescription>
            </Alert>

            {props.stale && (
              <Alert variant='destructive'>
                <AlertTitle>
                  {t(
                    'This reconciliation record changed while the dialog was open.'
                  )}
                </AlertTitle>
                <AlertDescription>
                  {t(
                    'Close this dialog and reopen the latest record before submitting.'
                  )}
                </AlertDescription>
              </Alert>
            )}

            <Form {...form}>
              <form id='manual-task-settlement-form' onSubmit={handleSubmit}>
                <FieldGroup>
                  <FormField
                    control={form.control}
                    name='actualQuota'
                    render={({ field, fieldState }) => (
                      <Field data-invalid={fieldState.invalid}>
                        <FieldLabel htmlFor='manual-task-actual-quota'>
                          {t('Exact final quota')}
                        </FieldLabel>
                        <Input
                          {...field}
                          id='manual-task-actual-quota'
                          type='number'
                          inputMode='numeric'
                          min={0}
                          max={item.task_quota}
                          step={1}
                          disabled={props.pending || props.stale}
                          aria-invalid={fieldState.invalid}
                        />
                        <FieldDescription>
                          {t(
                            'Enter quota value. {{quotaPerUnit}} quota = $1. Allowed range: 0 to {{quota}}.',
                            {
                              quotaPerUnit: formatNumber(currency.quotaPerUnit),
                              quota: formatQuota(item.task_quota),
                            }
                          )}
                        </FieldDescription>
                        {fieldState.error && (
                          <FieldError>{fieldState.error.message}</FieldError>
                        )}
                      </Field>
                    )}
                  />
                </FieldGroup>
              </form>
            </Form>
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
            type='submit'
            form='manual-task-settlement-form'
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
