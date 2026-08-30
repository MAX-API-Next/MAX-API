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
import type { ReactElement } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { formatTimestampRelative, formatTimestampToDate } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
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
import { getBillingSettlementReconciliation, getSmartOpsAlerts } from './api'
import { BillingSettlementEvidence } from './components/billing-settlement-evidence'
import {
  formatAlertCurrentValue,
  formatAlertThreshold,
  getObservedAtMilliseconds,
} from './lib/format'
import type {
  BillingSettlementReconciliationData,
  SmartOpsAlert,
} from './types'

const ALERT_POLL_INTERVAL_MS = 30000

const RESOURCE_LABEL: Record<string, string> = {
  system_cpu: 'CPU usage',
  system_memory: 'Memory usage',
  system_disk: 'Disk usage',
  billing_settlement_backlog: 'Billing reconciliation backlog',
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

interface ActiveAlertsContentProps {
  alerts: SmartOpsAlert[]
  error: unknown
  isError: boolean
  loadErrorMessage: string
  loading: boolean
  onRetry: () => void
}

function ActiveAlertsContent(props: ActiveAlertsContentProps): ReactElement {
  const { t } = useTranslation()

  if (props.loading) {
    return (
      <div className='flex flex-col gap-2'>
        {Array.from({ length: 3 }).map((_, index) => (
          <Skeleton key={index} className='h-10 w-full rounded-md' />
        ))}
      </div>
    )
  }

  if (props.isError) {
    return (
      <ErrorState
        title={props.loadErrorMessage}
        description={
          props.error instanceof Error ? props.error.message : undefined
        }
        onRetry={props.onRetry}
        className='min-h-[200px]'
      />
    )
  }

  if (props.alerts.length === 0) {
    return (
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
    )
  }

  return <ActiveAlertsTable alerts={props.alerts} />
}

export function ActiveAlerts(): ReactElement {
  const { t } = useTranslation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const canUpdateBlockingPolicy = userRole === ROLE.SUPER_ADMIN
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
            <ActiveAlertsContent
              alerts={alerts}
              error={alertsQuery.error}
              isError={alertsQuery.isError}
              loadErrorMessage={loadErrorMessage}
              loading={loading}
              onRetry={() => void alertsQuery.refetch()}
            />
            <BillingSettlementEvidence
              canUpdateBlockingPolicy={canUpdateBlockingPolicy}
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
