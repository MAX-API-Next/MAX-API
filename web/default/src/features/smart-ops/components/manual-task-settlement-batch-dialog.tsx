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
import {
  useCallback,
  useMemo,
  useRef,
  useState,
  type ReactElement,
} from 'react'
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
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { getManualTaskSettlementSchema } from '../lib/manual-task-settlement'
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
  const [values, setValues] = useState<Record<number, string>>(
    () =>
      Object.fromEntries(props.items.map((item) => [item.id, ''])) as Record<
        number,
        string
      >
  )
  const inputRefs = useRef<Record<number, HTMLInputElement | null>>({})
  const [submitted, setSubmitted] = useState(false)

  const schemas = useMemo(() => {
    const next = new Map<
      number,
      ReturnType<typeof getManualTaskSettlementSchema>
    >()
    for (const item of props.items) {
      next.set(
        item.task_quota,
        getManualTaskSettlementSchema(t, item.task_quota)
      )
    }
    return next
  }, [props.items, t])

  const getValidationErrors = useCallback(
    (
      nextValues: Record<number, string>
    ): Record<number, string | undefined> => {
      const next: Record<number, string | undefined> = {}
      for (const item of props.items) {
        const result = schemas.get(item.task_quota)?.safeParse({
          actualQuota: nextValues[item.id] ?? '',
        })
        next[item.id] = result?.success
          ? undefined
          : result?.error.issues[0]?.message
      }
      return next
    },
    [props.items, schemas]
  )

  const errors = useMemo(
    () => getValidationErrors(values),
    [getValidationErrors, values]
  )

  const canSubmit =
    props.items.length > 0 &&
    !props.pending &&
    !props.stale &&
    !configLoading &&
    !Object.values(errors).some(Boolean)

  const handleSubmit = (): void => {
    setSubmitted(true)
    if (!canSubmit) return
    const submittedValues = Object.fromEntries(
      props.items.map((item) => [
        item.id,
        inputRefs.current[item.id]?.value ?? values[item.id] ?? '',
      ])
    ) as Record<number, string>
    const submittedErrors = getValidationErrors(submittedValues)
    if (props.items.some((item) => submittedErrors[item.id])) {
      setValues(submittedValues)
      return
    }
    const actualQuotas = Object.fromEntries(
      props.items.map((item) => [item.id, Number(submittedValues[item.id])])
    ) as Record<number, number>
    props.onSubmit(props.items, actualQuotas)
  }

  const updateValue = (id: number, value: string): void => {
    setValues((current) => ({
      ...current,
      [id]: value,
    }))
  }

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
        <div className='flex flex-col gap-3'>
          {props.items.map((item) => (
            <Field
              key={`${item.id}:${item.revision}`}
              data-invalid={submitted && Boolean(errors[item.id])}
            >
              <FieldLabel htmlFor={`manual-task-actual-quota-${item.id}`}>
                {t('Task #{{id}} · reserved {{quota}}', {
                  id: item.task_id,
                  quota: formatQuota(item.task_quota),
                })}
              </FieldLabel>
              <Input
                id={`manual-task-actual-quota-${item.id}`}
                type='number'
                inputMode='numeric'
                min={0}
                max={item.task_quota}
                step={1}
                value={values[item.id] ?? ''}
                ref={(element) => {
                  inputRefs.current[item.id] = element
                }}
                disabled={props.pending || props.stale || configLoading}
                aria-invalid={submitted && Boolean(errors[item.id])}
                onInput={(event) =>
                  updateValue(item.id, (event.target as HTMLInputElement).value)
                }
                onChange={(event) =>
                  updateValue(item.id, (event.target as HTMLInputElement).value)
                }
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
              {submitted && errors[item.id] && (
                <FieldError>{errors[item.id]}</FieldError>
              )}
            </Field>
          ))}
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={props.pending}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSubmit} disabled={!canSubmit}>
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
