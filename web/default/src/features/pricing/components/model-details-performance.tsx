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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, HeartPulse, Timer } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { GroupBadge } from '@/components/group-badge'
import { getPerfMetrics } from '@/features/performance-metrics/api'
import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
} from '@/features/performance-metrics/lib/format'
import type {
  PerformanceAggregate,
  PerformanceGroup,
  PerformanceSeriesPoint,
} from '@/features/performance-metrics/types'
import { type UptimeDayPoint } from '../lib/mock-stats'
import type { PricingModel } from '../types'
import { LatencyTrendChart, UptimeTrendChart } from './model-details-charts'
import { UptimeSparkline } from './model-details-uptime-sparkline'

function StatCard(props: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: React.ReactNode
  hint?: string
  intent?: 'default' | 'warning' | 'success'
}) {
  const Icon = props.icon
  const intent = props.intent ?? 'default'
  return (
    <div className='bg-background flex flex-col gap-1 rounded-lg border p-3'>
      <span className='text-muted-foreground inline-flex items-center gap-1.5 text-[10px] font-medium tracking-wider uppercase'>
        <Icon className='size-3' />
        {props.label}
      </span>
      <span
        className={cn(
          'text-foreground font-mono text-lg font-semibold tabular-nums',
          intent === 'warning' && 'text-amber-600 dark:text-amber-400',
          intent === 'success' && 'text-emerald-600 dark:text-emerald-400'
        )}
      >
        {props.value}
      </span>
      {props.hint && (
        <span className='text-muted-foreground/70 text-[11px]'>
          {props.hint}
        </span>
      )}
    </div>
  )
}

type PerformanceRow = {
  group: string
  avg_ttft_ms: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
}

function toLatencySeriesFromAggregate(summary?: PerformanceAggregate) {
  if (!summary) return []
  return summary.series
    .filter((point) => point.avg_ttft_ms > 0)
    .map((point) => ({
      timestamp: new Date(point.ts * 1000).toISOString(),
      group: 'model',
      ttft_ms: point.avg_ttft_ms,
    }))
}

function toUptimeSeries(
  series: PerformanceSeriesPoint[] | undefined
): UptimeDayPoint[] {
  if (!series) return []
  return series.map((point) => ({
    date: new Date(point.ts * 1000).toISOString(),
    uptime_pct: Math.round(point.success_rate * 100) / 100,
    incidents: point.success_rate < 100 ? 1 : 0,
    outage_minutes: 0,
  }))
}

export function ModelDetailsPerformance(props: { model: PricingModel }) {
  const metricsQuery = useQuery({
    queryKey: ['perf-metrics', props.model.model_name],
    queryFn: () => getPerfMetrics(props.model.model_name, 24),
    staleTime: 60 * 1000,
  })
  const groups = useMemo(
    () => metricsQuery.data?.data.groups ?? [],
    [metricsQuery.data]
  )
  const summary = metricsQuery.data?.data.summary

  return (
    <ModelPerformanceDetailsContent
      groups={groups}
      summary={summary}
      loading={metricsQuery.isLoading}
    />
  )
}

export function ModelPerformanceDetailsContent(props: {
  groups: PerformanceGroup[]
  summary?: PerformanceAggregate
  loading?: boolean
}) {
  const { t } = useTranslation()
  const groups = props.groups
  const performances = useMemo<PerformanceRow[]>(
    () =>
      groups.map((group) => ({
        group: group.group,
        avg_ttft_ms: group.avg_ttft_ms,
        avg_latency_ms: group.avg_latency_ms,
        success_rate: group.success_rate,
        avg_tps: group.avg_tps,
      })),
    [groups]
  )
  const aggregate = useMemo<PerformanceAggregate | undefined>(() => {
    if (props.summary) return props.summary
    if (groups.length !== 1) return undefined
    const group = groups[0]
    return {
      avg_ttft_ms: group.avg_ttft_ms,
      avg_latency_ms: group.avg_latency_ms,
      success_rate: group.success_rate,
      avg_tps: group.avg_tps,
      series: group.series,
    }
  }, [groups, props.summary])
  const latencySeries = useMemo(
    () => (aggregate ? toLatencySeriesFromAggregate(aggregate) : []),
    [aggregate]
  )
  const uptimeSeries = useMemo(
    () => toUptimeSeries(aggregate?.series),
    [aggregate]
  )
  const uptimeByGroup = useMemo<Record<string, UptimeDayPoint[]>>(() => {
    const map: Record<string, UptimeDayPoint[]> = {}
    for (const group of groups) {
      map[group.group] = toUptimeSeries(group.series)
    }
    return map
  }, [groups])

  if (props.loading || performances.length === 0) {
    return (
      <div className='text-muted-foreground rounded-lg border p-6 text-center text-sm'>
        {t('Performance data is not yet available for this model.')}
      </div>
    )
  }

  const aggregateAvailable = aggregate != null
  const avgTps = aggregate?.avg_tps ?? Number.NaN
  const avgLatency = aggregate?.avg_latency_ms ?? Number.NaN
  const successRate = aggregate?.success_rate ?? Number.NaN
  const incidentCount = uptimeSeries.reduce((s, p) => s + p.incidents, 0)
  let intent: 'default' | 'warning' | 'success' = 'default'
  if (aggregateAvailable) {
    if (successRate >= 99.9) {
      intent = 'success'
    } else if (successRate < 99) {
      intent = 'warning'
    }
  }
  let successRateHint = t('N/A')
  if (aggregateAvailable) {
    successRateHint =
      incidentCount > 0
        ? t('{{count}} incidents in the last 24 hours', {
            count: incidentCount,
          })
        : t('No incidents in the last 24 hours')
  }

  const headerCellClass =
    'text-muted-foreground py-2 text-[10px] font-medium tracking-wider uppercase'

  return (
    <div className='flex flex-col gap-4'>
      <div className='grid grid-cols-1 gap-2 sm:grid-cols-3'>
        <StatCard
          icon={Timer}
          label='TPS'
          value={formatThroughput(avgTps)}
          hint={t('Sustained tokens per second')}
        />
        <StatCard
          icon={Timer}
          label={t('Average latency')}
          value={formatLatency(avgLatency)}
        />
        <StatCard
          icon={HeartPulse}
          label={t('Success rate')}
          value={formatUptimePct(successRate)}
          hint={successRateHint}
          intent={intent}
        />
      </div>

      <section>
        <SectionHeader
          icon={HeartPulse}
          title={t('Per-group performance')}
          description={t('Average latency, TTFT, TPS, and success rate')}
        />
        <div className='overflow-x-auto rounded-lg border'>
          <Table className='text-sm'>
            <TableHeader>
              <TableRow className='hover:bg-transparent'>
                <TableHead className={headerCellClass}>{t('Group')}</TableHead>
                <TableHead className={`${headerCellClass} text-right`}>
                  TPS
                </TableHead>
                <TableHead className={`${headerCellClass} text-right`}>
                  {t('Average TTFT')}
                </TableHead>
                <TableHead className={`${headerCellClass} text-right`}>
                  {t('Average latency')}
                </TableHead>
                <TableHead
                  className={`${headerCellClass} min-w-[180px] text-left`}
                >
                  {t('Success rate')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {performances.map((perf) => (
                <TableRow key={perf.group}>
                  <TableCell className='py-2.5'>
                    <GroupBadge group={perf.group} size='sm' />
                  </TableCell>
                  <TableCell className='py-2.5 text-right font-mono'>
                    {formatThroughput(perf.avg_tps)}
                  </TableCell>
                  <TableCell className='py-2.5 text-right font-mono'>
                    {formatLatency(perf.avg_ttft_ms)}
                  </TableCell>
                  <TableCell className='text-muted-foreground py-2.5 text-right font-mono'>
                    {formatLatency(perf.avg_latency_ms)}
                  </TableCell>
                  <TableCell className='py-2.5'>
                    <UptimeSparkline
                      size='sm'
                      series={uptimeByGroup[perf.group] ?? []}
                      overallPct={perf.success_rate}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </section>

      <section>
        <SectionHeader
          icon={Timer}
          title={t('Latency trend (last 24h)')}
          description={t('Average TTFT')}
        />
        <LatencyTrendChart series={latencySeries} />
      </section>

      <section>
        <SectionHeader
          icon={HeartPulse}
          title={t('Availability (last 24h)')}
          description={
            incidentCount > 0
              ? t(
                  'Request success rate; {{incidents}} incident buckets in the last 24 hours',
                  {
                    incidents: incidentCount,
                  }
                )
              : t('Request success rate sampled over the last 24 hours')
          }
          accent={
            incidentCount > 0 ? (
              <span className='inline-flex items-center gap-1 text-amber-600 dark:text-amber-400'>
                <AlertTriangle className='size-3.5' />
                {t('{{count}} incidents', {
                  count: incidentCount,
                })}
              </span>
            ) : null
          }
        />
        <UptimeTrendChart series={uptimeSeries} />
      </section>
    </div>
  )
}

function SectionHeader(props: {
  icon: React.ComponentType<{ className?: string }>
  title: string
  description?: string
  accent?: React.ReactNode
}) {
  const Icon = props.icon
  return (
    <div className='mb-2 flex flex-wrap items-center justify-between gap-2'>
      <div className='flex min-w-0 items-center gap-2'>
        <Icon className='text-muted-foreground/70 size-3.5 shrink-0' />
        <div className='min-w-0'>
          <div className='text-foreground text-sm font-semibold'>
            {props.title}
          </div>
          {props.description && (
            <p className='text-muted-foreground/80 text-xs'>
              {props.description}
            </p>
          )}
        </div>
      </div>
      {props.accent && (
        <div className='shrink-0 text-xs font-medium'>{props.accent}</div>
      )}
    </div>
  )
}
