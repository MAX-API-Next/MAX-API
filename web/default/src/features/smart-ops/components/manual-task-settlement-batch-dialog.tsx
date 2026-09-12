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
import { useFieldArray, useForm } from 'react-hook-form'
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
  getManualTaskSettlementBatchSchema,
  type ManualTaskSettlementBatchFormValues,
} from '../lib/manual-task-settlement'
import type { BillingSettlementReconciliationItem } from '../types'

interface ManualTaskSettlementBatchDialogProps {
  items: BillingSettlementReconciliationItem[]
  pending: boolean
  stale: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (
    items: BillingSettlementReconciliationItem[],
    actualQuotas: Record<number, number>
  ) => void
}

export function ManualTaskSettlementBatchDialog(
  props: ManualTaskSettlementBatchDialogProps
): ReactElement {
  const { t } = useTranslation()
  const { currency, loading: configLoading } = useSystemConfig()
  const maxQuotas = props.items.map((item) => item.task_quota)
  const form = useForm<ManualTaskSettlementBatchFormValues>({
    resolver: zodResolver(getManualTaskSettlementBatchSchema(t, maxQuotas)),
    defaultValues: {
      items: props.items.map(() => ({ actualQuota: '0' })),
    },
    mode: 'onChange',
  })
  const { fields } = useFieldArray({
    control: form.control,
    name: 'items',
  })

  const canSubmit =
    props.items.length > 0 &&
    !props.pending &&
    !props.stale &&
    !configLoading &&
    form.formState.isValid

  const handleSubmit = form.handleSubmit((values) => {
    const actualQuotas = Object.fromEntries(
      props.items.map((item, index) => [
        item.id,
        Number(values.items[index]?.actualQuota),
      ])
    ) as Record<number, number>
    props.onSubmit(props.items, actualQuotas)
  })

  return (
    <Dialog
      open={props.items.length > 0}
      onOpenChange={(open) => {
        if (!props.pending) props.onOpenChange(open)
      }}
    >
      <DialogContent
        className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'
        showCloseButton={!props.pending}
      >
        <DialogHeader>
          <DialogTitle>{t('Complete manual task billing')}</DialogTitle>
          <DialogDescription>
            {t(
              'Enter an exact final quota for each selected task. Each value is checked against its own reserved quota.'
            )}
          </DialogDescription>
        </DialogHeader>

        {configLoading && (
          <Alert>
            <AlertTitle>{t('Loading...')}</AlertTitle>
          </Alert>
        )}
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
          <form id='manual-task-settlement-batch-form' onSubmit={handleSubmit}>
            <FieldGroup>
              {fields.map((field, index) => {
                const item = props.items[index]
                if (!item) return null
                return (
                  <FormField
                    key={field.id}
                    control={form.control}
                    name={`items.${index}.actualQuota` as const}
                    render={({ field: inputField, fieldState }) => (
                      <Field data-invalid={fieldState.invalid}>
                        <FieldLabel
                          htmlFor={`manual-task-actual-quota-${item.id}`}
                        >
                          {t('Task #{{id}} · reserved {{quota}}', {
                            id: item.task_id,
                            quota: formatQuota(item.task_quota),
                          })}
                        </FieldLabel>
                        <Input
                          {...inputField}
                          id={`manual-task-actual-quota-${item.id}`}
                          type='number'
                          inputMode='numeric'
                          min={0}
                          max={item.task_quota}
                          step={1}
                          disabled={
                            props.pending || props.stale || configLoading
                          }
                          aria-invalid={fieldState.invalid}
                          onInput={(event) => inputField.onChange(event)}
                        />
                        <FieldDescription>
                          {t(
                            'Allowed range: 0 to {{quota}} ({{quotaPerUnit}} quota = $1).',
                            {
                              quota: formatQuota(item.task_quota),
                              quotaPerUnit: formatNumber(currency.quotaPerUnit),
                            }
                          )}
                        </FieldDescription>
                        {fieldState.error && (
                          <FieldError>{fieldState.error.message}</FieldError>
                        )}
                      </Field>
                    )}
                  />
                )
              })}
            </FieldGroup>
          </form>
        </Form>

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
            form='manual-task-settlement-batch-form'
            disabled={!canSubmit}
          >
            {props.pending && (
              <Loader2
                data-icon='inline-start'
                className='animate-spin'
                aria-hidden='true'
              />
            )}
            {t('Apply exact settlements')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
