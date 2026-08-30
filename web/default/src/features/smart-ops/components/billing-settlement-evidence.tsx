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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldTitle,
} from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { updateBillingSettlementBlockingPolicy } from '../api'
import { formatCount, formatLocalizedCount } from '../lib/format'
import { mutationErrorMessage } from '../lib/mutation-error'
import {
  SMART_OPS_ACTIVE_ALERTS_QUERY_KEY,
  SMART_OPS_BILLING_RECONCILIATION_QUERY_KEY,
} from '../lib/query-keys'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
} from '../types'
import { BillingSettlementTable } from './billing-settlement-table'
import { SettlementReviewDialog } from './settlement-review-dialog'

interface BillingSettlementEvidenceProps {
  canUpdateBlockingPolicy: boolean
  data?: BillingSettlementReconciliationData
  error: Error | null
  loading: boolean
  onRetry: () => void
}

export function BillingSettlementEvidence(
  props: BillingSettlementEvidenceProps
): ReactElement {
  const { t, i18n } = useTranslation()
  const [selectedItem, setSelectedItem] =
    useState<BillingSettlementReconciliationItem | null>(null)
  const [policyOverride, setPolicyOverride] = useState<boolean | null>(null)
  const queryClient = useQueryClient()
  const policyValue =
    policyOverride ?? props.data?.block_user_by_default ?? true

  const policyMutation = useMutation({
    mutationKey: ['smart-ops', 'billing-settlement-blocking-policy'],
    mutationFn: async (checked: boolean) => {
      const response = await updateBillingSettlementBlockingPolicy(checked)
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to update blocking policy.')
        )
      }
    },
    onSuccess: async () => {
      toast.success(t('Default user-blocking policy updated.'))
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: SMART_OPS_ACTIVE_ALERTS_QUERY_KEY,
        }),
        queryClient.invalidateQueries({
          queryKey: SMART_OPS_BILLING_RECONCILIATION_QUERY_KEY,
        }),
      ])
    },
    onError: (error) => {
      handleServerError(error, {
        fallback: mutationErrorMessage(
          error,
          t('Failed to update blocking policy.')
        ),
      })
    },
    onSettled: () => {
      setPolicyOverride(null)
    },
  })

  const handlePolicyChange = (checked: boolean) => {
    setPolicyOverride(checked)
    policyMutation.mutate(checked)
  }

  if (props.loading) {
    return (
      <div className='flex flex-col gap-2 border-t pt-4'>
        <Skeleton className='h-16 w-full rounded-md' />
        <Skeleton className='h-40 w-full rounded-md' />
      </div>
    )
  }

  if (props.error) {
    return (
      <Alert variant='destructive'>
        <TriangleAlert aria-hidden='true' />
        <AlertTitle>
          {t('We could not load billing reconciliation details.')}
        </AlertTitle>
        <AlertDescription>
          <p>{props.error.message}</p>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onRetry}
            className='mt-2'
          >
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }

  if (!props.data) {
    return <></>
  }

  return (
    <section
      aria-labelledby='billing-reconciliation-heading'
      className='flex flex-col gap-3 border-t pt-4'
    >
      <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
        <div className='min-w-0'>
          <h4
            id='billing-reconciliation-heading'
            className='text-sm font-semibold'
          >
            {t('Billing reconciliation controls')}
          </h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t(
              'Review and close operational alerts without changing the underlying pending or manual financial settlement.'
            )}
          </p>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Badge variant='outline'>
            {formatLocalizedCount(
              props.data.open_alert_count,
              i18n.language,
              t,
              'Open alert: {{count}}',
              'Open alerts: {{count}}'
            )}
          </Badge>
          <Badge variant='outline'>
            {formatLocalizedCount(
              props.data.reviewed_count,
              i18n.language,
              t,
              'Reviewed record: {{count}}',
              'Reviewed records: {{count}}'
            )}
          </Badge>
          <Badge variant='outline'>
            {t('Pending: {{count}}', {
              count: formatCount(props.data.pending_count, i18n.language),
            })}
          </Badge>
          <Badge variant='outline'>
            {t('Manual: {{count}}', {
              count: formatCount(props.data.manual_count, i18n.language),
            })}
          </Badge>
          <Badge variant='outline'>
            {formatLocalizedCount(
              props.data.blocked_user_count,
              i18n.language,
              t,
              'Blocked user: {{count}}',
              'Blocked users: {{count}}'
            )}
          </Badge>
        </div>
      </div>

      <Field className='rounded-lg border p-3' orientation='horizontal'>
        <FieldContent>
          <FieldTitle>{t('Block affected users by default')}</FieldTitle>
          <FieldDescription>
            {t(
              'When enabled, unresolved positive final settlements block new paid requests unless a reviewed record explicitly allows the user to continue.'
            )}
          </FieldDescription>
          {!props.canUpdateBlockingPolicy && (
            <FieldDescription>
              {t(
                'Only root administrators can change the default blocking policy.'
              )}
            </FieldDescription>
          )}
        </FieldContent>
        <Switch
          id='billing-reconciliation-block-user-default'
          checked={policyValue}
          onCheckedChange={handlePolicyChange}
          disabled={policyMutation.isPending || !props.canUpdateBlockingPolicy}
          aria-label={t('Block affected users by default')}
        />
      </Field>

      {props.data.items.length === 0 ? (
        <Alert>
          <CheckCircle2 aria-hidden='true' />
          <AlertTitle>{t('No unresolved reconciliation records.')}</AlertTitle>
          <AlertDescription>
            {t(
              'There are no pending or manual positive final settlements requiring operator review.'
            )}
          </AlertDescription>
        </Alert>
      ) : (
        <BillingSettlementTable
          items={props.data.items}
          onSelectItem={setSelectedItem}
        />
      )}

      {props.data.truncated && (
        <Alert>
          <AlertDescription>
            {t(
              'Showing the oldest {{count}} records; the summary covers all {{total}} unresolved records.',
              {
                count: formatCount(props.data.items.length, i18n.language),
                total: formatCount(props.data.total_count, i18n.language),
              }
            )}
          </AlertDescription>
        </Alert>
      )}
      <p className='text-muted-foreground text-right text-xs'>
        {t('Generated at {{time}}', {
          time: formatTimestampToDate(props.data.generated_at),
        })}
      </p>
      {selectedItem && (
        <SettlementReviewDialog
          key={`${selectedItem.id}:${selectedItem.reconciliation_reviewed_at}`}
          item={selectedItem}
          open
          onOpenChange={(open) => {
            if (!open) setSelectedItem(null)
          }}
        />
      )}
    </section>
  )
}
