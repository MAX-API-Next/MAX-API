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
import type { TFunction } from 'i18next'
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Bot,
  ChevronsUpDown,
  Clock3,
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
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import type { PerformanceCoverage } from '@/features/performance-metrics/types'
import { ModelPerformanceDetailsContent } from '@/features/pricing/components/model-details-performance'
import { getModelPerformance, getModelPerformanceDetail } from './api'
import {
  DEFAULT_MODEL_FILTERS,
  MAX_PERFORMANCE_HOURS,
  normalizeFilters,
  toModelQuery,
  type ModelFilterDraft,
} from './lib/filters'
import { formatLegacyLatency } from './lib/format'
import {
  sortModelPerformanceItems,
  type ModelPerformanceSortKey,
  type ModelPerformanceSortState,
} from './lib/model-sort'
import type { ModelPerformanceItem } from './types'

const priorityQualityFlags = [
  'error_logs_disabled',
  'consume_logs_disabled',
  'retry_backfill_pending',
  'retry_metrics_unavailable',
  'throughput_collection_disabled',
  'throughput_no_samples',
  'throughput_query_failed',
  'throughput_window_approximate',
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

export function ModelPerformance() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<ModelFilterDraft>(DEFAULT_MODEL_FILTERS)
  const [filters, setFilters] = useState<ModelFilterDraft>(
    DEFAULT_MODEL_FILTERS
  )
  const [selected, setSelected] = useState<ModelPerformanceItem | null>(null)
  const [sortState, setSortState] = useState<ModelPerformanceSortState>(null)
  const queryParams = useMemo(() => toModelQuery(filters), [filters])
  const numberFormatter = useMemo(
    () => new Intl.NumberFormat(i18n.language),
    [i18n.language]
  )

  const performanceQuery = useMutation({
    mutationKey: ['smart-ops', 'model-performance'],
    mutationFn: getModelPerformance,
    retry: false,
  })
  const detailQuery = useMutation({
    mutationKey: ['smart-ops', 'model-performance-detail'],
    mutationFn: getModelPerformanceDetail,
    retry: false,
  })

  const data = performanceQuery.data
  const sortedItems = useMemo(
    () =>
      sortModelPerformanceItems(data?.items ?? [], sortState, i18n.language),
    [data?.items, i18n.language, sortState]
  )
  const latestObservedAt = data?.summary.last_observed_at ?? 0
  const retryBackfillPending =
    data?.quality_flags.includes('retry_backfill_pending') ?? false
  const retryMetricsUnavailable =
    data?.quality_flags.includes('retry_metrics_unavailable') ?? false
  const errorLogsDisabled =
    data?.quality_flags.includes('error_logs_disabled') ?? false
  const throughputCollectionDisabled =
    data?.quality_flags.includes('throughput_collection_disabled') ?? false
  const throughputNoSamples =
    data?.quality_flags.includes('throughput_no_samples') ?? false
  const throughputQueryFailed =
    data?.quality_flags.includes('throughput_query_failed') ?? false
  const throughputPartial =
    data?.quality_flags.includes('throughput_partial') ?? false
  const throughputWindowApproximate =
    data?.quality_flags.includes('throughput_window_approximate') ?? false

  const openDetails = (item: ModelPerformanceItem) => {
    detailQuery.reset()
    setSelected(item)
    detailQuery.mutate(item.model_name)
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
      non_attempt_logs_excluded: t('Known non-attempt logs excluded'),
      time_range_clamped: t('Time range was limited'),
      truncated: t('Results truncated'),
      retry_backfill_pending: t('Retry marker backfill pending'),
      retry_metrics_unavailable: t('Retry metrics unavailable'),
      consume_logs_disabled: t('Consumption logs disabled'),
      error_logs_disabled: t('Error logs disabled'),
      incomplete_errors_possible: t('Errors may be incomplete'),
      throughput_collection_disabled: t(
        'Performance metrics collection is disabled'
      ),
      throughput_no_samples: t('No throughput samples'),
      throughput_query_failed: t('Throughput query failed'),
      throughput_window_approximate: t('Bucket-level estimate'),
    }
    return labels[flag] ?? flag
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>{t('Model performance')}</span>
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
                'This legacy view aggregates estimated model outcomes from eligible Consume and Error logs. It is not a complete Relay Attempt trace, and logged latency remains accurate only to whole seconds.'
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

          {throughputCollectionDisabled && (
            <Alert className='border-warning/40 bg-warning/5'>
              <AlertTriangle className='text-warning' aria-hidden='true' />
              <AlertTitle>
                {t('Performance metrics collection is disabled')}
              </AlertTitle>
              <AlertDescription>
                {t(
                  'New performance samples are not being collected. Existing performance data may be incomplete.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {throughputNoSamples && (
            <Alert className='border-warning/40 bg-warning/5'>
              <AlertTriangle className='text-warning' aria-hidden='true' />
              <AlertTitle>{t('No throughput samples')}</AlertTitle>
              <AlertDescription>
                {t('No performance samples were recorded for this time range.')}
              </AlertDescription>
            </Alert>
          )}

          {throughputQueryFailed && (
            <Alert className='border-destructive/40 bg-destructive/5'>
              <AlertTriangle className='text-destructive' aria-hidden='true' />
              <AlertTitle>{t('Throughput query failed')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Performance metrics could not be queried. Log-based model results remain available; check the application database.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {throughputPartial && (
            <Alert className='border-warning/40 bg-warning/5'>
              <AlertTriangle className='text-warning' aria-hidden='true' />
              <AlertTitle>{t('Throughput data is partial')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Stored and local performance samples are shown, but shared active buckets could not be read. Recent multi-node throughput may be incomplete.'
                )}
              </AlertDescription>
            </Alert>
          )}

          {throughputWindowApproximate && data?.throughput?.coverage && (
            <Alert>
              <Clock3 aria-hidden='true' />
              <AlertTitle>{t('Bucket-level estimate')}</AlertTitle>
              <AlertDescription>
                {formatCoverageDescription(
                  t,
                  data.throughput.coverage,
                  formatTimestamp
                )}
              </AlertDescription>
            </Alert>
          )}

          <form
            className='bg-card rounded-xl border p-3'
            onSubmit={(event) => {
              event.preventDefault()
              const normalizedDraft = normalizeFilters(draft)
              const nextQuery = toModelQuery(normalizedDraft)
              setDraft(normalizedDraft)
              setFilters(normalizedDraft)
              performanceQuery.mutate(nextQuery)
            }}
          >
            <FieldGroup className='grid gap-3 sm:grid-cols-2 lg:grid-cols-[8rem_minmax(10rem,1fr)_minmax(10rem,1fr)_auto] lg:items-end'>
              <Field className='gap-1.5'>
                <FieldLabel htmlFor='smart-ops-model-hours' className='text-xs'>
                  {t('Time range')} ({t('hours')}, 1–{MAX_PERFORMANCE_HOURS})
                </FieldLabel>
                <Input
                  id='smart-ops-model-hours'
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
                <FieldLabel htmlFor='smart-ops-model-name' className='text-xs'>
                  {t('Model')}
                </FieldLabel>
                <Input
                  id='smart-ops-model-name'
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
                <FieldLabel htmlFor='smart-ops-model-group' className='text-xs'>
                  {t('Group')}
                </FieldLabel>
                <Input
                  id='smart-ops-model-group'
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

          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-6'>
            <MetricCard
              icon={Bot}
              label={t('Models')}
              value={
                data ? numberFormatter.format(data.summary.model_count) : '—'
              }
              loading={performanceQuery.isPending}
            />
            <MetricCard
              icon={DatabaseZap}
              label={t('Observed model log events')}
              value={
                data ? numberFormatter.format(data.summary.observed_count) : '—'
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
              icon={ShieldCheck}
              label={t('Channels')}
              value={
                data ? numberFormatter.format(data.summary.channel_count) : '—'
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
              title={t('Unable to load model performance')}
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
                  {t('Model performance is not loaded automatically')}
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
                <EmptyTitle>{t('No recorded model logs')}</EmptyTitle>
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
                  {t('Model performance')}
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
                    <SortableModelPerformanceHead
                      sortKey='model_name'
                      title={t('Model')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='channel_count'
                      title={t('Channels')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='observed_count'
                      title={t('Observed model log events')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='error_log_count'
                      title={t('Recorded error logs')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='consumed_quota'
                      title={t('Consumed quota')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='observed_success_rate'
                      title={t('Estimated success rate')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='avg_logged_latency_ms'
                      title={t('Logged latency')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='avg_tps'
                      title={t('Throughput')}
                      sortState={sortState}
                      onSort={setSortState}
                    />
                    <SortableModelPerformanceHead
                      sortKey='retry_log_count'
                      title={t('Retries')}
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
                      key={item.model_name}
                      className='cursor-pointer'
                      onClick={() => openDetails(item)}
                    >
                      <TableCell className='max-w-72 truncate font-mono text-xs'>
                        {item.model_name}
                      </TableCell>
                      <TableCell className='text-right'>
                        {numberFormatter.format(item.channel_count)}
                      </TableCell>
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
                      <TableCell className='text-right tabular-nums'>
                        {formatThroughput(item.avg_tps ?? Number.NaN)}
                      </TableCell>
                      <TableCell className='text-right'>
                        {item.retry_log_count == null
                          ? t('N/A')
                          : numberFormatter.format(item.retry_log_count)}
                      </TableCell>
                      <TableCell>
                        {formatTimestamp(item.last_observed_at)}
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          variant='ghost'
                          size='xs'
                          aria-label={`${t('View details')}: ${item.model_name}`}
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
                <SheetTitle>{t('Model performance details')}</SheetTitle>
                <SheetDescription>
                  {selected?.model_name ?? ''}
                </SheetDescription>
              </SheetHeader>
              {selected && (
                <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-auto px-4 pb-4'>
                  <ModelPerformanceDetailGrid
                    rows={[
                      [t('Model'), selected.model_name],
                      [
                        t('Channels'),
                        numberFormatter.format(selected.channel_count),
                      ],
                      [
                        t('Observed model log events'),
                        numberFormatter.format(selected.observed_count),
                      ],
                      [
                        t('Consume logs'),
                        numberFormatter.format(selected.consume_log_count),
                      ],
                      [
                        t('Recorded error logs'),
                        numberFormatter.format(selected.error_log_count),
                      ],
                      [
                        t('Consumed quota'),
                        formatQuota(selected.consumed_quota),
                      ],
                      [
                        t('Estimated success rate'),
                        selected.observed_success_rate == null
                          ? t('N/A')
                          : formatUptimePct(selected.observed_success_rate),
                      ],
                      [
                        t('Retries'),
                        selected.retry_log_count == null
                          ? t('N/A')
                          : numberFormatter.format(selected.retry_log_count),
                      ],
                      [
                        t('Latency samples'),
                        numberFormatter.format(selected.latency_sample_count),
                      ],
                      [
                        t('Logged latency'),
                        formatLegacyLatency(selected.avg_logged_latency_ms),
                      ],
                      [
                        t('Throughput'),
                        formatThroughput(selected.avg_tps ?? Number.NaN),
                      ],
                      [
                        t('Last observed'),
                        formatTimestamp(selected.last_observed_at),
                      ],
                    ]}
                  />
                  <div>
                    <div className='mb-2 text-sm font-medium'>
                      {t('Data quality')}
                    </div>
                    <div className='flex flex-wrap gap-1.5'>
                      {selected.quality_flags.map((flag) => (
                        <Badge key={flag} variant='outline'>
                          {qualityLabel(flag)}
                        </Badge>
                      ))}
                    </div>
                  </div>
                  <Alert>
                    <AlertTriangle aria-hidden='true' />
                    <AlertTitle>{t('Interpret with caution')}</AlertTitle>
                    <AlertDescription>
                      {t(
                        'Model results are aggregated from eligible logs and are not a complete Relay Attempt trace.'
                      )}
                    </AlertDescription>
                  </Alert>
                  {detailQuery.isPending ? (
                    <ModelPerformanceDetailSkeleton />
                  ) : detailQuery.isError ? (
                    <ErrorState
                      className='min-h-64 border'
                      title={t('Unable to load model performance details')}
                      description={t(
                        'Check performance metrics collection and database availability, then try again.'
                      )}
                      onRetry={() => detailQuery.mutate(selected.model_name)}
                    />
                  ) : detailQuery.data ? (
                    <>
                      {detailQuery.data.collection_state ===
                        'collection_disabled' && (
                        <Alert className='border-warning/40 bg-warning/5'>
                          <AlertTriangle
                            className='text-warning'
                            aria-hidden='true'
                          />
                          <AlertTitle>
                            {t('Performance metrics collection is disabled')}
                          </AlertTitle>
                          <AlertDescription>
                            {t(
                              'New performance samples are not being collected. Existing performance data may be incomplete.'
                            )}
                          </AlertDescription>
                        </Alert>
                      )}
                      {detailQuery.data.collection_state === 'no_samples' && (
                        <Alert className='border-warning/40 bg-warning/5'>
                          <AlertTriangle
                            className='text-warning'
                            aria-hidden='true'
                          />
                          <AlertTitle>{t('No throughput samples')}</AlertTitle>
                          <AlertDescription>
                            {t(
                              'No performance samples were recorded for this time range.'
                            )}
                          </AlertDescription>
                        </Alert>
                      )}
                      {detailQuery.data.collection_state === 'partial' && (
                        <Alert className='border-warning/40 bg-warning/5'>
                          <AlertTriangle
                            className='text-warning'
                            aria-hidden='true'
                          />
                          <AlertTitle>
                            {t('Throughput data is partial')}
                          </AlertTitle>
                          <AlertDescription>
                            {t(
                              'Stored and local performance samples are shown, but shared active buckets could not be read. Recent multi-node throughput may be incomplete.'
                            )}
                          </AlertDescription>
                        </Alert>
                      )}
                      {detailQuery.data.coverage?.approximate && (
                        <Alert>
                          <Clock3 aria-hidden='true' />
                          <AlertTitle>{t('Bucket-level estimate')}</AlertTitle>
                          <AlertDescription>
                            {formatCoverageDescription(
                              t,
                              detailQuery.data.coverage,
                              formatTimestamp
                            )}
                          </AlertDescription>
                        </Alert>
                      )}
                      <ModelPerformanceDetailsContent
                        groups={detailQuery.data.groups}
                        summary={detailQuery.data.summary}
                      />
                    </>
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

function SortableModelPerformanceHead({
  sortKey,
  title,
  sortState,
  onSort,
}: {
  sortKey: ModelPerformanceSortKey
  title: string
  sortState: ModelPerformanceSortState
  onSort: (sortState: ModelPerformanceSortState) => void
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
    <TableHead
      className={sortKey === 'model_name' ? '' : 'text-right'}
      aria-sort={ariaSort}
    >
      <div className={sortKey === 'model_name' ? 'flex' : 'flex justify-end'}>
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

function ModelPerformanceDetailSkeleton() {
  return (
    <div className='flex flex-col gap-4' aria-hidden='true'>
      <div className='grid grid-cols-1 gap-2 sm:grid-cols-3'>
        {Array.from({ length: 3 }).map((_, index) => (
          <Skeleton key={index} className='h-24 w-full' />
        ))}
      </div>
      <Skeleton className='h-48 w-full' />
      <Skeleton className='h-64 w-full' />
      <Skeleton className='h-56 w-full' />
    </div>
  )
}

export function ModelPerformanceDetailGrid({
  rows,
}: {
  rows: [string, string][]
}) {
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

function formatCoverageDescription(
  t: TFunction,
  coverage: PerformanceCoverage,
  formatTimestamp: (timestamp: number | null | undefined) => string
) {
  const values = {
    start: formatTimestamp(coverage.bucket_start_at),
    end: formatTimestamp(coverage.bucket_end_at),
  }
  if (coverage.granularity_state === 'known' && coverage.bucket_seconds > 0) {
    return t(
      'Throughput coverage: {{start}} – {{end}} ({{minutes}} minute buckets).',
      {
        ...values,
        minutes: Math.max(1, Math.round(coverage.bucket_seconds / 60)),
      }
    )
  }
  return t(
    'Throughput coverage: {{start}} – {{end}}. Bucket granularity is unknown or mixed, so this range is approximate.',
    values
  )
}
