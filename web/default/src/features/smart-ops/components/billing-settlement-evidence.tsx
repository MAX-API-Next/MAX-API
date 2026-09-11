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
import type { TFunction } from 'i18next'
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
  completeManualTaskBillingSettlements,
  completeManualTaskBillingSettlementsZero,
  reviewBillingSettlements,
  updateBillingSettlementBlockingPolicy,
} from '../api'
import { formatCount, formatLocalizedCount } from '../lib/format'
import { mutationErrorMessage } from '../lib/mutation-error'
import {
  SMART_OPS_ACTIVE_ALERTS_QUERY_KEY,
  SMART_OPS_BILLING_RECONCILIATION_QUERY_KEY,
} from '../lib/query-keys'
import { classifyBillingSettlementReviewSelection } from '../lib/reconciliation-validation'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
  ManualTaskBillingBatchCompletionData,
  ManualTaskBillingBatchFailure,
  BillingSettlementReviewTarget,
} from '../types'
import { BillingSettlementTable } from './billing-settlement-table'
import { ManualTaskSettlementBatchDialog } from './manual-task-settlement-batch-dialog'
import { ManualTaskSettlementDialog } from './manual-task-settlement-dialog'

function formatBatchFailureMessage(
  failure: ManualTaskBillingBatchFailure,
  t: TFunction
): string {
  const code =
    failure.code ??
    (failure.message ===
    'record changed or could not be applied safely; refresh and reconcile it'
      ? 'record_conflict'
      : undefined)
  switch (code) {
    case 'minimax_h3_required':
      return t(
        'Only MiniMax-H3 task settlements can use the zero-quota batch action'
      )
    case 'record_conflict':
      return t(
        'manual task billing settlement could not be applied safely; refresh and reconcile the current record'
      )
    case 'token_quota_inconsistent':
      return t(
        'the token quota mirror is inconsistent; repair the token record before completing this settlement'
      )
    case 'subscription_refund_clamped':
      return t(
        'the subscription usage mirror is lower than the refund; repair the subscription record before completing this settlement'
      )
    case 'subscription_reservation_invalid':
      return t(
        'the subscription reservation is unbound or its period changed; escalate this record for manual reconciliation'
      )
    case 'manual_review_required':
      return t('record still requires a manual financial review')
    default:
      return t('Failed to complete manual task billing.')
  }
}

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
  const [manualTaskBatchItems, setManualTaskBatchItems] = useState<
    BillingSettlementReconciliationItem[]
  >([])
  const [batchFailures, setBatchFailures] = useState<
    ManualTaskBillingBatchFailure[]
  >([])
  const reconciliationItems = props.data?.items
  const currentManualTaskItem = useMemo(() => {
    if (!manualTaskItem) return null
    return (
      reconciliationItems?.find((item) => item.id === manualTaskItem.id) ?? null
    )
  }, [manualTaskItem, reconciliationItems])
  const manualTaskItemStale = Boolean(
    manualTaskItem &&
    (props.error ||
      !props.data ||
      !currentManualTaskItem ||
      currentManualTaskItem.revision !== manualTaskItem.revision ||
      !currentManualTaskItem.requires_manual_completion)
  )
  const manualTaskBatchStale = useMemo(() => {
    if (manualTaskBatchItems.length === 0) return false
    return manualTaskBatchItems.some((selected) => {
      const current = reconciliationItems?.find(
        (item) => item.id === selected.id
      )
      return (
        !current ||
        current.revision !== selected.revision ||
        !current.requires_manual_completion
      )
    })
  }, [manualTaskBatchItems, reconciliationItems])
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
  const selectedManualItems = useMemo(
    () =>
      (reconciliationItems ?? []).filter(
        (item) =>
          item.requires_manual_completion &&
          activeSelectedTargetMap.get(item.id)?.revision === item.revision
      ),
    [activeSelectedTargetMap, reconciliationItems]
  )
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
      setBatchFailures([])
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

  const zeroSettlementMutation = useMutation({
    mutationKey: ['smart-ops', 'manual-task-billing-zero-batch'],
    mutationFn: async (
      targets: BillingSettlementReviewTarget[]
    ): Promise<ManualTaskBillingBatchCompletionData | undefined> => {
      const response = await completeManualTaskBillingSettlementsZero({
        items: targets,
      })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to complete manual task billing.')
        )
      }
      return response.data
    },
    onSuccess: (data) => {
      setSelectedTargets(new Map())
      const completed = data?.completed_count ?? 0
      const failed = data?.failed_count ?? 0
      const failures = data?.failed ?? []
      setBatchFailures(failures)
      toast.success(
        failed > 0
          ? t(
              'Completed {{completed}} selected task settlements; {{failed}} remain for review.',
              { completed, failed }
            )
          : t(
              'Completed selected task settlements with zero final quota: {{count}}.',
              { count: completed }
            )
      )
      if (failures.length > 0) {
        toast.warning(
          t('Some settlements remain for review: {{ids}}', {
            ids: failures
              .map((failure) => `#${failure.settlement_id}`)
              .join(', '),
          })
        )
      }
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

  const manualTaskBatchCompletionMutation = useMutation({
    mutationKey: ['smart-ops', 'manual-task-billing-exact-batch'],
    mutationFn: async ({
      items,
      actualQuotas,
    }: {
      items: BillingSettlementReconciliationItem[]
      actualQuotas: Record<number, number>
    }): Promise<ManualTaskBillingBatchCompletionData | undefined> => {
      const response = await completeManualTaskBillingSettlements({
        items: items.map((item) => ({
          id: item.id,
          revision: item.revision,
          actual_quota: actualQuotas[item.id],
        })),
      })
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to complete manual task billing.')
        )
      }
      return response.data
    },
    onSuccess: (data) => {
      setManualTaskBatchItems([])
      setSelectedTargets(new Map())
      const completed = data?.completed_count ?? 0
      const failed = data?.failed_count ?? 0
      const failures = data?.failed ?? []
      setBatchFailures(failures)
      toast.success(
        failed > 0
          ? t(
              'Completed {{completed}} selected task settlements; {{failed}} remain for review.',
              { completed, failed }
            )
          : t('Completed selected task settlements: {{count}}.', {
              count: completed,
            })
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
          t('Failed to complete manual task billing.')
        ),
      })
    },
  })

  const reviewTargets = (targets: BillingSettlementReviewTarget[]): void => {
    if (
      targets.length === 0 ||
      reviewMutation.isPending ||
      zeroSettlementMutation.isPending ||
      manualTaskCompletionMutation.isPending ||
      manualTaskBatchCompletionMutation.isPending
    )
      return
    const selectedItems = (reconciliationItems ?? []).filter((item) =>
      targets.some(
        (target) => target.id === item.id && target.revision === item.revision
      )
    )
    switch (classifyBillingSettlementReviewSelection(selectedItems)) {
      case 'exact_quota_required':
        toast.error(
          t('Some selected task settlements still require an exact quota.')
        )
        return
      case 'exact_quota':
        setManualTaskBatchItems(selectedItems)
        return
      case 'mixed':
        toast.error(
          t('Select either task settlements or ordinary alerts, not both.')
        )
        return
      case 'zero_quota':
        zeroSettlementMutation.mutate(targets)
        return
      case 'ordinary':
        reviewMutation.mutate(targets)
        return
      default:
        return
    }
  }

  const manualTaskCompletionMutation = useMutation({
    mutationKey: ['smart-ops', 'manual-task-billing-completion'],
    mutationFn: async ({
      item,
      actualQuota,
    }: {
      item: BillingSettlementReconciliationItem
      actualQuota: number
    }): Promise<void> => {
      const response = await completeManualTaskBillingSettlement(item.id, {
        revision: item.revision,
        actual_quota: actualQuota,
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
      const message = mutationErrorMessage(
        error,
        t('Failed to complete manual task billing.')
      )
      handleServerError(error, {
        fallback: message,
      })
    },
  })

  const manualTaskDialog = manualTaskItem ? (
    <ManualTaskSettlementDialog
      key={`${manualTaskItem.id}:${manualTaskItem.revision}`}
      item={manualTaskItem}
      pending={manualTaskCompletionMutation.isPending}
      stale={manualTaskItemStale}
      onOpenChange={(open) => {
        if (!open) setManualTaskItem(null)
      }}
      onSubmit={(item, actualQuota) =>
        manualTaskCompletionMutation.mutate({ item, actualQuota })
      }
    />
  ) : null

  const manualTaskBatchDialog =
    manualTaskBatchItems.length > 0 ? (
      <ManualTaskSettlementBatchDialog
        key={manualTaskBatchItems
          .map((item) => `${item.id}:${item.revision}`)
          .join(',')}
        items={manualTaskBatchItems}
        pending={manualTaskBatchCompletionMutation.isPending}
        stale={manualTaskBatchStale}
        onOpenChange={(open) => {
          if (!open) setManualTaskBatchItems([])
        }}
        onSubmit={(items, actualQuotas) =>
          manualTaskBatchCompletionMutation.mutate({ items, actualQuotas })
        }
      />
    ) : null

  const replaceSelectedTargets = (
    targets: Map<number, BillingSettlementReviewTarget>
  ): void => {
    setSelectedTargets(targets)
  }

  let content: ReactElement
  if (props.loading) {
    content = (
      <div className='flex flex-col gap-2 border-t pt-4'>
        <Skeleton className='h-16 w-full rounded-md' />
        <Skeleton className='h-40 w-full rounded-md' />
      </div>
    )
  } else if (props.error) {
    content = (
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
  } else if (!props.data) {
    content = <></>
  } else {
    content = (
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
                'Batch-close ordinary alerts after review. Root administrators can select MiniMax-H3 task-finalization alerts for an explicit zero-quota settlement; other task records still require an exact quota.'
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
            disabled={
              policyMutation.isPending || !props.canUpdateBlockingPolicy
            }
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
                  count: formatCount(
                    activeSelectedTargets.length,
                    i18n.language
                  ),
                })}
              </span>
              <Button
                type='button'
                size='sm'
                onClick={() => reviewTargets(activeSelectedTargets)}
                disabled={
                  activeSelectedTargets.length === 0 ||
                  reviewMutation.isPending ||
                  zeroSettlementMutation.isPending ||
                  manualTaskCompletionMutation.isPending ||
                  manualTaskBatchCompletionMutation.isPending
                }
              >
                {(reviewMutation.isPending ||
                  zeroSettlementMutation.isPending) && (
                  <Loader2
                    data-icon='inline-start'
                    className='animate-spin'
                    aria-hidden='true'
                  />
                )}
                {t('Review and close selected ({{count}})', {
                  count: formatCount(
                    activeSelectedTargets.length,
                    i18n.language
                  ),
                })}
              </Button>
              {selectedManualItems.length > 0 &&
                selectedManualItems.length === activeSelectedTargets.length && (
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setManualTaskBatchItems(selectedManualItems)}
                    disabled={
                      reviewMutation.isPending ||
                      zeroSettlementMutation.isPending ||
                      manualTaskCompletionMutation.isPending ||
                      manualTaskBatchCompletionMutation.isPending
                    }
                  >
                    {t('Enter exact quotas ({{count}})', {
                      count: formatCount(
                        activeSelectedTargets.length,
                        i18n.language
                      ),
                    })}
                  </Button>
                )}
            </div>
            {batchFailures.length > 0 && (
              <Alert variant='destructive'>
                <TriangleAlert aria-hidden='true' />
                <AlertTitle>
                  {t('Some task settlements still need review.')}
                </AlertTitle>
                <AlertDescription>
                  <ul className='list-disc space-y-1 pl-4'>
                    {batchFailures.map((failure) => (
                      <li key={`${failure.settlement_id}:${failure.message}`}>
                        {t('Settlement #{{id}}: {{message}}', {
                          id: failure.settlement_id,
                          message: formatBatchFailureMessage(failure, t),
                        })}
                      </li>
                    ))}
                  </ul>
                </AlertDescription>
              </Alert>
            )}
            <BillingSettlementTable
              items={props.data.items}
              canCompleteManualTask={props.canCompleteManualTask}
              selectedTargets={activeSelectedTargetMap}
              reviewPending={
                reviewMutation.isPending ||
                zeroSettlementMutation.isPending ||
                manualTaskCompletionMutation.isPending ||
                manualTaskBatchCompletionMutation.isPending
              }
              onSelectedTargetsChange={replaceSelectedTargets}
              onReviewTargets={reviewTargets}
              onCompleteManualTask={setManualTaskItem}
            />
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

  return (
    <>
      {content}
      {manualTaskDialog}
      {manualTaskBatchDialog}
    </>
  )
}
