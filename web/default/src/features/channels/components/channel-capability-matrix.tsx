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
import {
  BadgeCheck,
  ChevronDown,
  CircleAlert,
  CircleCheckBig,
  Info,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { StatusBadge } from '@/components/status-badge'
import {
  CHANNEL_CAPABILITY_STATUS_LABELS,
  CHANNEL_CAPABILITY_STATUS_VARIANTS,
  getChannelCapabilityRows,
  type ChannelConfigValidationIssue,
} from '../lib'

type ChannelCapabilityMatrixProps = {
  channelType: number
  issues: ChannelConfigValidationIssue[]
}

const ISSUE_ICON_MAP = {
  error: CircleAlert,
  warning: BadgeCheck,
  info: Info,
} as const

const ISSUE_VARIANT_MAP = {
  error: 'danger',
  warning: 'warning',
  info: 'info',
} as const

const ISSUE_LABEL_MAP = {
  error: 'Error',
  warning: 'Warning',
  info: 'Info',
} as const

export function ChannelCapabilityMatrix(props: ChannelCapabilityMatrixProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const rows = getChannelCapabilityRows(props.channelType)
  const supportedCount = rows.filter(
    (row) => row.status !== 'unsupported'
  ).length
  const errorCount = props.issues.filter(
    (issue) => issue.severity === 'error'
  ).length
  const warningCount = props.issues.filter(
    (issue) => issue.severity === 'warning'
  ).length
  const hasBlockingIssues = errorCount > 0
  const hasWarnings = warningCount > 0

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='border-border/60 rounded-lg border'
    >
      <CollapsibleTrigger
        render={
          <button
            type='button'
            className='hover:bg-muted/35 flex w-full items-start justify-between gap-3 rounded-lg px-4 py-3 text-left transition-colors'
            aria-expanded={open}
          />
        }
      >
        <div className='flex min-w-0 items-start gap-3'>
          <span className='bg-muted text-muted-foreground mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md'>
            <ShieldCheck className='h-4 w-4' aria-hidden='true' />
          </span>
          <div className='min-w-0 space-y-1'>
            <div className='flex flex-wrap items-center gap-2'>
              <h3 className='text-sm font-semibold'>
                {t('Channel capability matrix')}
              </h3>
              <StatusBadge
                label={t('{{supported}}/{{total}} available', {
                  supported: supportedCount,
                  total: rows.length,
                })}
                variant='info'
                copyable={false}
                size='sm'
              />
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Shows the request APIs, task capabilities, and operational features supported by this channel type.'
              )}
            </p>
          </div>
        </div>
        <div className='flex shrink-0 flex-wrap items-center justify-end gap-2'>
          <StatusBadge
            label={
              hasBlockingIssues
                ? t('{{count}} blocking issue(s)', { count: errorCount })
                : t('Configuration valid')
            }
            variant={hasBlockingIssues ? 'danger' : 'success'}
            copyable={false}
            size='sm'
          />
          {hasWarnings && (
            <StatusBadge
              label={t('{{count}} warning(s)', { count: warningCount })}
              variant='warning'
              copyable={false}
              size='sm'
            />
          )}
          <ChevronDown
            className={cn(
              'text-muted-foreground h-4 w-4 transition-transform',
              open && 'rotate-180'
            )}
            aria-hidden='true'
          />
        </div>
      </CollapsibleTrigger>

      <CollapsibleContent className='border-t px-4 pt-4 pb-4'>
        <div className='grid gap-2 md:grid-cols-2 xl:grid-cols-3'>
          {rows.map((row) => (
            <div
              key={row.id}
              className='border-border/60 bg-muted/20 flex min-h-20 flex-col justify-between rounded-md border px-3 py-2'
            >
              <div className='space-y-1'>
                <div className='flex items-start justify-between gap-2'>
                  <div className='min-w-0 space-y-0.5'>
                    <div className='text-sm font-medium'>{t(row.label)}</div>
                    <div className='text-muted-foreground text-xs leading-relaxed'>
                      {t(row.description)}
                    </div>
                  </div>
                  <StatusBadge
                    label={t(CHANNEL_CAPABILITY_STATUS_LABELS[row.status])}
                    variant={CHANNEL_CAPABILITY_STATUS_VARIANTS[row.status]}
                    copyable={false}
                    size='sm'
                    className='shrink-0'
                  />
                </div>
              </div>
            </div>
          ))}
        </div>

        <div className='mt-4 border-t pt-4'>
          <div className='mb-2 flex items-center gap-2'>
            <CircleCheckBig className='text-muted-foreground h-4 w-4' />
            <h4 className='text-sm font-semibold'>
              {t('Configuration validation')}
            </h4>
          </div>

          {props.issues.length === 0 ? (
            <Alert>
              <AlertDescription>
                {t(
                  'No blocking configuration issues were detected for this channel type.'
                )}
              </AlertDescription>
            </Alert>
          ) : (
            <div className='space-y-2'>
              {props.issues.map((issue) => {
                const IssueIcon = ISSUE_ICON_MAP[issue.severity]
                return (
                  <Alert
                    key={issue.id}
                    variant={
                      issue.severity === 'error' ? 'destructive' : 'default'
                    }
                    className={cn(
                      issue.severity === 'warning' &&
                        'border-warning/30 bg-warning/5 text-foreground'
                    )}
                  >
                    <IssueIcon className='h-4 w-4' />
                    <AlertDescription>
                      <div className='space-y-0.5'>
                        <div className='flex items-center gap-2'>
                          <StatusBadge
                            label={t(ISSUE_LABEL_MAP[issue.severity])}
                            variant={ISSUE_VARIANT_MAP[issue.severity]}
                            copyable={false}
                            size='sm'
                          />
                          <span className='text-sm'>{t(issue.message)}</span>
                        </div>
                      </div>
                    </AlertDescription>
                  </Alert>
                )
              })}
            </div>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
