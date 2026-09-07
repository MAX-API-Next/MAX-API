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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Loader2, TriangleAlert } from 'lucide-react'
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
import {
  completeManualTaskBillingSettlement,
  reviewBillingSettlements,
  updateBillingSettlementBlockingPolicy,
} from '../api'
import { formatCount, formatLocalizedCount } from '../lib/format'
import { mutationErrorMessage } from '../lib/mutation-error'
import {
  SMART_OPS_ACTIVE_ALERTS_QUERY_KEY,
  SMART_OPS_BILLING_RECONCILIATION_QUERY_KEY,
} from '../lib/query-keys'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
  BillingSettlementReviewTarget,
} from '../types'
import { BillingSettlementTable } from './billing-settlement-table'
import { ManualTaskSettlementDialog } from './manual-task-settlement-dialog'

interface BillingSettlementEvidenceProps {
  canCompleteManualTask: boolean
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
  const [selectedTargets, setSelectedTargets] = useState<
    Map<number, BillingSettlementReviewTarget>
  >(() => new Map())
  const [manualTaskItem, setManualTaskItem] =
    useState<BillingSettlementReconciliationItem | null>(null)
  const reconciliationItems = props.data?.items
  const currentManualTaskItem = useMemo(() => {
    if (!manualTaskItem) return null
    return (
      reconciliationItems?.find((item) => item.id === manualTaskItem.id) ?? null
    )
  }, [manualTaskItem, reconciliationItems])
  const manualTaskItemStale = Boolean(
    manualTaskItem &&
    (!currentManualTaskItem ||
      currentManualTaskItem.revision !== manualTaskItem.revision ||
      !currentManualTaskItem.requires_manual_completion)
  )
  const { activeSelectedTargets, activeSelectedTargetMap } = useMemo(() => {
    const currentRevisions = new Map(
      reconciliationItems?.map((item) => [item.id, item.revision]) ?? []
    )
    const targets = Array.from(selectedTargets.values()).filter(
      (target) => currentRevisions.get(target.id) === target.revision
    )
    return {
      activeSelectedTargets: targets,
      activeSelectedTargetMap: new Map(
        targets.map((target) => [target.id, target])
      ),
    }
  }, [reconciliationItems, selectedTargets])
  const [policyOverride, setPolicyOverride] = useState<boolean | null>(null)
  const queryClient = useQueryClient()
  const policyValue =
    policyOverride ?? props.data?.block_user_by_default ?? false

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

  const reviewMutation = useMutation({
    mutationKey: ['smart-ops', 'billing-settlement-reviews'],
    mutationFn: async (
      targets: BillingSettlementReviewTarget[]
    ): Promise<number> => {
      const response = await reviewBillingSettlements({ items: targets })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to close reconciliation alerts.')
        )
      }
      return targets.length
    },
    onSuccess: async (count: number): Promise<void> => {
      setSelectedTargets(new Map())
      toast.success(
        t('Billing reconciliation alerts closed: {{count}}', { count })
      )
    },
    onSettled: async () => {
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
          t('Failed to close reconciliation alerts.')
        ),
      })
    },
  })

  const reviewTargets = (targets: BillingSettlementReviewTarget[]): void => {
    if (targets.length > 0 && !reviewMutation.isPending) {
      reviewMutation.mutate(targets)
    }
  }

  const manualTaskCompletionMutation = useMutation({
    mutationKey: ['smart-ops', 'manual-task-billing-completion'],
    mutationFn: async ({
      item,
      actualQuota,
      note,
    }: {
      item: BillingSettlementReconciliationItem
      actualQuota: number
      note: string
    }): Promise<void> => {
      const response = await completeManualTaskBillingSettlement(item.id, {
        revision: item.revision,
        actual_quota: actualQuota,
        note,
      })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to complete manual task billing.')
        )
      }
    },
    onSuccess: () => {
      setManualTaskItem(null)
      toast.success(t('Manual task billing completed.'))
    },
    onSettled: async () => {
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
          t('Failed to complete manual task billing.')
        ),
      })
    },
  })

  const replaceSelectedTargets = (
    targets: Map<number, BillingSettlementReviewTarget>
  ): void => {
    setSelectedTargets(targets)
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
            {t('Open billing reconciliation alerts')}
          </h4>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t(
              'Batch-close ordinary alerts after review. Task-finalization alerts require a root administrator to enter the exact provider-backed quota before they can close.'
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
            {t('Open pending settlements: {{count}}', {
              count: formatCount(props.data.pending_count, i18n.language),
            })}
          </Badge>
          <Badge variant='outline'>
            {t('Open manual settlements: {{count}}', {
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
              'When enabled, new paid requests remain blocked while any unresolved positive final settlement record still blocks the user. Allowing one reviewed record does not override other blocking records.'
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
          <AlertTitle>{t('No open reconciliation alerts.')}</AlertTitle>
          <AlertDescription>
            {t(
              'There are no pending or manual positive final settlements waiting for administrator review.'
            )}
          </AlertDescription>
        </Alert>
      ) : (
        <div className='flex flex-col gap-2'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <span className='text-muted-foreground text-xs'>
              {t('Selected alerts: {{count}}', {
                count: formatCount(activeSelectedTargets.length, i18n.language),
              })}
            </span>
            <Button
              type='button'
              size='sm'
              onClick={() => reviewTargets(activeSelectedTargets)}
              disabled={
                activeSelectedTargets.length === 0 || reviewMutation.isPending
              }
            >
              {reviewMutation.isPending && (
                <Loader2
                  data-icon='inline-start'
                  className='animate-spin'
                  aria-hidden='true'
                />
              )}
              {t('Review and close selected ({{count}})', {
                count: formatCount(activeSelectedTargets.length, i18n.language),
              })}
            </Button>
          </div>
          <BillingSettlementTable
            items={props.data.items}
            canCompleteManualTask={props.canCompleteManualTask}
            selectedTargets={activeSelectedTargetMap}
            reviewPending={
              reviewMutation.isPending || manualTaskCompletionMutation.isPending
            }
            onSelectedTargetsChange={replaceSelectedTargets}
            onReviewTargets={reviewTargets}
            onCompleteManualTask={setManualTaskItem}
          />
          {manualTaskItem && (
            <ManualTaskSettlementDialog
              key={`${manualTaskItem.id}:${manualTaskItem.revision}`}
              item={manualTaskItem}
              pending={manualTaskCompletionMutation.isPending}
              stale={manualTaskItemStale}
              onOpenChange={(open) => {
                if (!open) setManualTaskItem(null)
              }}
              onSubmit={(item, actualQuota, note) =>
                manualTaskCompletionMutation.mutate({
                  item,
                  actualQuota,
                  note,
                })
              }
            />
          )}
        </div>
      )}

      {props.data.truncated && (
        <Alert>
          <AlertDescription>
            {t(
              'Showing the oldest {{count}} alerts; the summary covers all {{total}} open alerts.',
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
    </section>
  )
}
