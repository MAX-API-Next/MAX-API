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
import type { LatencyTimePoint, UptimeDayPoint } from '../lib/mock-stats'

export function buildLatencyChartData(series: LatencyTimePoint[]) {
  return series.map((point) => ({
    time: point.timestamp,
    group: point.group,
    ttft: point.ttft_ms,
  }))
}

export function buildUptimeChartData(series: UptimeDayPoint[]) {
  return series.map((point) => ({
    date: point.date,
    uptime: point.uptime_pct,
    incidents: point.incidents,
    outage: point.outage_minutes,
  }))
}

export function downsampleUptimeSeries(
  series: UptimeDayPoint[],
  maxPoints: number
): UptimeDayPoint[] {
  if (maxPoints <= 0 || series.length <= maxPoints) return series

  return Array.from({ length: maxPoints }, (_, index) => {
    const start = Math.floor((index * series.length) / maxPoints)
    const end = Math.max(
      start + 1,
      Math.floor(((index + 1) * series.length) / maxPoints)
    )
    let worst = series[start]
    for (let cursor = start + 1; cursor < end; cursor += 1) {
      if (series[cursor].uptime_pct < worst.uptime_pct) {
        worst = series[cursor]
      }
    }
    return worst
  })
}

export function formatChartAxisTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatChartTooltipTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function getUptimeAxisDomain(series: UptimeDayPoint[]) {
  const values = series
    .map((point) => point.uptime_pct)
    .filter((value) => Number.isFinite(value))
  if (values.length === 0) return { min: 95, max: 100 }

  const minValue = Math.min(...values)
  const maxValue = Math.max(...values)
  return {
    min: Math.min(95, Math.floor(minValue / 5) * 5),
    max: Math.max(100, Math.ceil(maxValue / 5) * 5),
  }
}
