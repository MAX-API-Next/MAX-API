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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2, RefreshCw, ServerCog, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampRelative, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { listSystemInstances } from '../api'
import {
  SYSTEM_INSTANCES_QUERY_KEY,
  useStaleInstanceCleanup,
} from '../hooks/use-stale-instance-cleanup'
import type { SystemInstance, SystemInstanceStatus } from '../types'

const INSTANCE_POLL_INTERVAL_MS = 30_000

const STATUS_CLASS_NAME: Record<SystemInstanceStatus, string> = {
  online:
    'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
  stale: 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
}

function formatPercent(value?: number) {
  if (typeof value !== 'number' || Number.isNaN(value)) return '-'
  return `${new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 1,
  }).format(value)}%`
}

function formatBytes(bytes?: number) {
  if (typeof bytes !== 'number' || Number.isNaN(bytes)) return '-'
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  )
  const value = bytes / 1024 ** index
  return `${new Intl.NumberFormat(undefined, {
    maximumFractionDigits: index === 0 ? 0 : 1,
  }).format(value)} ${units[index]}`
}

function getNodeName(instance: SystemInstance) {
  return instance.info?.node?.name || instance.node_name
}

function getStatusLabel(status: SystemInstanceStatus) {
  return status === 'online' ? 'Online' : 'Stale'
}

function getRoleLabel(instance: SystemInstance) {
  return instance.info?.role?.is_master ? 'Master' : 'Worker'
}

function RuntimeCell({ instance }: { instance: SystemInstance }) {
  const runtime = instance.info?.runtime
  const platform = [runtime?.goos, runtime?.goarch].filter(Boolean).join('/')
  return (
    <div className='space-y-0.5'>
      <div className='font-mono text-xs'>{runtime?.version || '-'}</div>
      <div className='text-muted-foreground font-mono text-[11px]'>
        {platform || '-'}
      </div>
    </div>
  )
}

function SystemInstancesTable({
  instances,
  deletingNodeName,
  deleteDisabled,
  onDeleteRequest,
}: {
  instances: SystemInstance[]
  deletingNodeName?: string | null
  deleteDisabled?: boolean
  onDeleteRequest: (instance: SystemInstance) => void
}) {
  const { t, i18n } = useTranslation()

  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table className='min-w-[1060px]'>
        <TableHeader>
          <TableRow className='bg-muted/40 hover:bg-muted/40'>
            <TableHead className='h-9 px-4 text-xs'>{t('Instance')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Status')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Role')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Resources')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Runtime')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Started')}</TableHead>
            <TableHead className='h-9 pr-4 text-xs'>{t('Last Seen')}</TableHead>
            <TableHead className='h-9 pr-4 text-right text-xs'>
              {t('Actions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {instances.map((instance) => {
            const resources = instance.info?.resources
            const storage = resources?.storage
            const nodeName = instance.node_name
            const isDeleting = deletingNodeName === nodeName
            return (
              <TableRow key={nodeName}>
                <TableCell className='px-4 py-3'>
                  <div className='min-w-0'>
                    <div className='truncate text-sm font-medium'>
                      {getNodeName(instance)}
                    </div>
                    <div className='text-muted-foreground truncate font-mono text-[11px]'>
                      {instance.info?.host?.hostname || '-'}
                    </div>
                  </div>
                </TableCell>
                <TableCell className='py-3'>
                  <Badge
                    variant='secondary'
                    className={cn(
                      'capitalize',
                      STATUS_CLASS_NAME[instance.status]
                    )}
                  >
                    {t(getStatusLabel(instance.status))}
                  </Badge>
                </TableCell>
                <TableCell className='py-3'>
                  <Badge variant='outline'>{t(getRoleLabel(instance))}</Badge>
                </TableCell>
                <TableCell className='text-muted-foreground py-3 text-xs'>
                  <div className='grid gap-1 font-mono'>
                    <span>
                      CPU {formatPercent(resources?.cpu?.usage_percent)}
                    </span>
                    <span>
                      MEM {formatPercent(resources?.memory?.usage_percent)}
                    </span>
                    <span>
                      DISK {formatPercent(storage?.used_percent)} /{' '}
                      {formatBytes(storage?.used_bytes)}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='py-3'>
                  <RuntimeCell instance={instance} />
                </TableCell>
                <TableCell className='text-muted-foreground py-3 text-xs whitespace-nowrap'>
                  {formatTimestampToDate(instance.started_at)}
                </TableCell>
                <TableCell
                  className='text-muted-foreground py-3 pr-4 text-xs whitespace-nowrap'
                  title={formatTimestampToDate(instance.last_seen_at)}
                >
                  {formatTimestampRelative(
                    instance.last_seen_at,
                    'seconds',
                    i18n.language
                  )}
                </TableCell>
                <TableCell className='py-3 pr-4 text-right'>
                  {instance.status === 'stale' ? (
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            onClick={() => onDeleteRequest(instance)}
                            disabled={isDeleting || deleteDisabled}
                            aria-label={t('Delete stale instance')}
                          />
                        }
                      >
                        {isDeleting ? (
                          <Loader2
                            className='animate-spin'
                            aria-hidden='true'
                          />
                        ) : (
                          <Trash2 aria-hidden='true' />
                        )}
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>{t('Delete stale instance')}</p>
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    <span className='text-muted-foreground text-xs'>-</span>
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

export function SystemInstancesPanel() {
  const { t } = useTranslation()
  const [instanceToDelete, setInstanceToDelete] =
    useState<SystemInstance | null>(null)
  const [deleteAllConfirmOpen, setDeleteAllConfirmOpen] = useState(false)
  const cleanup = useStaleInstanceCleanup({
    onDeletedAllStale: () => setDeleteAllConfirmOpen(false),
    onDeletedInstance: () => setInstanceToDelete(null),
  })
  const instancesQuery = useQuery({
    queryKey: SYSTEM_INSTANCES_QUERY_KEY,
    queryFn: async () => {
      const res = await listSystemInstances()
      if (!res.success || !Array.isArray(res.data)) {
        throw new Error(res.message || 'We could not load instances.')
      }
      return res.data
    },
    retry: false,
    refetchInterval: INSTANCE_POLL_INTERVAL_MS,
  })

  const instances = instancesQuery.data ?? []
  const staleInstances = instances.filter(
    (instance) => instance.status === 'stale'
  )
  const loading = instancesQuery.isLoading
  const refreshing = instancesQuery.isFetching && !loading
  const deleteNodeName = instanceToDelete
    ? getNodeName(instanceToDelete)
    : undefined

  const handleConfirmDelete = () => {
    if (!instanceToDelete) return

    cleanup.deleteInstance(instanceToDelete.node_name)
  }

  const handleConfirmDeleteAll = () => {
    cleanup.deleteAllStale()
  }

  return (
    <>
      <section className='bg-card overflow-hidden rounded-lg border shadow-xs'>
        <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
          <div className='flex min-w-0 items-center gap-2'>
            <span className='bg-muted text-muted-foreground inline-flex size-7 items-center justify-center rounded-md'>
              <ServerCog className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <h3 className='text-sm font-semibold'>{t('Instances')}</h3>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t(
                  'Nodes reporting from this deployment and their latest heartbeat.'
                )}
              </p>
            </div>
          </div>
          <div className='flex shrink-0 flex-wrap items-center gap-2 sm:justify-end'>
            {staleInstances.length > 0 ? (
              <Button
                type='button'
                variant='destructive'
                size='sm'
                onClick={() => setDeleteAllConfirmOpen(true)}
                disabled={cleanup.isDeletingAnyInstance}
              >
                {cleanup.deletingAllStale ? (
                  <Loader2
                    data-icon='inline-start'
                    className='animate-spin'
                    aria-hidden='true'
                  />
                ) : (
                  <Trash2 data-icon='inline-start' aria-hidden='true' />
                )}
                {t('Delete all stale')}
              </Button>
            ) : null}
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void instancesQuery.refetch()}
              disabled={
                instancesQuery.isFetching || cleanup.isDeletingAnyInstance
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
        </div>

        {loading ? (
          <div className='flex flex-col gap-2 p-4 sm:p-5'>
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className='h-9 w-full rounded-md' />
            ))}
          </div>
        ) : instancesQuery.isError ? (
          <ErrorState
            title={t('We could not load instances.')}
            description={
              instancesQuery.error instanceof Error
                ? instancesQuery.error.message
                : undefined
            }
            onRetry={() => void instancesQuery.refetch()}
            className='min-h-[220px]'
          />
        ) : instances.length === 0 ? (
          <div className='text-muted-foreground px-4 py-10 text-center text-sm sm:px-5'>
            {t('No instances have reported yet.')}
          </div>
        ) : (
          <div className='p-4 sm:p-5'>
            <SystemInstancesTable
              instances={instances}
              deletingNodeName={cleanup.deletingNodeName}
              deleteDisabled={cleanup.deletingAllStale}
              onDeleteRequest={setInstanceToDelete}
            />
          </div>
        )}
      </section>
      <ConfirmDialog
        open={deleteAllConfirmOpen}
        onOpenChange={setDeleteAllConfirmOpen}
        title={t('Delete stale instances')}
        desc={t(
          'Delete {{count}} stale instance records? Online instances will not be deleted.',
          { count: staleInstances.length }
        )}
        destructive
        isLoading={cleanup.deletingAllStale}
        confirmText={cleanup.deletingAllStale ? t('Deleting...') : t('Delete')}
        handleConfirm={handleConfirmDeleteAll}
      />
      <ConfirmDialog
        open={Boolean(instanceToDelete)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setInstanceToDelete(null)
        }}
        title={t('Delete stale instance')}
        desc={t(
          'This removes the offline instance record for {{node}}. Running instances cannot be deleted.',
          { node: deleteNodeName ?? '-' }
        )}
        destructive
        isLoading={Boolean(cleanup.deletingNodeName)}
        confirmText={cleanup.deletingNodeName ? t('Deleting...') : t('Delete')}
        handleConfirm={handleConfirmDelete}
      />
    </>
  )
}
