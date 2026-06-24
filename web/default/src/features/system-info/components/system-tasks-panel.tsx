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
import { useQuery } from '@tanstack/react-query'
import { ListChecks, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampRelative, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { listSystemTasks } from '@/features/system-settings/api'
import type {
  SystemTask,
  SystemTaskStatus,
} from '@/features/system-settings/types'

const TASK_LIMIT = 20
const ACTIVE_POLL_INTERVAL_MS = 8000

const STATUS_CLASS_NAME: Record<SystemTaskStatus, string> = {
  pending: 'bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
  running: 'bg-sky-50 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300',
  succeeded:
    'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
  failed: '',
}

const TYPE_LABEL: Record<string, string> = {
  log_cleanup: 'Log cleanup',
  channel_test: 'Batch channel test',
  model_update: 'Batch upstream model update',
  midjourney_poll: 'Midjourney task polling',
  async_task_poll: 'Async task polling',
}

const STATUS_LABEL: Record<SystemTaskStatus, string> = {
  pending: 'Pending',
  running: 'Running',
  succeeded: 'Succeeded',
  failed: 'Failed',
}

function isActiveStatus(status: SystemTaskStatus) {
  return status === 'pending' || status === 'running'
}

function getProgress(task: SystemTask) {
  const progress = (task.state as { progress?: unknown } | undefined)?.progress
  if (typeof progress !== 'number' || Number.isNaN(progress)) return null
  return Math.min(100, Math.max(0, progress))
}

function SystemTasksTable({ tasks }: { tasks: SystemTask[] }) {
  const { t, i18n } = useTranslation()

  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table className='min-w-[840px]'>
        <TableHeader>
          <TableRow className='bg-muted/40 hover:bg-muted/40'>
            <TableHead className='h-9 px-4 text-xs'>{t('Type')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Status')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Progress')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Executor')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Updated')}</TableHead>
            <TableHead className='h-9 pr-4 text-xs'>{t('Error')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {tasks.map((task) => {
            const progress = getProgress(task)
            return (
              <TableRow key={task.task_id}>
                <TableCell className='px-4 py-3'>
                  <div className='space-y-0.5'>
                    <div className='text-sm font-medium'>
                      {t(TYPE_LABEL[task.type] ?? task.type)}
                    </div>
                    <div className='text-muted-foreground font-mono text-[11px]'>
                      {task.type}
                    </div>
                  </div>
                </TableCell>
                <TableCell className='py-3'>
                  <Badge
                    variant={task.status === 'failed' ? 'destructive' : 'secondary'}
                    className={cn('capitalize', STATUS_CLASS_NAME[task.status])}
                  >
                    {t(STATUS_LABEL[task.status])}
                  </Badge>
                </TableCell>
                <TableCell className='py-3'>
                  <div className='flex items-center gap-2'>
                    <Progress value={progress ?? 0} className='w-24' />
                    <span className='text-muted-foreground w-10 text-right text-xs tabular-nums'>
                      {progress === null ? '-' : `${progress}%`}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='text-muted-foreground max-w-[220px] truncate py-3 font-mono text-xs'>
                  {task.locked_by || '-'}
                </TableCell>
                <TableCell
                  className='text-muted-foreground py-3 text-xs whitespace-nowrap'
                  title={formatTimestampToDate(task.updated_at)}
                >
                  {formatTimestampRelative(
                    task.updated_at,
                    'seconds',
                    i18n.language
                  )}
                </TableCell>
                <TableCell
                  className='text-destructive max-w-[220px] truncate py-3 pr-4 text-xs'
                  title={task.error || undefined}
                >
                  {task.error || '-'}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

export function SystemTasksPanel() {
  const { t } = useTranslation()
  const tasksQuery = useQuery({
    queryKey: ['system-info', 'system-tasks'],
    queryFn: async () => {
      const res = await listSystemTasks(TASK_LIMIT)
      if (!res.success || !Array.isArray(res.data)) {
        throw new Error(res.message || 'We could not load system tasks.')
      }
      return res.data
    },
    retry: false,
    refetchInterval: (query) =>
      query.state.data?.some((task) => isActiveStatus(task.status))
        ? ACTIVE_POLL_INTERVAL_MS
        : false,
  })

  const tasks = tasksQuery.data ?? []
  const loading = tasksQuery.isLoading
  const refreshing = tasksQuery.isFetching && !loading
  const activeTasks = tasks.filter((task) => isActiveStatus(task.status))
  const historyTasks = tasks.filter((task) => !isActiveStatus(task.status))

  return (
    <section className='bg-card overflow-hidden rounded-lg border shadow-xs'>
      <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='bg-muted text-muted-foreground inline-flex size-7 items-center justify-center rounded-md'>
            <ListChecks className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <h3 className='text-sm font-semibold'>{t('System Tasks')}</h3>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t(
                'Recent maintenance tasks running across instances and their execution status.'
              )}
            </p>
          </div>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void tasksQuery.refetch()}
          disabled={tasksQuery.isFetching}
        >
          <RefreshCw
            data-icon='inline-start'
            className={cn(refreshing && 'animate-spin')}
            aria-hidden='true'
          />
          {refreshing ? t('Refreshing...') : t('Refresh')}
        </Button>
      </div>

      {loading ? (
        <div className='space-y-2 p-4 sm:p-5'>
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className='h-9 w-full rounded-md' />
          ))}
        </div>
      ) : tasksQuery.isError ? (
        <ErrorState
          title={t('We could not load system tasks.')}
          description={
            tasksQuery.error instanceof Error
              ? tasksQuery.error.message
              : undefined
          }
          onRetry={() => void tasksQuery.refetch()}
          className='min-h-[260px]'
        />
      ) : tasks.length === 0 ? (
        <div className='text-muted-foreground px-4 py-10 text-center text-sm sm:px-5'>
          {t('No system tasks yet.')}
        </div>
      ) : (
        <div className='space-y-4 p-4 sm:p-5'>
          <div>
            <div className='mb-2 flex items-center justify-between gap-3'>
              <div>
                <h4 className='text-sm font-medium'>{t('Active Tasks')}</h4>
                <p className='text-muted-foreground mt-0.5 text-xs'>
                  {t('Tasks currently pending or running.')}
                </p>
              </div>
              <Badge variant='outline'>{activeTasks.length}</Badge>
            </div>
            {activeTasks.length > 0 ? (
              <SystemTasksTable tasks={activeTasks} />
            ) : (
              <div className='text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
                {t('No active system tasks.')}
              </div>
            )}
          </div>

          <div>
            <div className='mb-2 flex items-center justify-between gap-3'>
              <div>
                <h4 className='text-sm font-medium'>{t('Task History')}</h4>
                <p className='text-muted-foreground mt-0.5 text-xs'>
                  {t('Recently completed or failed system task runs.')}
                </p>
              </div>
              <Badge variant='outline'>{historyTasks.length}</Badge>
            </div>
            {historyTasks.length > 0 ? (
              <SystemTasksTable tasks={historyTasks} />
            ) : (
              <div className='text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
                {t('No historical system tasks.')}
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
