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
import type { TFunction } from 'i18next'
import { CheckCircle2, Pencil, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  formatQuota,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { updateBillingSettlementBlockingPolicy } from '../api'
import { formatCount, formatLocalizedCount } from '../lib/format'
import { mutationErrorMessage } from '../lib/mutation-error'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
} from '../types'
import { SettlementReviewDialog } from './settlement-review-dialog'

const FUNDING_SOURCE_LABEL: Record<string, string> = {
  wallet: 'Wallet',
  subscription: 'Subscription',
}

function settlementReferences(
  item: BillingSettlementReconciliationItem,
  t: TFunction
): string[] {
  const references = [t('User #{{id}}', { id: item.user_id })]
  if (item.subscription_id > 0) {
    references.push(t('Subscription #{{id}}', { id: item.subscription_id }))
  }
  if (item.token_id > 0) {
    references.push(t('Token #{{id}}', { id: item.token_id }))
  }
  if (item.task_id > 0) {
    references.push(t('Task #{{id}}', { id: item.task_id }))
  }
  return references
}

interface BillingSettlementEvidenceProps {
  canUpdateBlockingPolicy: boolean
  data?: BillingSettlementReconciliationData
  error: Error | null
  loading: boolean
  onRetry: () => void
  onChanged: () => Promise<void>
}

export function BillingSettlementEvidence(
  props: BillingSettlementEvidenceProps
): ReactElement {
  const { t, i18n } = useTranslation()
  const [selectedItem, setSelectedItem] =
    useState<BillingSettlementReconciliationItem | null>(null)
  const [policyOverride, setPolicyOverride] = useState<boolean | null>(null)
  const [policySaving, setPolicySaving] = useState(false)
  const policyValue =
    policyOverride ?? props.data?.block_user_by_default ?? true

  const handlePolicyChange = async (checked: boolean) => {
    setPolicyOverride(checked)
    setPolicySaving(true)
    try {
      const response = await updateBillingSettlementBlockingPolicy(checked)
      if (!response.success) {
        throw new Error(
          response.message || t('Failed to update blocking policy.')
        )
      }
      toast.success(t('Default user-blocking policy updated.'))
      await props.onChanged()
    } catch (error) {
      handleServerError(error, {
        fallback: mutationErrorMessage(
          error,
          t('Failed to update blocking policy.')
        ),
      })
    } finally {
      setPolicyOverride(null)
      setPolicySaving(false)
    }
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
              'Reviewed: {{count}}'
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
          onCheckedChange={(checked) => void handlePolicyChange(checked)}
          disabled={policySaving || !props.canUpdateBlockingPolicy}
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
        <div className='overflow-x-auto rounded-md border'>
          <Table className='min-w-[1580px]'>
            <TableHeader>
              <TableRow className='bg-muted/40 hover:bg-muted/40'>
                <TableHead>{t('Operation')}</TableHead>
                <TableHead>{t('Financial / alert status')}</TableHead>
                <TableHead>{t('User access')}</TableHead>
                <TableHead>{t('References')}</TableHead>
                <TableHead>{t('Funding source')}</TableHead>
                <TableHead className='text-right'>
                  {t('Outstanding funding')}
                </TableHead>
                <TableHead>{t('Attempts / next retry')}</TableHead>
                <TableHead>{t('Last error')}</TableHead>
                <TableHead>{t('Administrator review')}</TableHead>
                <TableHead>{t('Created')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {props.data.items.map((item) => {
                const outstandingFunding =
                  item.funding_delta - item.applied_funding_delta
                const references = settlementReferences(item, t)
                return (
                  <TableRow key={item.id}>
                    <TableCell className='max-w-72 whitespace-normal'>
                      <span className='font-mono text-xs break-all'>
                        {item.operation_key}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div className='flex flex-col items-start gap-1'>
                        <Badge
                          variant={
                            item.status === 'manual' ? 'destructive' : 'outline'
                          }
                        >
                          {item.status === 'manual'
                            ? t('Manual')
                            : t('Pending')}
                        </Badge>
                        <Badge
                          variant={
                            item.reconciliation_reviewed_at > 0
                              ? 'secondary'
                              : 'destructive'
                          }
                        >
                          {item.reconciliation_reviewed_at > 0
                            ? t('Reviewed')
                            : t('Open alert')}
                        </Badge>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex flex-col items-start gap-1'>
                        <Badge
                          variant={
                            item.blocks_user ? 'destructive' : 'secondary'
                          }
                        >
                          {item.blocks_user
                            ? t('User blocked')
                            : t('User allowed')}
                        </Badge>
                        <span className='text-muted-foreground text-xs'>
                          {item.record_blocks_user
                            ? t('Record policy: block')
                            : t('Record policy: allow')}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className='whitespace-normal'>
                      <div className='flex max-w-48 flex-col gap-0.5 font-mono text-xs'>
                        {references.map((reference) => (
                          <span key={reference}>{reference}</span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      {t(FUNDING_SOURCE_LABEL[item.source] ?? item.source)}
                    </TableCell>
                    <TableCell className='text-right font-mono tabular-nums'>
                      {formatQuota(outstandingFunding)}
                    </TableCell>
                    <TableCell className='whitespace-normal'>
                      <div className='flex max-w-44 flex-col gap-0.5 text-xs'>
                        <span>
                          {formatLocalizedCount(
                            item.attempts,
                            i18n.language,
                            t,
                            '{{count}} attempt',
                            '{{count}} attempts'
                          )}
                        </span>
                        <span
                          className='text-muted-foreground'
                          title={formatTimestampToDate(item.next_attempt)}
                        >
                          {item.status === 'manual'
                            ? t('Manual review')
                            : formatTimestampRelative(
                                item.next_attempt,
                                'seconds',
                                i18n.language
                              )}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className='max-w-72 whitespace-normal'>
                      <span
                        className='text-muted-foreground text-xs break-words'
                        title={item.last_error || undefined}
                      >
                        {item.last_error || t('No error detail')}
                      </span>
                    </TableCell>
                    <TableCell className='max-w-72 whitespace-normal'>
                      {item.reconciliation_reviewed_at > 0 ? (
                        <div className='flex flex-col gap-0.5 text-xs'>
                          <span>
                            {t('Administrator #{{id}}', {
                              id: item.reconciliation_reviewed_by,
                            })}
                          </span>
                          <span
                            className='text-muted-foreground'
                            title={formatTimestampToDate(
                              item.reconciliation_reviewed_at
                            )}
                          >
                            {formatTimestampRelative(
                              item.reconciliation_reviewed_at,
                              'seconds',
                              i18n.language
                            )}
                          </span>
                          <span
                            className='text-muted-foreground line-clamp-3'
                            title={item.reconciliation_review_note}
                          >
                            {item.reconciliation_review_note}
                          </span>
                        </div>
                      ) : (
                        <span className='text-muted-foreground text-xs'>
                          {t('Not reviewed')}
                        </span>
                      )}
                    </TableCell>
                    <TableCell
                      className='text-muted-foreground text-xs'
                      title={formatTimestampToDate(item.created_at)}
                    >
                      {formatTimestampRelative(
                        item.created_at,
                        'seconds',
                        i18n.language
                      )}
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => setSelectedItem(item)}
                      >
                        {item.reconciliation_reviewed_at > 0 && (
                          <Pencil data-icon='inline-start' aria-hidden='true' />
                        )}
                        {item.reconciliation_reviewed_at > 0
                          ? t('Edit review')
                          : t('Review and close')}
                      </Button>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </div>
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
          onReviewed={props.onChanged}
        />
      )}
    </section>
  )
}
