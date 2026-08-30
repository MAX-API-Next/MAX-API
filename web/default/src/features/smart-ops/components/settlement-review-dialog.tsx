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
import { useState, type ReactElement } from 'react'
import { Loader2, LockKeyhole, LockOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
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
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { reviewBillingSettlement } from '../api'
import { formatLocalizedCount } from '../lib/format'
import { mutationErrorMessage } from '../lib/mutation-error'
import type { BillingSettlementReconciliationItem } from '../types'

interface SettlementReviewDialogProps {
  item: BillingSettlementReconciliationItem
  open: boolean
  onOpenChange: (open: boolean) => void
  onReviewed: () => Promise<void>
}

export function SettlementReviewDialog(
  props: SettlementReviewDialogProps
): ReactElement {
  const { t, i18n } = useTranslation()
  const [decision, setDecision] = useState<'block' | 'allow'>(
    props.item.record_blocks_user ? 'block' : 'allow'
  )
  const [note, setNote] = useState(props.item.reconciliation_review_note || '')
  const [saving, setSaving] = useState(false)

  const trimmedNote = note.trim()
  const noteLength = Array.from(trimmedNote).length
  const noteInvalid = noteLength > 0 && (noteLength < 3 || noteLength > 1000)

  const handleSubmit = async () => {
    if (noteLength < 3 || noteLength > 1000) return
    setSaving(true)
    try {
      const response = await reviewBillingSettlement(props.item.id, {
        block_user: decision === 'block',
        note: trimmedNote,
      })
      if (!response.success) {
        throw new Error(response.message || t('Failed to save review.'))
      }
      toast.success(t('Billing reconciliation alert closed.'))
      props.onOpenChange(false)
      await props.onReviewed()
    } catch (error) {
      handleServerError(error, {
        fallback: mutationErrorMessage(error, t('Failed to save review.')),
      })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!saving) props.onOpenChange(open)
      }}
    >
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.item.reconciliation_reviewed_at
              ? t('Edit reconciliation review')
              : t('Review billing reconciliation')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Closing this alert records an administrator review only. It does not mark the settlement as applied or change any balance.'
            )}
          </DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field>
            <FieldLabel>{t('User access while unresolved')}</FieldLabel>
            <ToggleGroup
              value={[decision]}
              onValueChange={(value) => {
                const nextDecision = value[0]
                if (nextDecision === 'block' || nextDecision === 'allow') {
                  setDecision(nextDecision)
                }
              }}
              variant='outline'
              spacing={2}
              className='grid w-full grid-cols-1 sm:grid-cols-2'
            >
              <ToggleGroupItem
                value='block'
                aria-label={t('Continue blocking user')}
                className='h-auto min-w-0 justify-start px-3 py-2 whitespace-normal'
              >
                <LockKeyhole aria-hidden='true' />
                {t('Continue blocking user')}
              </ToggleGroupItem>
              <ToggleGroupItem
                value='allow'
                aria-label={t('Allow user to continue')}
                className='h-auto min-w-0 justify-start px-3 py-2 whitespace-normal'
              >
                <LockOpen aria-hidden='true' />
                {t('Allow user to continue')}
              </ToggleGroupItem>
            </ToggleGroup>
            <FieldDescription>
              {t(
                'This choice affects only new paid-request admission while the financial settlement remains pending or manual.'
              )}
            </FieldDescription>
          </Field>

          <Field data-invalid={noteInvalid || undefined}>
            <FieldLabel htmlFor='billing-reconciliation-review-note'>
              {t('Review note')}
            </FieldLabel>
            <Textarea
              id='billing-reconciliation-review-note'
              value={note}
              onChange={(event) => setNote(event.target.value)}
              rows={5}
              aria-invalid={noteInvalid || undefined}
              placeholder={t(
                'Record the evidence checked, the conclusion, and any follow-up action.'
              )}
            />
            <FieldDescription>
              {formatLocalizedCount(
                noteLength,
                i18n.language,
                t,
                '{{count}} / 1000 character',
                '{{count}} / 1000 characters'
              )}
            </FieldDescription>
            {noteInvalid && (
              <FieldError>
                {t('Review note must contain between 3 and 1000 characters.')}
              </FieldError>
            )}
          </Field>
        </FieldGroup>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={saving}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={() => void handleSubmit()}
            disabled={saving || noteLength < 3 || noteLength > 1000}
          >
            {saving && (
              <Loader2
                data-icon='inline-start'
                className='animate-spin'
                aria-hidden='true'
              />
            )}
            {props.item.reconciliation_reviewed_at
              ? t('Save review')
              : t('Close alert')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
