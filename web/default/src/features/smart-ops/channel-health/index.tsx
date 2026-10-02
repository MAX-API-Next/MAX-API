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
import { useMemo, useState, type ReactElement } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import {
  Activity,
  AlertTriangle,
  Bell,
  CheckCircle2,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldAlert,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { getOptionValue } from '@/features/system-settings/hooks/use-system-options'
import {
  getChannelHealthPolicy,
  recoverChannelHealthChannel,
  getSmartOpsAlerts,
  updateChannelHealthPolicy,
} from '../api'
import { getObservedAtMilliseconds } from '../lib/format'
import type { SmartOpsAlert } from '../types'

const ALERT_POLL_INTERVAL_MS = 30000

const DELIVERY_STATUS_LABELS: Record<string, string> = {
  queued: 'Queued',
  sending: 'Sending',
  sent: 'Sent',
  skipped_repeat: 'Skipped (repeat interval)',
  skipped_unconfigured: 'Skipped (not configured)',
  failed: 'Delivery failed',
  delivery_unknown: 'Delivery status unknown',
}

const DEFAULT_OPTION_VALUES = {
  'monitor_setting.auto_priority_demotion_enabled': false,
  'monitor_setting.streaming_first_result_timeout_seconds': 0,
  'monitor_setting.non_streaming_response_timeout_seconds': 0,
  'monitor_setting.priority_deduction': 0,
  'monitor_setting.timeout_auto_disable_enabled': false,
  'monitor_setting.timeout_auto_disable_count': 5,
  'monitor_setting.timeout_auto_disable_window_seconds': 3600,
  'monitor_setting.timeout_auto_disable_minimum_samples': 0,
  'monitor_setting.timeout_auto_disable_ratio_percent': 0,
  'monitor_setting.timeout_auto_disable_duration_seconds': 1800,
  'monitor_setting.timeout_auto_disable_successful_probe_count': 1,
  'monitor_setting.timeout_auto_disable_recovery_mode': 'probe_then_automatic',
  'monitor_setting.timeout_auto_disable_count_scope': 'same_mode',
  'monitor_setting.penalty_cooldown_seconds': 600,
  'monitor_setting.recovery_mode': 'automatic',
  'monitor_setting.extend_on_repeat_timeout': true,
  'monitor_setting.channel_timeout_notification_enabled': false,
  'monitor_setting.alert_repeat_interval_seconds': 900,
} as const

type ChannelHealthDraft = {
  autoPriorityDemotionEnabled: boolean
  streamingFirstResultTimeoutSeconds: number
  nonStreamingResponseTimeoutSeconds: number
  priorityDeduction: number
  timeoutAutoDisableEnabled: boolean
  timeoutAutoDisableCount: number
  timeoutAutoDisableWindowSeconds: number
  timeoutAutoDisableMinimumSamples: number
  timeoutAutoDisableRatioPercent: number
  timeoutAutoDisableDurationSeconds: number
  timeoutAutoDisableSuccessfulProbeCount: number
  timeoutAutoDisableRecoveryMode: string
  timeoutAutoDisableCountScope: string
  penaltyCooldownSeconds: number
  recoveryMode: string
  extendOnRepeatTimeout: boolean
  channelTimeoutNotificationEnabled: boolean
  alertRepeatIntervalSeconds: number
}

const OPTION_KEYS = {
  autoPriorityDemotionEnabled: 'monitor_setting.auto_priority_demotion_enabled',
  streamingFirstResultTimeoutSeconds:
    'monitor_setting.streaming_first_result_timeout_seconds',
  nonStreamingResponseTimeoutSeconds:
    'monitor_setting.non_streaming_response_timeout_seconds',
  priorityDeduction: 'monitor_setting.priority_deduction',
  timeoutAutoDisableEnabled: 'monitor_setting.timeout_auto_disable_enabled',
  timeoutAutoDisableCount: 'monitor_setting.timeout_auto_disable_count',
  timeoutAutoDisableWindowSeconds:
    'monitor_setting.timeout_auto_disable_window_seconds',
  timeoutAutoDisableMinimumSamples:
    'monitor_setting.timeout_auto_disable_minimum_samples',
  timeoutAutoDisableRatioPercent:
    'monitor_setting.timeout_auto_disable_ratio_percent',
  timeoutAutoDisableDurationSeconds:
    'monitor_setting.timeout_auto_disable_duration_seconds',
  timeoutAutoDisableSuccessfulProbeCount:
    'monitor_setting.timeout_auto_disable_successful_probe_count',
  timeoutAutoDisableRecoveryMode:
    'monitor_setting.timeout_auto_disable_recovery_mode',
  timeoutAutoDisableCountScope:
    'monitor_setting.timeout_auto_disable_count_scope',
  penaltyCooldownSeconds: 'monitor_setting.penalty_cooldown_seconds',
  recoveryMode: 'monitor_setting.recovery_mode',
  extendOnRepeatTimeout: 'monitor_setting.extend_on_repeat_timeout',
  channelTimeoutNotificationEnabled:
    'monitor_setting.channel_timeout_notification_enabled',
  alertRepeatIntervalSeconds: 'monitor_setting.alert_repeat_interval_seconds',
} as const

function parseDraft(
  options: Array<{ key: string; value: string }> | undefined
): ChannelHealthDraft {
  const values = getOptionValue(options, DEFAULT_OPTION_VALUES)
  return {
    autoPriorityDemotionEnabled:
      values[OPTION_KEYS.autoPriorityDemotionEnabled],
    streamingFirstResultTimeoutSeconds:
      values[OPTION_KEYS.streamingFirstResultTimeoutSeconds],
    nonStreamingResponseTimeoutSeconds:
      values[OPTION_KEYS.nonStreamingResponseTimeoutSeconds],
    priorityDeduction: values[OPTION_KEYS.priorityDeduction],
    timeoutAutoDisableEnabled: values[OPTION_KEYS.timeoutAutoDisableEnabled],
    timeoutAutoDisableCount: values[OPTION_KEYS.timeoutAutoDisableCount],
    timeoutAutoDisableWindowSeconds:
      values[OPTION_KEYS.timeoutAutoDisableWindowSeconds],
    timeoutAutoDisableMinimumSamples:
      values[OPTION_KEYS.timeoutAutoDisableMinimumSamples],
    timeoutAutoDisableRatioPercent:
      values[OPTION_KEYS.timeoutAutoDisableRatioPercent],
    timeoutAutoDisableDurationSeconds:
      values[OPTION_KEYS.timeoutAutoDisableDurationSeconds],
    timeoutAutoDisableSuccessfulProbeCount:
      values[OPTION_KEYS.timeoutAutoDisableSuccessfulProbeCount],
    timeoutAutoDisableRecoveryMode:
      values[OPTION_KEYS.timeoutAutoDisableRecoveryMode],
    timeoutAutoDisableCountScope:
      values[OPTION_KEYS.timeoutAutoDisableCountScope],
    penaltyCooldownSeconds: values[OPTION_KEYS.penaltyCooldownSeconds],
    recoveryMode: values[OPTION_KEYS.recoveryMode],
    extendOnRepeatTimeout: values[OPTION_KEYS.extendOnRepeatTimeout],
    channelTimeoutNotificationEnabled:
      values[OPTION_KEYS.channelTimeoutNotificationEnabled],
    alertRepeatIntervalSeconds: values[OPTION_KEYS.alertRepeatIntervalSeconds],
  }
}

function validateDraft(draft: ChannelHealthDraft): string | null {
  const bounded: Array<[number, number, string, number]> = [
    [
      draft.streamingFirstResultTimeoutSeconds,
      0,
      'Streaming first-result timeout must be between 0 and 86400 seconds.',
      86400,
    ],
    [
      draft.nonStreamingResponseTimeoutSeconds,
      0,
      'Non-streaming response timeout must be between 0 and 86400 seconds.',
      86400,
    ],
    [
      draft.priorityDeduction,
      0,
      'Priority deduction must be a non-negative integer.',
      2147483647,
    ],
    [
      draft.timeoutAutoDisableCount,
      2,
      'Disable count must be at least 2.',
      2147483647,
    ],
    [
      draft.timeoutAutoDisableWindowSeconds,
      60,
      'Disable window must be between 60 and 2592000 seconds.',
      2592000,
    ],
    [
      draft.timeoutAutoDisableMinimumSamples,
      0,
      'Minimum samples must be a non-negative integer.',
      2147483647,
    ],
    [
      draft.timeoutAutoDisableDurationSeconds,
      0,
      'Disable duration must be between 0 and 604800 seconds.',
      604800,
    ],
    [
      draft.timeoutAutoDisableSuccessfulProbeCount,
      1,
      'Successful probe count must be between 1 and 10.',
      10,
    ],
    [
      draft.penaltyCooldownSeconds,
      0,
      'Penalty cooldown must be between 0 and 2592000 seconds.',
      2592000,
    ],
    [
      draft.alertRepeatIntervalSeconds,
      0,
      'Alert repeat interval must be between 0 and 2592000 seconds.',
      2592000,
    ],
  ]
  for (const [value, min, message, max] of bounded) {
    if (!Number.isInteger(value) || value < min || value > max) {
      return message
    }
  }
  if (
    !Number.isFinite(draft.timeoutAutoDisableRatioPercent) ||
    draft.timeoutAutoDisableRatioPercent < 0 ||
    draft.timeoutAutoDisableRatioPercent > 100
  ) {
    return 'Timeout ratio must be between 0 and 100 percent.'
  }
  if (
    draft.timeoutAutoDisableRecoveryMode !== 'probe_then_automatic' &&
    draft.timeoutAutoDisableRecoveryMode !== 'manual'
  ) {
    return 'Recovery mode is invalid.'
  }
  if (
    draft.timeoutAutoDisableCountScope !== 'same_mode' &&
    draft.timeoutAutoDisableCountScope !== 'combined'
  ) {
    return 'Timeout count scope is invalid.'
  }
  if (draft.recoveryMode !== 'automatic' && draft.recoveryMode !== 'manual') {
    return 'Recovery mode is invalid.'
  }
  return null
}

function isChannelAlert(alert: SmartOpsAlert): boolean {
  return (
    alert.key.startsWith('channel_timeout_priority_demotion:') ||
    alert.key.startsWith('channel_timeout_auto_disabled:')
  )
}

function channelIdFromAlert(alert: SmartOpsAlert): string {
  return alert.key.split(':', 2)[1] ?? alert.node ?? '—'
}

function NumberSetting({
  id,
  label,
  description,
  value,
  disabled,
  step = 1,
  onChange,
}: {
  id: string
  label: string
  description: string
  value: number
  disabled: boolean
  step?: number
  onChange: (value: number) => void
}): ReactElement {
  return (
    <label className='flex flex-col gap-1.5' htmlFor={id}>
      <span className='text-sm font-medium'>{label}</span>
      <Input
        id={id}
        type='number'
        min={0}
        step={step}
        value={value}
        disabled={disabled}
        onChange={(event) => {
          const next = Number(event.target.value)
          onChange(Number.isFinite(next) ? Math.max(0, next) : 0)
        }}
      />
      <span className='text-muted-foreground text-xs'>{description}</span>
    </label>
  )
}

function SelectSetting({
  label,
  description,
  value,
  disabled,
  options,
  onChange,
}: {
  label: string
  description: string
  value: string
  disabled: boolean
  options: Array<{ value: string; label: string }>
  onChange: (value: string) => void
}): ReactElement {
  return (
    <label className='flex flex-col gap-1.5'>
      <span className='text-sm font-medium'>{label}</span>
      <Select
        value={value}
        onValueChange={(next) => {
          if (next != null) onChange(next)
        }}
        disabled={disabled}
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <span className='text-muted-foreground text-xs'>{description}</span>
    </label>
  )
}

export function ChannelHealth(): ReactElement {
  const { t, i18n } = useTranslation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const canEdit = userRole === ROLE.SUPER_ADMIN
  const loadErrorMessage = t('We could not load active alerts.')
  const alertsQuery = useQuery({
    queryKey: ['smart-ops', 'channel-health-alerts', loadErrorMessage],
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
  const policyQuery = useQuery({
    queryKey: ['smart-ops', 'channel-health-policy'],
    queryFn: async () => {
      const response = await getChannelHealthPolicy()
      const hasAllOptions =
        response.success &&
        Array.isArray(response.data) &&
        Object.values(OPTION_KEYS).every((key) =>
          response.data.some((option) => option.key === key)
        )
      if (!hasAllOptions) {
        throw new Error(
          response.message || t('We could not load the channel health policy.')
        )
      }
      return response
    },
    retry: false,
    staleTime: 30 * 1000,
  })
  const [draftOverride, setDraftOverride] = useState<ChannelHealthDraft | null>(
    null
  )
  const updatePolicy = useMutation({
    mutationFn: updateChannelHealthPolicy,
    onSuccess: (response) => {
      if (response.success) {
        toast.success(t('Setting updated successfully'))
        setDraftOverride(null)
        void policyQuery.refetch()
      } else {
        toast.error(response.message || t('Failed to update setting'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update setting'))
    },
  })
  const recoveryMutation = useMutation({
    mutationFn: recoverChannelHealthChannel,
    onSuccess: (response) => {
      if (response.success) {
        toast.success(t('Channel recovery requested'))
        void alertsQuery.refetch()
      } else {
        toast.error(response.message || t('Channel recovery failed'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Channel recovery failed'))
    },
  })
  const loadedDraft = useMemo(
    () => parseDraft(policyQuery.data?.data),
    [policyQuery.data?.data]
  )
  const draft = draftOverride ?? loadedDraft
  const updateDraft = (
    updater: (current: ChannelHealthDraft) => ChannelHealthDraft
  ) => {
    setDraftOverride((current) => updater(current ?? loadedDraft))
  }

  const channelAlerts = (alertsQuery.data ?? []).filter(isChannelAlert)
  const demotionAlerts = channelAlerts.filter((alert) =>
    alert.key.startsWith('channel_timeout_priority_demotion:')
  )
  const disabledAlerts = channelAlerts.filter((alert) =>
    alert.key.startsWith('channel_timeout_auto_disabled:')
  )
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(i18n.language, {
        dateStyle: 'medium',
        timeStyle: 'medium',
      }),
    [i18n.language]
  )

  const save = async () => {
    const validationError = validateDraft(draft)
    if (validationError) {
      toast.error(t(validationError))
      return
    }
    const updates: Array<{ key: string; value: string | number | boolean }> = []
    ;(Object.keys(OPTION_KEYS) as Array<keyof typeof OPTION_KEYS>).forEach(
      (name) => {
        if (draft[name] !== loadedDraft[name]) {
          updates.push({ key: OPTION_KEYS[name], value: draft[name] })
        }
      }
    )
    if (updates.length === 0) {
      return
    }
    updatePolicy.mutate(updates)
  }

  const setNumber = (name: keyof ChannelHealthDraft, value: number) =>
    updateDraft((current) => ({ ...current, [name]: value }))

  if (policyQuery.isError) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Channel health')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <ErrorState
            title={t('We could not load the channel health policy.')}
            onRetry={() => void policyQuery.refetch()}
          />
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (policyQuery.isLoading) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Channel health')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <Skeleton className='h-96 w-full rounded-xl' />
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex min-w-0 items-center gap-2'>
          <span className='truncate'>{t('Channel health')}</span>
          <Badge variant='outline'>{t('Smart Operations')}</Badge>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          onClick={() => {
            void alertsQuery.refetch()
            void policyQuery.refetch()
          }}
          disabled={alertsQuery.isFetching || policyQuery.isFetching}
        >
          <RefreshCw
            data-icon='inline-start'
            className={alertsQuery.isFetching ? 'animate-spin' : undefined}
            aria-hidden='true'
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <Alert>
            <Activity aria-hidden='true' />
            <AlertTitle>{t('Channel timeout governance')}</AlertTitle>
            <AlertDescription>
              {t(
                'This view combines timeout alerts with the existing monitor_setting policy. Values are stored through the existing system option endpoint; no new database table or field is used.'
              )}
            </AlertDescription>
          </Alert>

          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            <StatusCard
              icon={Activity}
              label={t('Channel timeout alerts')}
              value={channelAlerts.length}
            />
            <StatusCard
              icon={AlertTriangle}
              label={t('Priority demotions')}
              value={demotionAlerts.length}
            />
            <StatusCard
              icon={ShieldAlert}
              label={t('Automatic disable alerts')}
              value={disabledAlerts.length}
            />
            <StatusCard
              icon={Bell}
              label={t('Notifications')}
              value={
                draft.channelTimeoutNotificationEnabled
                  ? t('Enabled')
                  : t('Disabled')
              }
            />
          </div>

          <Card>
            <CardHeader>
              <CardTitle>{t('Timeout policy')}</CardTitle>
              <CardDescription>
                {canEdit
                  ? t(
                      'Super administrators can change the existing monitor_setting values.'
                    )
                  : t('Only super administrators can change these values.')}
              </CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-5'>
              <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                <div>
                  <div className='text-sm font-medium'>
                    {t('Enable automatic priority demotion')}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {t(
                      'Reduce a channel priority after an eligible upstream timeout.'
                    )}
                  </div>
                </div>
                <Switch
                  checked={draft.autoPriorityDemotionEnabled}
                  disabled={!canEdit}
                  onCheckedChange={(checked) =>
                    updateDraft((current) => ({
                      ...current,
                      autoPriorityDemotionEnabled: checked,
                    }))
                  }
                />
              </div>
              <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                <div>
                  <div className='text-sm font-medium'>
                    {t('Enable automatic channel disable')}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {t(
                      'Disable a channel when its configured timeout count and ratio gates are met.'
                    )}
                  </div>
                </div>
                <Switch
                  checked={draft.timeoutAutoDisableEnabled}
                  disabled={!canEdit}
                  onCheckedChange={(checked) =>
                    updateDraft((current) => ({
                      ...current,
                      timeoutAutoDisableEnabled: checked,
                    }))
                  }
                />
              </div>
              <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
                <NumberSetting
                  id='streaming-timeout'
                  label={t('Streaming first-result timeout')}
                  description={t('Seconds; 0 disables this threshold.')}
                  value={draft.streamingFirstResultTimeoutSeconds}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('streamingFirstResultTimeoutSeconds', value)
                  }
                />
                <NumberSetting
                  id='non-streaming-timeout'
                  label={t('Non-streaming response timeout')}
                  description={t('Seconds; 0 disables this threshold.')}
                  value={draft.nonStreamingResponseTimeoutSeconds}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('nonStreamingResponseTimeoutSeconds', value)
                  }
                />
                <NumberSetting
                  id='priority-deduction'
                  label={t('Priority deduction')}
                  description={t(
                    'Administrator-defined priority points per timeout.'
                  )}
                  value={draft.priorityDeduction}
                  disabled={!canEdit}
                  onChange={(value) => setNumber('priorityDeduction', value)}
                />
                <NumberSetting
                  id='penalty-cooldown'
                  label={t('Penalty cooldown')}
                  description={t(
                    'Seconds before the same channel can be penalized again.'
                  )}
                  value={draft.penaltyCooldownSeconds}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('penaltyCooldownSeconds', value)
                  }
                />
                <NumberSetting
                  id='alert-repeat'
                  label={t('Alert repeat interval')}
                  description={t(
                    'Seconds between repeated notifications for one channel.'
                  )}
                  value={draft.alertRepeatIntervalSeconds}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('alertRepeatIntervalSeconds', value)
                  }
                />
                <SelectSetting
                  label={t('Recovery mode')}
                  description={t(
                    'Automatic recovery restores a penalty after its cooldown; manual recovery keeps the penalty until an administrator acts.'
                  )}
                  value={draft.recoveryMode}
                  disabled={!canEdit}
                  options={[
                    { value: 'automatic', label: t('Automatic') },
                    { value: 'manual', label: t('Manual recovery') },
                  ]}
                  onChange={(value) =>
                    updateDraft((current) => ({
                      ...current,
                      recoveryMode: value,
                    }))
                  }
                />
                <NumberSetting
                  id='disable-count'
                  label={t('Disable after timeout count')}
                  description={t(
                    'Consecutive/window count used by the existing auto-disable policy.'
                  )}
                  value={draft.timeoutAutoDisableCount}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('timeoutAutoDisableCount', value)
                  }
                />
                <NumberSetting
                  id='disable-window'
                  label={t('Auto-disable window')}
                  description={t('Seconds used to evaluate timeout counts.')}
                  value={draft.timeoutAutoDisableWindowSeconds}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('timeoutAutoDisableWindowSeconds', value)
                  }
                />
                <NumberSetting
                  id='disable-minimum-samples'
                  label={t('Minimum timeout samples')}
                  description={t(
                    'Minimum observations before ratio-based disable is eligible.'
                  )}
                  value={draft.timeoutAutoDisableMinimumSamples}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('timeoutAutoDisableMinimumSamples', value)
                  }
                />
                <NumberSetting
                  id='disable-ratio'
                  label={t('Timeout ratio threshold')}
                  description={t(
                    'Percentage threshold for the existing auto-disable policy.'
                  )}
                  value={draft.timeoutAutoDisableRatioPercent}
                  disabled={!canEdit}
                  step={0.1}
                  onChange={(value) =>
                    setNumber('timeoutAutoDisableRatioPercent', value)
                  }
                />
                <NumberSetting
                  id='disable-duration'
                  label={t('Auto-disable duration')}
                  description={t(
                    'Seconds a channel remains disabled before recovery checks.'
                  )}
                  value={draft.timeoutAutoDisableDurationSeconds}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('timeoutAutoDisableDurationSeconds', value)
                  }
                />
                <NumberSetting
                  id='probe-count'
                  label={t('Successful recovery probes')}
                  description={t(
                    'Successful probes required before automatic recovery.'
                  )}
                  value={draft.timeoutAutoDisableSuccessfulProbeCount}
                  disabled={!canEdit}
                  onChange={(value) =>
                    setNumber('timeoutAutoDisableSuccessfulProbeCount', value)
                  }
                />
                <SelectSetting
                  label={t('Automatic recovery mode')}
                  description={t(
                    'Probe mode recovers after configured successful probes; manual mode requires an administrator.'
                  )}
                  value={draft.timeoutAutoDisableRecoveryMode}
                  disabled={!canEdit}
                  options={[
                    {
                      value: 'probe_then_automatic',
                      label: t('Probe then automatic'),
                    },
                    { value: 'manual', label: t('Manual recovery') },
                  ]}
                  onChange={(value) =>
                    updateDraft((current) => ({
                      ...current,
                      timeoutAutoDisableRecoveryMode: value,
                    }))
                  }
                />
                <div className='flex items-center justify-between gap-4 rounded-lg border p-3 sm:col-span-2 lg:col-span-3'>
                  <div>
                    <div className='text-sm font-medium'>
                      {t('Extend penalty on repeated timeout')}
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {t(
                        'Keep the current penalty deadline when repeated timeouts continue during an active penalty.'
                      )}
                    </div>
                  </div>
                  <Switch
                    checked={draft.extendOnRepeatTimeout}
                    disabled={!canEdit}
                    onCheckedChange={(checked) =>
                      updateDraft((current) => ({
                        ...current,
                        extendOnRepeatTimeout: checked,
                      }))
                    }
                  />
                </div>
                <SelectSetting
                  label={t('Timeout count scope')}
                  description={t(
                    'Count streaming and non-streaming timeouts separately or together.'
                  )}
                  value={draft.timeoutAutoDisableCountScope}
                  disabled={!canEdit}
                  options={[
                    {
                      value: 'same_mode',
                      label: t('Same request mode'),
                    },
                    { value: 'combined', label: t('Combined modes') },
                  ]}
                  onChange={(value) =>
                    updateDraft((current) => ({
                      ...current,
                      timeoutAutoDisableCountScope: value,
                    }))
                  }
                />
              </div>
              <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                <div>
                  <div className='text-sm font-medium'>
                    {t('Channel timeout notifications')}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {t(
                      'Expose channel demotion and disable transitions to the SmartOps alert stream.'
                    )}
                  </div>
                </div>
                <Switch
                  checked={draft.channelTimeoutNotificationEnabled}
                  disabled={!canEdit}
                  onCheckedChange={(checked) =>
                    updateDraft((current) => ({
                      ...current,
                      channelTimeoutNotificationEnabled: checked,
                    }))
                  }
                />
              </div>
              {canEdit && (
                <div className='flex justify-end'>
                  <Button
                    onClick={() => void save()}
                    disabled={updatePolicy.isPending || policyQuery.isLoading}
                  >
                    <Save data-icon='inline-start' aria-hidden='true' />
                    {updatePolicy.isPending
                      ? t('Saving...')
                      : t('Save timeout policy')}
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>

          {alertsQuery.isLoading && (
            <Skeleton className='h-32 w-full rounded-xl' />
          )}
          {alertsQuery.isError && (
            <ErrorState
              title={t('We could not load active alerts.')}
              onRetry={() => void alertsQuery.refetch()}
            />
          )}
          {!alertsQuery.isLoading && !alertsQuery.isError && (
            <Card>
              <CardHeader>
                <CardTitle>{t('Channel alert stream')}</CardTitle>
                <CardDescription>
                  {t(
                    'Live channel timeout transitions from the existing SmartOps alert endpoint.'
                  )}
                </CardDescription>
              </CardHeader>
              <CardContent>
                {channelAlerts.length === 0 ? (
                  <div className='text-muted-foreground flex items-center gap-2 rounded-lg border border-dashed p-6 text-sm'>
                    <CheckCircle2
                      className='text-success size-4'
                      aria-hidden='true'
                    />
                    {t('No active channel timeout alerts.')}
                  </div>
                ) : (
                  <div className='divide-y rounded-lg border'>
                    {channelAlerts.map((alert) => {
                      const observedAt = getObservedAtMilliseconds(alert)
                      return (
                        <div
                          key={alert.key}
                          className='flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between'
                        >
                          <div className='min-w-0'>
                            <div className='flex flex-wrap items-center gap-2'>
                              <Badge
                                variant={
                                  alert.severity === 'critical'
                                    ? 'destructive'
                                    : 'secondary'
                                }
                              >
                                {alert.severity === 'critical'
                                  ? t('Critical')
                                  : t('Warning')}
                              </Badge>
                              <span className='font-medium'>
                                {channelIdFromAlert(alert) === '—'
                                  ? t('Unknown channel')
                                  : t('Channel {{id}}', {
                                      id: channelIdFromAlert(alert),
                                    })}
                              </span>
                            </div>
                            <div className='text-muted-foreground mt-1 text-sm'>
                              {alert.message}
                            </div>
                            {alert.delivery_status && (
                              <div className='text-muted-foreground mt-1 text-xs'>
                                {t('Delivery status')}:{' '}
                                {DELIVERY_STATUS_LABELS[alert.delivery_status]
                                  ? t(
                                      DELIVERY_STATUS_LABELS[
                                        alert.delivery_status
                                      ]
                                    )
                                  : alert.delivery_status}
                                {alert.delivery_attempts != null
                                  ? ` (${alert.delivery_attempts})`
                                  : ''}
                              </div>
                            )}
                            <div className='text-muted-foreground mt-1 font-mono text-xs'>
                              {alert.key}
                            </div>
                          </div>
                          <time
                            className='text-muted-foreground shrink-0 text-xs'
                            dateTime={
                              observedAt == null
                                ? undefined
                                : new Date(observedAt).toISOString()
                            }
                          >
                            {observedAt == null
                              ? '—'
                              : dateFormatter.format(new Date(observedAt))}
                          </time>
                          {canEdit &&
                            /^\d+$/.test(channelIdFromAlert(alert)) && (
                              <Button
                                variant='outline'
                                size='sm'
                                disabled={recoveryMutation.isPending}
                                onClick={() =>
                                  recoveryMutation.mutate(
                                    channelIdFromAlert(alert)
                                  )
                                }
                              >
                                <RotateCcw
                                  data-icon='inline-start'
                                  aria-hidden='true'
                                />
                                {t('Recover channel')}
                              </Button>
                            )}
                        </div>
                      )
                    })}
                  </div>
                )}
              </CardContent>
            </Card>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function StatusCard({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof Activity
  label: string
  value: number | string
}): ReactElement {
  return (
    <Card size='sm'>
      <CardHeader>
        <CardDescription>{label}</CardDescription>
        <CardTitle className='tabular-nums'>{value}</CardTitle>
        <div className='bg-muted text-muted-foreground flex size-8 items-center justify-center rounded-md'>
          <Icon className='size-4' aria-hidden='true' />
        </div>
      </CardHeader>
    </Card>
  )
}
