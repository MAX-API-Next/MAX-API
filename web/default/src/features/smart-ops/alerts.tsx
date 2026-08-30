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
import { useEffect, useState, type ReactElement } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import {
  CheckCircle2,
  Loader2,
  LockKeyhole,
  LockOpen,
  Pencil,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  formatQuota,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
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
import { Textarea } from '@/components/ui/textarea'
import {
  ToggleGroup,
  ToggleGroupItem,
} from '@/components/ui/toggle-group'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import {
  getBillingSettlementReconciliation,
  getSmartOpsAlerts,
  reviewBillingSettlement,
  updateBillingSettlementBlockingPolicy,
} from './api'
import type {
  BillingSettlementReconciliationData,
  BillingSettlementReconciliationItem,
  SmartOpsAlert,
} from './types'

const ALERT_POLL_INTERVAL_MS = 30000
const BILLING_BACKLOG_ALERT_KEY = 'billing_settlement_backlog'

const RESOURCE_LABEL: Record<string, string> = {
  system_cpu: 'CPU usage',
  system_memory: 'Memory usage',
  system_disk: 'Disk usage',
  [BILLING_BACKLOG_ALERT_KEY]: 'Billing reconciliation backlog',
}

const FUNDING_SOURCE_LABEL: Record<string, string> = {
  wallet: 'Wallet',
  subscription: 'Subscription',
}

function formatPercent(value: number, locale: string): string {
  return `${new Intl.NumberFormat(locale, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(value)}%`
}

function formatCount(value: number, locale: string): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(
    value
  )
}

function formatDurationSeconds(
  value: number,
  locale: string,
  t: TFunction
): string {
  const totalSeconds = Math.max(0, Math.floor(value))
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const units: Array<[number, string]> = [
    [days, '{{count}} days'],
    [hours, '{{count}} hours'],
    [minutes, '{{count}} minutes'],
    [seconds, '{{count}} seconds'],
  ]
  const parts = units
    .filter(([count]) => count > 0)
    .slice(0, 2)
    .map(([count, key]) =>
      t(key, {
        count: formatCount(count, locale),
        interpolation: { escapeValue: false },
      })
    )
  return parts.length > 0
    ? parts.join(' ')
    : t('{{count}} seconds', { count: 0 })
}

function getObservedAtMilliseconds(alert: SmartOpsAlert): number | undefined {
  const timestamp = Date.parse(alert.observed_at)
  return Number.isNaN(timestamp) ? undefined : timestamp
}

function isBillingBacklogAlert(alert: SmartOpsAlert): boolean {
  return alert.key === BILLING_BACKLOG_ALERT_KEY
}

function formatAlertCurrentValue(
  alert: SmartOpsAlert,
  locale: string,
  t: TFunction
): string {
  if (isBillingBacklogAlert(alert)) {
    return t('{{count}} records', {
      count: formatCount(alert.current_value, locale),
      interpolation: { escapeValue: false },
    })
  }
  return formatPercent(alert.current_value, locale)
}

function formatAlertThreshold(
  alert: SmartOpsAlert,
  locale: string,
  t: TFunction
): string {
  if (isBillingBacklogAlert(alert)) {
    return formatDurationSeconds(alert.threshold, locale, t)
  }
  return formatPercent(alert.threshold, locale)
}

interface ActiveAlertsTableProps {
  alerts: SmartOpsAlert[]
}

function ActiveAlertsTable(props: ActiveAlertsTableProps): ReactElement {
  const { t, i18n } = useTranslation()

  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table className='min-w-[820px]'>
        <TableHeader>
          <TableRow className='bg-muted/40 hover:bg-muted/40'>
            <TableHead className='h-9 px-4 text-xs'>{t('Resource')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Status')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Node')}</TableHead>
            <TableHead className='h-9 text-right text-xs'>
              {t('Current Value')}
            </TableHead>
            <TableHead className='h-9 text-right text-xs'>
              {t('Threshold / Oldest wait')}
            </TableHead>
            <TableHead className='h-9 pr-4 text-right text-xs'>
              {t('Observed')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.alerts.map((alert) => {
            const observedAt = getObservedAtMilliseconds(alert)
            return (
              <TableRow key={`${alert.node ?? 'unknown'}:${alert.key}`}>
                <TableCell className='px-4 py-3 whitespace-normal'>
                  <div className='flex max-w-md flex-col gap-0.5'>
                    <span className='text-sm font-medium'>
                      {t(RESOURCE_LABEL[alert.key] ?? alert.key)}
                    </span>
                    <span className='text-muted-foreground font-mono text-[11px]'>
                      {alert.key}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='py-3'>
                  <Badge variant='destructive'>{t('Firing')}</Badge>
                </TableCell>
                <TableCell className='text-muted-foreground py-3 font-mono text-xs'>
                  {alert.node || t('Unknown')}
                </TableCell>
                <TableCell className='py-3 text-right font-mono text-sm tabular-nums'>
                  {formatAlertCurrentValue(alert, i18n.language, t)}
                </TableCell>
                <TableCell className='py-3 text-right font-mono text-sm tabular-nums'>
                  {formatAlertThreshold(alert, i18n.language, t)}
                </TableCell>
                <TableCell
                  className='text-muted-foreground py-3 pr-4 text-right text-xs whitespace-nowrap'
                  title={formatTimestampToDate(observedAt, 'milliseconds')}
                >
                  {formatTimestampRelative(
                    observedAt,
                    'milliseconds',
                    i18n.language
                  )}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
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
  data?: BillingSettlementReconciliationData
  error: Error | null
  loading: boolean
  onRetry: () => void
  onChanged: () => Promise<void>
}

interface SettlementReviewDialogProps {
  item: BillingSettlementReconciliationItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onReviewed: () => Promise<void>
}

function mutationErrorMessage(error: unknown, fallback: string): string {
  if (
    typeof error === 'object' &&
    error !== null &&
    'response' in error &&
    typeof error.response === 'object' &&
    error.response !== null &&
    'data' in error.response &&
    typeof error.response.data === 'object' &&
    error.response.data !== null &&
    'message' in error.response.data &&
    typeof error.response.data.message === 'string'
  ) {
    return error.response.data.message
  }
  return error instanceof Error ? error.message : fallback
}

function SettlementReviewDialog(
  props: SettlementReviewDialogProps
): ReactElement {
  const { t } = useTranslation()
  const [decision, setDecision] = useState<'block' | 'allow'>('block')
  const [note, setNote] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!props.open || !props.item) return
    setDecision(props.item.blocks_user ? 'block' : 'allow')
    setNote(props.item.reconciliation_review_note || '')
  }, [props.item, props.open])

  const noteLength = note.trim().length
  const noteInvalid = noteLength > 0 && (noteLength < 3 || noteLength > 1000)

  const handleSubmit = async () => {
    if (!props.item || noteLength < 3 || noteLength > 1000) return
    setSaving(true)
    try {
      const response = await reviewBillingSettlement(props.item.id, {
        block_user: decision === 'block',
        note: note.trim(),
      })
      if (!response.success) {
        throw new Error(response.message || t('Failed to save review.'))
      }
      toast.success(t('Billing reconciliation alert closed.'))
      props.onOpenChange(false)
      await props.onReviewed()
    } catch (error) {
      toast.error(mutationErrorMessage(error, t('Failed to save review.')))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.item?.reconciliation_reviewed_at
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
              maxLength={1000}
              aria-invalid={noteInvalid || undefined}
              placeholder={t(
                'Record the evidence checked, the conclusion, and any follow-up action.'
              )}
            />
            <FieldDescription>
              {t('{{count}} / 1000 characters', { count: noteLength })}
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
            {props.item?.reconciliation_reviewed_at
              ? t('Save review')
              : t('Close alert')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function BillingSettlementEvidence(
  props: BillingSettlementEvidenceProps
): ReactElement {
  const { t, i18n } = useTranslation()
  const [selectedItem, setSelectedItem] =
    useState<BillingSettlementReconciliationItem | null>(null)
  const [policyValue, setPolicyValue] = useState(
    props.data?.block_user_by_default ?? true
  )
  const [policySaving, setPolicySaving] = useState(false)

  useEffect(() => {
    if (props.data) setPolicyValue(props.data.block_user_by_default)
  }, [props.data])

  const handlePolicyChange = async (checked: boolean) => {
    const previous = policyValue
    setPolicyValue(checked)
    setPolicySaving(true)
    try {
      const response = await updateBillingSettlementBlockingPolicy(checked)
      if (!response.success) {
        throw new Error(response.message || t('Failed to update blocking policy.'))
      }
      toast.success(t('Default user-blocking policy updated.'))
      await props.onChanged()
    } catch (error) {
      setPolicyValue(previous)
      toast.error(
        mutationErrorMessage(error, t('Failed to update blocking policy.'))
      )
    } finally {
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
            {t('Open alerts: {{count}}', {
              count: formatCount(props.data.open_alert_count, i18n.language),
            })}
          </Badge>
          <Badge variant='outline'>
            {t('Reviewed: {{count}}', {
              count: formatCount(props.data.reviewed_count, i18n.language),
            })}
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
            {t('Blocked users: {{count}}', {
              count: formatCount(props.data.blocked_user_count, i18n.language),
            })}
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
        </FieldContent>
        <Switch
          id='billing-reconciliation-block-user-default'
          checked={policyValue}
          onCheckedChange={(checked) => void handlePolicyChange(checked)}
          disabled={policySaving}
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
                            item.status === 'manual'
                              ? 'destructive'
                              : 'outline'
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
                      <Badge
                        variant={item.blocks_user ? 'destructive' : 'secondary'}
                      >
                        {item.blocks_user
                          ? t('User blocked')
                          : t('User allowed')}
                      </Badge>
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
                          {t('{{count}} attempts', { count: item.attempts })}
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
      <SettlementReviewDialog
        item={selectedItem}
        open={selectedItem !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedItem(null)
        }}
        onReviewed={props.onChanged}
      />
    </section>
  )
}

export function ActiveAlerts(): ReactElement {
  const { t } = useTranslation()
  const loadErrorMessage = t('We could not load active alerts.')
  const reconciliationErrorMessage = t(
    'We could not load billing reconciliation details.'
  )
  const alertsQuery = useQuery({
    queryKey: ['smart-ops', 'active-alerts', loadErrorMessage],
    queryFn: async (): Promise<SmartOpsAlert[]> => {
      const response = await getSmartOpsAlerts()
      if (!response.success || !Array.isArray(response.data)) {
        throw new Error(response.message || loadErrorMessage)
      }
      return response.data
    },
    retry: false,
    refetchInterval: ALERT_POLL_INTERVAL_MS,
  })

  const alerts = alertsQuery.data ?? []
  const reconciliationQuery = useQuery({
    queryKey: [
      'smart-ops',
      'billing-settlement-reconciliation',
      reconciliationErrorMessage,
    ],
    queryFn: async (): Promise<BillingSettlementReconciliationData> => {
      const response = await getBillingSettlementReconciliation()
      if (!response.success || !response.data) {
        throw new Error(response.message || reconciliationErrorMessage)
      }
      return response.data
    },
    retry: false,
    refetchInterval: ALERT_POLL_INTERVAL_MS,
  })

  const loading = alertsQuery.isLoading
  const refreshing =
    (alertsQuery.isFetching && !loading) || reconciliationQuery.isFetching

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>{t('Active Alerts')}</span>
          <Badge variant='outline' className='shrink-0'>
            {t('Administrator controls')}
          </Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <section className='bg-card overflow-hidden rounded-lg border shadow-xs'>
          <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
            <div className='flex min-w-0 items-center gap-2'>
              <span className='bg-muted text-muted-foreground inline-flex size-7 items-center justify-center rounded-md'>
                <TriangleAlert className='size-4' aria-hidden='true' />
              </span>
              <div className='min-w-0'>
                <div className='flex items-center gap-2'>
                  <h3 className='text-sm font-semibold'>
                    {t('Alerts requiring administrator attention')}
                  </h3>
                  <Badge
                    variant={alerts.length > 0 ? 'destructive' : 'outline'}
                  >
                    {alerts.length}
                  </Badge>
                </div>
                <p className='text-muted-foreground mt-0.5 text-xs'>
                  {t(
                    'SmartOps reports sustained host pressure and billing reconciliation backlogs here for manual investigation.'
                  )}
                </p>
              </div>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => {
                void alertsQuery.refetch()
                void reconciliationQuery.refetch()
              }}
              disabled={
                alertsQuery.isFetching || reconciliationQuery.isFetching
              }
            >
              <RefreshCw
                data-icon='inline-start'
                className={cn(refreshing && 'animate-spin')}
                aria-hidden='true'
              />
              {refreshing ? t('Refreshing...') : t('Refresh')}
            </Button>
          </div>

          <div className='flex flex-col gap-4 p-4 sm:p-5'>
            {loading ? (
              <div className='flex flex-col gap-2'>
              {Array.from({ length: 3 }).map((_, index) => (
                <Skeleton key={index} className='h-10 w-full rounded-md' />
              ))}
              </div>
            ) : alertsQuery.isError ? (
              <ErrorState
                title={loadErrorMessage}
                description={
                  alertsQuery.error instanceof Error
                    ? alertsQuery.error.message
                    : undefined
                }
                onRetry={() => void alertsQuery.refetch()}
                className='min-h-[200px]'
              />
            ) : alerts.length === 0 ? (
              <Empty className='min-h-[200px] rounded-md border'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <ShieldCheck aria-hidden='true' />
                  </EmptyMedia>
                  <EmptyTitle>{t('No active alerts.')}</EmptyTitle>
                  <EmptyDescription>
                    {t(
                      'This process is not currently reporting sustained host pressure or an open billing reconciliation alert.'
                    )}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ActiveAlertsTable alerts={alerts} />
            )}
            <BillingSettlementEvidence
              data={reconciliationQuery.data}
              error={
                reconciliationQuery.error instanceof Error
                  ? reconciliationQuery.error
                  : null
              }
              loading={reconciliationQuery.isLoading}
              onRetry={() => void reconciliationQuery.refetch()}
              onChanged={async () => {
                await Promise.all([
                  alertsQuery.refetch(),
                  reconciliationQuery.refetch(),
                ])
              }}
            />
          </div>
        </section>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
