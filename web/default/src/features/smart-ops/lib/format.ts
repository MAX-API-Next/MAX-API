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
import type { TFunction } from 'i18next'
import { formatLatency } from '@/features/performance-metrics/lib/format'
import type { SmartOpsAlert } from '../types'

const BILLING_BACKLOG_ALERT_KEY = 'billing_settlement_backlog'

export function formatLegacyLatency(
  milliseconds: number | null | undefined
): string {
  if (
    milliseconds == null ||
    !Number.isFinite(milliseconds) ||
    milliseconds < 0
  ) {
    return '—'
  }
  if (milliseconds === 0) return '0ms'
  return formatLatency(milliseconds)
}

export function formatPercent(value: number, locale: string): string {
  return `${new Intl.NumberFormat(locale, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(value)}%`
}

export function formatCount(value: number, locale: string): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(
    value
  )
}

export function formatLocalizedCount(
  value: number,
  locale: string,
  t: TFunction,
  singularKey: string,
  pluralKey: string
): string {
  const key =
    new Intl.PluralRules(locale).select(value) === 'one'
      ? singularKey
      : pluralKey
  return t(key, {
    count: formatCount(value, locale),
    interpolation: { escapeValue: false },
  })
}

export function formatDurationSeconds(
  value: number,
  locale: string,
  t: TFunction
): string {
  const totalSeconds = Math.max(0, Math.floor(value))
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const units: Array<[number, string, string]> = [
    [days, '{{count}} day', '{{count}} days'],
    [hours, '{{count}} hour', '{{count}} hours'],
    [minutes, '{{count}} minute', '{{count}} minutes'],
    [seconds, '{{count}} second', '{{count}} seconds'],
  ]
  const parts = units
    .filter(([count]) => count > 0)
    .slice(0, 2)
    .map(([count, singularKey, pluralKey]) =>
      formatLocalizedCount(count, locale, t, singularKey, pluralKey)
    )
  return parts.length > 0
    ? parts.join(' ')
    : formatLocalizedCount(
        0,
        locale,
        t,
        '{{count}} second',
        '{{count}} seconds'
      )
}

export function getObservedAtMilliseconds(
  alert: SmartOpsAlert
): number | undefined {
  const timestamp = Date.parse(alert.observed_at)
  return Number.isNaN(timestamp) ? undefined : timestamp
}

export function isBillingBacklogAlert(alert: SmartOpsAlert): boolean {
  return alert.key === BILLING_BACKLOG_ALERT_KEY
}

export function formatAlertCurrentValue(
  alert: SmartOpsAlert,
  locale: string,
  t: TFunction
): string {
  if (isBillingBacklogAlert(alert)) {
    return formatLocalizedCount(
      alert.current_value,
      locale,
      t,
      '{{count}} record',
      '{{count}} records'
    )
  }
  return formatPercent(alert.current_value, locale)
}

export function formatAlertThreshold(
  alert: SmartOpsAlert,
  locale: string,
  t: TFunction
): string {
  if (isBillingBacklogAlert(alert)) {
    return formatDurationSeconds(alert.threshold, locale, t)
  }
  return formatPercent(alert.threshold, locale)
}
