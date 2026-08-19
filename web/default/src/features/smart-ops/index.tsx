/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Clock3,
  ChevronsUpDown,
  DatabaseZap,
  HeartPulse,
  RefreshCw,
  Search,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import {
  formatLatency,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import { getChannelPerformance, getChannelPerformanceDetail } from './api'
import { getChannelDetailObservation } from './lib/channel-detail'
import {
  DEFAULT_FILTERS,
  MAX_PERFORMANCE_HOURS,
  normalizeFilters,
  toQuery,
} from './lib/filters'
import { formatLegacyLatency } from './lib/format'
import {
  sortChannelPerformanceItems,
  type ChannelPerformanceSortKey,
  type ChannelPerformanceSortState,
} from './lib/sort'
import type { ChannelPerformanceData, ChannelPerformanceItem } from './types'

const priorityQualityFlags = [
  'error_logs_disabled',
  'consume_logs_disabled',
  'retry_backfill_pending',
  'retry_metrics_unavailable',
  'time_range_clamped',
  'truncated',
  'incomplete_errors_possible',
]

function getVisibleQualityFlags(flags: string[]) {
  const flagSet = new Set(flags)
  const priorityFlags = priorityQualityFlags.filter((flag) => flagSet.has(flag))
  const otherFlags = flags.filter(
    (flag) => !priorityQualityFlags.includes(flag)
  )
  return [
    ...priorityFlags,
    ...otherFlags.slice(0, Math.max(0, 4 - priorityFlags.length)),
  ]
}

function statusBadge(
  status: number | null,
  enabledLabel: string,
  disabledLabel: string
) {
  if (status == null) return <Badge variant='outline'>—</Badge>
  return status === 1 ? (
    <Badge variant='secondary'>{enabledLabel}</Badge>
  ) : (
    <Badge variant='destructive'>{disabledLabel}</Badge>
  )
}

export function ProductionPerformance() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState(DEFAULT_FILTERS)
  const [filters, setFilters] = useState(DEFAULT_FILTERS)
  const [selected, setSelected] = useState<ChannelPerformanceItem | null>(null)
  const [sortState, setSortState] = useState<ChannelPerformanceSortState>(null)
  const queryParams = useMemo(() => toQuery(filters), [filters])
  const numberFormatter = useMemo(
    () => new Intl.NumberFormat(i18n.language),
    [i18n.language]
  )

  const performanceQuery = useMutation({
    mutationKey: ['smart-ops', 'channel-performance'],
    mutationFn: getChannelPerformance,
    retry: false,
  })
  const detailQuery = useMutation({
    mutationKey: ['smart-ops', 'channel-performance-detail'],
    mutationFn: getChannelPerformanceDetail,
    retry: false,
  })

  const data = performanceQuery.data
  const selectedDetailObservation = selected
    ? getChannelDetailObservation(selected, detailQuery.data)
    : null
  const sortedItems = useMemo(
    () => sortChannelPerformanceItems(data?.items ?? [], sortState),
    [data?.items, sortState]
  )
  const latestObservedAt = data?.summary.last_observed_at ?? 0
  const retryBackfillPending =
    data?.quality_flags.includes('retry_backfill_pending') ?? false
  const retryMetricsUnavailable =
    data?.quality_flags.includes('retry_metrics_unavailable') ?? false
  const errorLogsDisabled =
    data?.quality_flags.includes('error_logs_disabled') ?? false

  const openDetails = (item: ChannelPerformanceItem) => {
    detailQuery.reset()
    setSelected(item)
    detailQuery.mutate(item.channel_id)
  }

  const formatTimestamp = (timestamp: number | null | undefined) => {
    if (!timestamp) return '—'
    return new Intl.DateTimeFormat(i18n.language, {
      dateStyle: 'medium',
      timeStyle: 'medium',
    }).format(new Date(timestamp * 1000))
  }

  const qualityLabel = (flag: string) => {
    const labels: Record<string, string> = {
      legacy: t('Legacy log'),
      partial: t('Partial data'),
      weak_correlation: t('Weak correlation'),
      heuristic_outcome: t('Heuristic outcome'),
      coarse_latency: t('Coarse latency'),
      probe_separate: t('Probe data is separate'),
      non_attempt_logs_excluded: t('Known non-attempt logs excluded'),
      time_range_clamped: t('Time range was limited'),
      truncated: t('Results truncated'),
      retry_backfill_pending: t('Retry marker backfill pending'),
      retry_metrics_unavailable: t('Retry metrics unavailable'),
      consume_logs_disabled: t('Consumption logs disabled'),
      error_logs_disabled: t('Error logs disabled'),
      incomplete_errors_possible: t('Errors may be incomplete'),
      metadata_missing: t('Channel metadata missing'),
      probe_unavailable: t('Probe unavailable'),
    }
    return labels[flag] ?? flag
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>{t('Channel performance')}</span>
          <Badge variant='outline'>{t('Read only')}</Badge>
          <Badge variant='secondary'>{t('Legacy log')}</Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          onClick={() => performanceQuery.mutate(queryParams)}
          disabled={performanceQuery.isPending}
        >
          <RefreshCw
            data-icon='inline-start'
            className={performanceQuery.isPending ? 'animate-spin' : undefined}
            aria-hidden='true'
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <Alert className='border-warning/40 bg-warning/5'>
            <AlertTriangle className='text-warning' aria-hidden='true' />
            <AlertTitle>{t('Log fallback mode')}</AlertTitle>
            <AlertDescription>
              {t(
                'This legacy view estimates outcomes from eligible Consume and Error logs. Known billing adjustments and channel probes are excluded, but it is not a complete Relay Attempt trace. Logged latency uses only non-retry samples and remains accurate only to whole seconds.'
              )}
            </AlertDescription>
          </Alert>

          {(retryBackfillPending || retryMetricsUnavailable) && (
            <Alert>
              <AlertTriangle aria-hidden='true' />
              <AlertTitle>
                {t('Retry and latency data are temporarily unavailable')}
              </AlertTitle>
              <AlertDescription>
                {retryBackfillPending
                  ? t(
                      'Historical retry markers are still being prepared. Request counts and estimated success rates remain available; retry counts and logged latency will appear after the backfill completes.'
                    )
                  : t(
                      'The retry marker readiness state could not be verified. Request counts and estimated success rates remain available, but retry counts and logged latency are unavailable; check the application and log databases.'
                    )}
              </AlertDescription>
            </Alert>
          )}

          {errorLogsDisabled && (
            <Alert className='border-warning/40 bg-warning/5'>
              <AlertTriangle className='text-warning' aria-hidden='true' />
              <AlertTitle>{t('Error log collection is disabled')}</AlertTitle>
              <AlertDescription>
                {t(
                  'The displayed error count includes only recorded logs and may be incomplete. Estimated success rate is unavailable while error log collection is disabled.'
                )}
              </AlertDescription>
            </Alert>
          )}

          <form
            className='bg-card rounded-xl border p-3'
            onSubmit={(event) => {
              event.preventDefault()
              const normalizedDraft = normalizeFilters(draft)
              const nextQuery = toQuery(normalizedDraft)
              setDraft(normalizedDraft)
              setFilters(normalizedDraft)
              performanceQuery.mutate(nextQuery)
            }}
          >
            <FieldGroup className='grid gap-3 sm:grid-cols-2 lg:grid-cols-[8rem_10rem_minmax(10rem,1fr)_minmax(10rem,1fr)_auto] lg:items-end'>
              <Field className='gap-1.5'>
                <FieldLabel htmlFor='smart-ops-hours' className='text-xs'>
                  {t('Time range')} ({t('hours')}, 1–{MAX_PERFORMANCE_HOURS})
                </FieldLabel>
                <Input
                  id='smart-ops-hours'
                  inputMode='numeric'
                  min={1}
                  max={MAX_PERFORMANCE_HOURS}
                  step={1}
                  type='number'
                  value={draft.hours}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      hours: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field className='gap-1.5'>
                <FieldLabel htmlFor='smart-ops-channel-id' className='text-xs'>
                  {t('Channel ID')}
                </FieldLabel>
                <Input
                  id='smart-ops-channel-id'
                  inputMode='numeric'
                  min={1}
                  type='number'
                  value={draft.channelId}
                  placeholder={t('All channels')}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      channelId: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field className='gap-1.5'>
                <FieldLabel htmlFor='smart-ops-model' className='text-xs'>
                  {t('Model')}
                </FieldLabel>
                <Input
                  id='smart-ops-model'
                  value={draft.model}
                  placeholder={t('All models')}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      model: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field className='gap-1.5'>
                <FieldLabel htmlFor='smart-ops-group' className='text-xs'>
                  {t('Group')}
                </FieldLabel>
                <Input
                  id='smart-ops-group'
                  value={draft.group}
                  placeholder={t('All Groups')}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      group: event.target.value,
                    }))
                  }
                />
              </Field>
              <Button
                type='submit'
                size='sm'
                className='self-end'
                disabled={performanceQuery.isPending}
              >
                <Search data-icon='inline-start' aria-hidden='true' />
                {t('Apply filters')}
              </Button>
            </FieldGroup>
          </form>

          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-5'>
            <MetricCard
              icon={DatabaseZap}
              label={t('Observed log events')}
              value={
                data ? numberFormatter.format(data.summary.observed_count) : '—'
              }
              loading={performanceQuery.isPending}
            />
            <MetricCard
              icon={ShieldCheck}
              label={t('Consume logs')}
              value={
                data
                  ? numberFormatter.format(data.summary.consume_log_count)
                  : '—'
              }
              loading={performanceQuery.isPending}
            />
            <MetricCard
              icon={AlertTriangle}
              label={t('Recorded error logs')}
              value={
                data
                  ? numberFormatter.format(data.summary.error_log_count)
                  : '—'
              }
              loading={performanceQuery.isPending}
            />
            <MetricCard
              icon={HeartPulse}
              label={t('Estimated success rate')}
              value={
                data
                  ? data.summary.observed_success_rate == null
                    ? t('N/A')
                    : formatUptimePct(data.summary.observed_success_rate)
                  : '—'
              }
              loading={performanceQuery.isPending}
            />
            <MetricCard
              icon={Clock3}
              label={t('Last observed')}
              value={formatTimestamp(latestObservedAt)}
              loading={performanceQuery.isPending}
            />
          </div>

          {performanceQuery.isPending ? (
            <PerformanceTableSkeleton />
          ) : performanceQuery.isError ? (
            <ErrorState
              title={t('Unable to load channel performance')}
              description={t(
                'Check log database availability and administrator permissions, then try again.'
              )}
              onRetry={() => performanceQuery.mutate(queryParams)}
            />
          ) : performanceQuery.isIdle ? (
            <Empty className='min-h-72 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <DatabaseZap aria-hidden='true' />
                </EmptyMedia>
                <EmptyTitle>
                  {t('Channel performance is not loaded automatically')}
                </EmptyTitle>
                <EmptyDescription>
                  {t(
                    'Select Apply filters or Refresh to query the current time range.'
                  )}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : !data?.items.length ? (
            <Empty className='min-h-72 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <DatabaseZap aria-hidden='true' />
                </EmptyMedia>
                <EmptyTitle>{t('No recorded channel logs')}</EmptyTitle>
                <EmptyDescription>
                  {t(
                    'Try a wider time range or check whether consumption and error logging are enabled.'
                  )}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className='bg-card overflow-hidden rounded-xl border'>
              <div className='flex flex-wrap items-center gap-2 border-b px-3 py-2.5'>
                <span className='text-sm font-semibold'>
                  {t('Channel performance')}
                </span>
                <span className='text-muted-foreground text-xs'>
                  {t('{{count}} rows', { count: data.items.length })}
                </span>
                <div className='ml-auto flex flex-wrap gap-1'>
                  {getVisibleQualityFlags(data.quality_flags).map((flag) => (
                    <Badge key={flag} variant='outline'>
                      {qualityLabel(flag)}
                    </Badge>
                  ))}
                </div>
              </div>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Channel')}</TableHead>
                    <TableHead>{t('Model')}</TableHead>
                    <TableHead>{t('Group')}</TableHead>
                    <SortablePerformanceHead
                      sortKey='observed_count'
                      title={t('Observed log events')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortablePerformanceHead
                      sortKey='error_log_count'
                      title={t('Recorded error logs')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortablePerformanceHead
                      sortKey='consumed_quota'
                      title={t('Consumed quota')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortablePerformanceHead
                      sortKey='observed_success_rate'
                      title={t('Estimated success rate')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortablePerformanceHead
                      sortKey='avg_logged_latency_ms'
                      title={t('Logged latency')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortablePerformanceHead
                      sortKey='retry_log_count'
                      title={t('Retries')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortablePerformanceHead
                      sortKey='probe_latency_ms'
                      title={t('Probe latency')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <TableHead>{t('Last observed')}</TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sortedItems.map((item) => (
                    <TableRow
                      key={`${item.channel_id}:${item.model_name}:${item.effective_group}`}
                      className='cursor-pointer'
                      onClick={() => openDetails(item)}
                    >
                      <TableCell>
                        <div className='flex min-w-48 items-center gap-2'>
                          <div className='min-w-0'>
                            <div className='truncate font-medium'>
                              {item.channel_name}
                            </div>
                            <div className='text-muted-foreground text-xs'>
                              #{item.channel_id}
                            </div>
                          </div>
                          {statusBadge(
                            item.channel_status,
                            t('Enabled'),
                            t('Disabled')
                          )}
                        </div>
                      </TableCell>
                      <TableCell className='max-w-64 truncate font-mono text-xs'>
                        {item.model_name}
                      </TableCell>
                      <TableCell>{item.effective_group || '—'}</TableCell>
                      <TableCell className='text-right'>
                        {numberFormatter.format(item.observed_count)}
                      </TableCell>
                      <TableCell className='text-right'>
                        {numberFormatter.format(item.error_log_count)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {formatQuota(item.consumed_quota)}
                      </TableCell>
                      <TableCell className='text-right'>
                        {item.observed_success_rate == null
                          ? t('N/A')
                          : formatUptimePct(item.observed_success_rate)}
                      </TableCell>
                      <TableCell className='text-right'>
                        {formatLegacyLatency(item.avg_logged_latency_ms)}
                      </TableCell>
                      <TableCell className='text-right'>
                        {item.retry_log_count == null
                          ? t('N/A')
                          : numberFormatter.format(item.retry_log_count)}
                      </TableCell>
                      <TableCell className='text-right'>
                        {formatLatency(item.probe_latency_ms ?? Number.NaN)}
                      </TableCell>
                      <TableCell>
                        {formatTimestamp(item.last_observed_at)}
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          variant='ghost'
                          size='xs'
                          aria-label={`${t('View details')}: ${item.channel_name} · ${item.model_name}`}
                          onClick={(event) => {
                            event.stopPropagation()
                            openDetails(item)
                          }}
                        >
                          {t('Details')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {data.truncated && (
                <div className='text-warning border-t px-3 py-2 text-xs'>
                  {t(
                    'The table was truncated to {{count}} rows. Summary cards still cover all matching eligible logs.',
                    { count: data.items.length }
                  )}
                </div>
              )}
            </div>
          )}

          {data && (
            <div className='text-muted-foreground text-right text-xs'>
              {t('Generated at {{time}}', {
                time: formatTimestamp(data.generated_at),
              })}
            </div>
          )}
          <Sheet
            open={selected != null}
            onOpenChange={(open) => {
              if (!open) {
                setSelected(null)
                detailQuery.reset()
              }
            }}
          >
            <SheetContent className='w-full sm:max-w-3xl lg:max-w-4xl'>
              <SheetHeader className='border-b'>
                <SheetTitle>{t('Channel performance details')}</SheetTitle>
                <SheetDescription>
                  {selected
                    ? `${selected.channel_name} · #${selected.channel_id}`
                    : ''}
                </SheetDescription>
              </SheetHeader>
              {selected && (
                <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 pb-4'>
                  <DetailGrid
                    rows={[
                      [t('Channel ID'), String(selected.channel_id)],
                      [
                        t('Channel type'),
                        selected.channel_type == null
                          ? '—'
                          : String(selected.channel_type),
                      ],
                      [
                        t('Probe latency'),
                        formatLatency(
                          selectedDetailObservation?.probeLatencyMs ??
                            Number.NaN
                        ),
                      ],
                      [
                        t('Last observed'),
                        formatTimestamp(
                          selectedDetailObservation?.lastObservedAt
                        ),
                      ],
                    ]}
                  />
                  <div>
                    <div className='mb-2 text-sm font-medium'>
                      {t('Data quality')}
                    </div>
                    <div className='flex flex-wrap gap-1.5'>
                      {selectedDetailObservation?.qualityFlags.length ? (
                        selectedDetailObservation.qualityFlags.map((flag) => (
                          <Badge key={flag} variant='outline'>
                            {qualityLabel(flag)}
                          </Badge>
                        ))
                      ) : (
                        <Badge variant='outline'>{t('N/A')}</Badge>
                      )}
                    </div>
                  </div>
                  <Alert>
                    <AlertTriangle aria-hidden='true' />
                    <AlertTitle>{t('Interpret with caution')}</AlertTitle>
                    <AlertDescription>
                      {t(
                        'Probe latency is shown for comparison only and is not included in production log latency or success-rate calculations.'
                      )}
                    </AlertDescription>
                  </Alert>
                  {detailQuery.isPending ? (
                    <ChannelPerformanceDetailSkeleton />
                  ) : detailQuery.isError ? (
                    <ErrorState
                      className='min-h-64 border'
                      title={t('Unable to load channel performance details')}
                      description={t(
                        'Check log database availability and administrator permissions, then try again.'
                      )}
                      onRetry={() => detailQuery.mutate(selected.channel_id)}
                    />
                  ) : detailQuery.data ? (
                    <ChannelPerformanceDetail
                      data={detailQuery.data}
                      numberFormatter={numberFormatter}
                      formatTimestamp={formatTimestamp}
                    />
                  ) : null}
                </div>
              )}
            </SheetContent>
          </Sheet>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function SortablePerformanceHead({
  sortKey,
  title,
  sortState,
  onSort,
}: {
  sortKey: ChannelPerformanceSortKey
  title: string
  sortState: ChannelPerformanceSortState
  onSort: (sortState: ChannelPerformanceSortState) => void
}) {
  const { t } = useTranslation()
  const direction = sortState?.key === sortKey ? sortState.direction : null
  const ariaSort =
    direction === 'asc'
      ? 'ascending'
      : direction === 'desc'
        ? 'descending'
        : 'none'

  return (
    <TableHead className='text-right' aria-sort={ariaSort}>
      <div className='flex justify-end'>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant='ghost'
                size='sm'
                className='data-popup-open:bg-accent -me-2 h-8'
              />
            }
          >
            <span>{title}</span>
            {direction === 'desc' ? (
              <ArrowDown data-icon='inline-end' aria-hidden='true' />
            ) : direction === 'asc' ? (
              <ArrowUp data-icon='inline-end' aria-hidden='true' />
            ) : (
              <ChevronsUpDown data-icon='inline-end' aria-hidden='true' />
            )}
          </DropdownMenuTrigger>
          <DropdownMenuContent align='end'>
            <DropdownMenuGroup>
              <DropdownMenuItem
                onClick={() => onSort({ key: sortKey, direction: 'asc' })}
              >
                <ArrowUp aria-hidden='true' />
                {t('Asc')}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => onSort({ key: sortKey, direction: 'desc' })}
              >
                <ArrowDown aria-hidden='true' />
                {t('Desc')}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </TableHead>
  )
}

function MetricCard({
  icon: Icon,
  label,
  value,
  loading,
}: {
  icon: typeof DatabaseZap
  label: string
  value: string
  loading: boolean
}) {
  return (
    <Card size='sm'>
      <CardHeader>
        <CardDescription className='truncate'>{label}</CardDescription>
        <CardTitle className='truncate tabular-nums'>
          {loading ? <Skeleton className='h-5 w-20' /> : value}
        </CardTitle>
        <CardAction>
          <div className='bg-muted text-muted-foreground flex size-9 items-center justify-center rounded-lg'>
            <Icon className='size-4' aria-hidden='true' />
          </div>
        </CardAction>
      </CardHeader>
    </Card>
  )
}

function PerformanceTableSkeleton() {
  return (
    <div
      className='bg-card overflow-hidden rounded-xl border'
      aria-hidden='true'
    >
      <div className='border-b px-3 py-3'>
        <Skeleton className='h-4 w-40' />
      </div>
      <div className='space-y-2 p-3'>
        {Array.from({ length: 6 }).map((_, index) => (
          <Skeleton key={index} className='h-10 w-full' />
        ))}
      </div>
    </div>
  )
}

function ChannelPerformanceDetail({
  data,
  numberFormatter,
  formatTimestamp,
}: {
  data: ChannelPerformanceData
  numberFormatter: Intl.NumberFormat
  formatTimestamp: (timestamp: number | null | undefined) => string
}) {
  const { t } = useTranslation()
  const summary = data.summary

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <div className='text-sm font-semibold'>{t('Last 24 hours')}</div>
          <div className='text-muted-foreground text-xs'>
            {t('Channel models and groups from eligible production logs')}
          </div>
        </div>
        <Badge variant='outline'>
          {t('{{count}} rows', { count: data.items.length })}
        </Badge>
      </div>
      <DetailGrid
        rows={[
          [
            t('Observed log events'),
            numberFormatter.format(summary.observed_count),
          ],
          [
            t('Recorded error logs'),
            numberFormatter.format(summary.error_log_count),
          ],
          [
            t('Estimated success rate'),
            summary.observed_success_rate == null
              ? t('N/A')
              : formatUptimePct(summary.observed_success_rate),
          ],
          [t('Consumed quota'), formatQuota(summary.consumed_quota)],
          [
            t('Retries'),
            summary.retry_log_count == null
              ? t('N/A')
              : numberFormatter.format(summary.retry_log_count),
          ],
          [
            t('Logged latency'),
            formatLegacyLatency(summary.avg_logged_latency_ms),
          ],
          [t('Last observed'), formatTimestamp(summary.last_observed_at)],
        ]}
      />
      {data.items.length === 0 ? (
        <Empty className='min-h-56 border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <DatabaseZap aria-hidden='true' />
            </EmptyMedia>
            <EmptyTitle>{t('No channel model performance')}</EmptyTitle>
            <EmptyDescription>
              {t(
                'No eligible production logs were recorded for this channel in the last 24 hours.'
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='overflow-x-auto rounded-xl border'>
          <Table className='min-w-[980px]'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>{t('Group')}</TableHead>
                <TableHead className='text-right'>
                  {t('Observed log events')}
                </TableHead>
                <TableHead className='text-right'>
                  {t('Recorded error logs')}
                </TableHead>
                <TableHead className='text-right'>
                  {t('Estimated success rate')}
                </TableHead>
                <TableHead className='text-right'>
                  {t('Consumed quota')}
                </TableHead>
                <TableHead className='text-right'>{t('Retries')}</TableHead>
                <TableHead className='text-right'>
                  {t('Logged latency')}
                </TableHead>
                <TableHead>{t('Last observed')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((item) => (
                <TableRow
                  key={`${item.channel_id}:${item.model_name}:${item.effective_group}`}
                >
                  <TableCell className='max-w-64 font-mono text-xs break-all'>
                    {item.model_name}
                  </TableCell>
                  <TableCell>{item.effective_group || '—'}</TableCell>
                  <TableCell className='text-right'>
                    {numberFormatter.format(item.observed_count)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {numberFormatter.format(item.error_log_count)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {item.observed_success_rate == null
                      ? t('N/A')
                      : formatUptimePct(item.observed_success_rate)}
                  </TableCell>
                  <TableCell className='text-right tabular-nums'>
                    {formatQuota(item.consumed_quota)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {item.retry_log_count == null
                      ? t('N/A')
                      : numberFormatter.format(item.retry_log_count)}
                  </TableCell>
                  <TableCell className='text-right'>
                    {formatLegacyLatency(item.avg_logged_latency_ms)}
                  </TableCell>
                  <TableCell>
                    {formatTimestamp(item.last_observed_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {data.truncated && (
        <div className='text-warning text-xs'>
          {t(
            'The detail table was truncated to {{count}} rows. The summary still covers all matching eligible logs.',
            { count: data.items.length }
          )}
        </div>
      )}
    </div>
  )
}

function ChannelPerformanceDetailSkeleton() {
  return (
    <div className='space-y-3' aria-label='Loading channel performance details'>
      <Skeleton className='h-5 w-44' />
      <div className='grid gap-2 sm:grid-cols-2'>
        {Array.from({ length: 6 }).map((_, index) => (
          <Skeleton key={index} className='h-16 w-full' />
        ))}
      </div>
      <Skeleton className='h-56 w-full' />
    </div>
  )
}

function DetailGrid({ rows }: { rows: [string, string][] }) {
  return (
    <dl className='bg-border grid shrink-0 grid-cols-1 gap-px overflow-hidden rounded-xl border sm:grid-cols-2'>
      {rows.map(([label, value]) => (
        <div key={label} className='bg-background px-3 py-2.5'>
          <dt className='text-muted-foreground text-xs'>{label}</dt>
          <dd className='mt-1 text-sm font-medium break-all tabular-nums'>
            {value}
          </dd>
        </div>
      ))}
    </dl>
  )
}
