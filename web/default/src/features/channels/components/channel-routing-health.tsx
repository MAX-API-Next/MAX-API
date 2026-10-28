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
import { useContext } from 'react'
import { useTranslation } from 'react-i18next'
import {
  readChannelRoutingHealth,
  summarizeChannelRoutingHealth,
  type ChannelRoutingHealth,
} from '../lib/channel-routing-health'
import { isTagAggregateRow, type TagRow } from '../lib/channel-utils'
import type { Channel } from '../types'
import { ChannelHealthSnapshotContext } from './channel-health-snapshot-context'

function RecoveryNote(props: { health: ChannelRoutingHealth }) {
  const { t, i18n } = useTranslation()
  const health = props.health
  const snapshot = useContext(ChannelHealthSnapshotContext)
  const elapsed =
    snapshot.now !== null && snapshot.receivedAt !== undefined
      ? Math.max(0, snapshot.now - snapshot.receivedAt)
      : 0
  const evaluatedAt = Date.parse(health.evaluated_at) + elapsed
  const formatTime = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage || i18n.language)
  if (health.status === 'runtime_disabled') {
    if (health.disable_recovery_mode === 'manual') {
      return <span>{t('Waiting for manual recovery')}</span>
    }
    if (
      health.disabled_until &&
      Date.parse(health.disabled_until) > evaluatedAt
    ) {
      return (
        <span>
          {t('Earliest recovery check: {{time}}', {
            time: formatTime(health.disabled_until),
          })}
          {' · '}
          {t('Recovery requires a successful probe.')}
        </span>
      )
    }
    return <span>{t('Waiting for a successful recovery probe')}</span>
  }
  if (health.status !== 'penalized') return null
  if (health.penalty_recovery_mode === 'manual') {
    return <span>{t('Waiting for manual recovery')}</span>
  }
  if (
    !health.penalty_until ||
    Date.parse(health.penalty_until) <= evaluatedAt
  ) {
    return <span>{t('Waiting for a fresh runtime snapshot')}</span>
  }
  return (
    <span>
      {t('Penalty expiry in this snapshot: {{time}}', {
        time: formatTime(health.penalty_until),
      })}
    </span>
  )
}

export function ChannelRoutingHealthDetails(props: { routingHealth: unknown }) {
  const { t, i18n } = useTranslation()
  const snapshot = useContext(ChannelHealthSnapshotContext)
  const observation = readChannelRoutingHealth(props.routingHealth)
  if (observation.kind !== 'known') {
    let message = t('Runtime state unavailable')
    if (observation.kind === 'missing') {
      message = t('Runtime state display is not supported by this server.')
    } else if (observation.kind === 'unsupported') {
      message = t('Unsupported runtime state version')
    }
    return (
      <div className='text-muted-foreground space-y-1 text-xs'>
        <span>—</span>
        <p>{message}</p>
        <p>
          {t(
            'Unknown means the display could not confirm runtime state. Routing may use cached state or configured priority.'
          )}
        </p>
      </div>
    )
  }
  const health = observation.health
  let label = t('No runtime restriction')
  if (health.status === 'penalized') {
    label = t('Timeout penalty: {{points}}', { points: health.active_penalty })
  } else if (health.status === 'runtime_disabled') {
    label = t('Paused for upstream timeouts')
  } else if (health.status === 'policy_off') {
    label = t('Runtime policy is disabled')
  } else if (health.status === 'unknown') {
    label = t('Runtime state unavailable')
  }
  const formatTime = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage || i18n.language)
  const atFloor =
    health.status === 'penalized' &&
    health.base_priority >= 0 &&
    health.effective_priority === 0
  return (
    <div className='space-y-1 text-xs'>
      {snapshot.now !== null &&
        snapshot.receivedAt !== undefined &&
        snapshot.now - snapshot.receivedAt > 60_000 && (
          <p className='text-muted-foreground'>
            {t('Runtime snapshot may be outdated.')}
          </p>
        )}
      <div className='flex flex-wrap items-center gap-x-2 gap-y-1'>
        <span className='font-mono font-medium tabular-nums'>
          {health.effective_priority ?? '—'}
        </span>
        <span>{label}</span>
      </div>
      {health.status === 'unknown' && (
        <p className='text-muted-foreground'>
          {t(
            'Unknown means the display could not confirm runtime state. Routing may use cached state or configured priority.'
          )}
        </p>
      )}
      {atFloor && (
        <p className='text-muted-foreground'>
          {t('Priority is at its minimum')}
        </p>
      )}
      {(health.status === 'penalized' ||
        health.status === 'runtime_disabled') && (
        <p className='text-muted-foreground'>
          <RecoveryNote health={health} />
        </p>
      )}
      <details className='text-muted-foreground'>
        <summary className='focus-visible:ring-ring cursor-pointer rounded-sm focus-visible:ring-2'>
          {t('Routing snapshot details')}
        </summary>
        <div className='mt-1 space-y-1'>
          <p>
            {t('Configured priority')}: {health.base_priority}
          </p>
          {health.stored_penalty !== null && (
            <p>
              {t('Recorded timeout penalty: {{points}}', {
                points: health.stored_penalty,
              })}
            </p>
          )}
          {health.stored_runtime_disabled && !health.runtime_disabled && (
            <p>
              {t('The recorded timeout disable is not currently enforced.')}
            </p>
          )}
          {health.last_observed_at && (
            <p>
              {t('Last runtime event')}: {formatTime(health.last_observed_at)}
            </p>
          )}
          <p>
            {t('Snapshot time')}:{' '}
            <time dateTime={health.evaluated_at}>
              {formatTime(health.evaluated_at)}
            </time>
          </p>
          <p>
            {t(
              'This snapshot describes runtime restrictions, not upstream health.'
            )}
          </p>
          <p>
            {t(
              'Ordinary priority routing only. Node configuration, policy and runtime caches must match this snapshot; group, model, retry and weights also affect selection.'
            )}
          </p>
          <p>
            {t(
              'Snapshot time is the responding node’s calculation time, not a global state version or a record of a past request.'
            )}
          </p>
        </div>
      </details>
    </div>
  )
}

function TagChannelHealth(props: { channel: TagRow }) {
  const { t } = useTranslation()
  const summary = summarizeChannelRoutingHealth(props.channel.children)
  let value = '—'
  if (summary.multiplePriorities) value = t('Multiple effective priorities')
  else if (summary.effectivePriority !== null) {
    value = String(summary.effectivePriority)
  } else if (summary.disabled === summary.total && summary.total > 0) {
    value = t('Paused for upstream timeouts')
  }
  return (
    <div className='space-y-1 text-xs'>
      <p className='font-mono font-medium tabular-nums'>{value}</p>
      <p className='text-muted-foreground'>
        {t(
          'Current results: {{penalized}} penalized / {{disabled}} paused / {{unknown}} unknown',
          {
            penalized: summary.penalized,
            disabled: summary.disabled,
            unknown: summary.unknown,
          }
        )}
      </p>
      {summary.unknown > 0 && summary.unknown < summary.total && (
        <p>{t('Runtime state is partially unknown')}</p>
      )}
      {summary.disabled > 0 && summary.disabled < summary.total && (
        <p>{t('Includes channels paused by timeout policy')}</p>
      )}
    </div>
  )
}

export function ChannelRoutingHealthCell(props: { channel: Channel }) {
  const { t } = useTranslation()
  const snapshot = useContext(ChannelHealthSnapshotContext)
  if (snapshot.placeholder) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t(
          'Waiting for the current results. Previous runtime values are hidden.'
        )}
      </p>
    )
  }
  return (
    <div className='max-w-full min-w-0 [overflow-wrap:anywhere] whitespace-normal'>
      {isTagAggregateRow(props.channel) ? (
        <TagChannelHealth channel={props.channel} />
      ) : (
        <ChannelRoutingHealthDetails
          routingHealth={props.channel.routing_health}
        />
      )}
    </div>
  )
}
