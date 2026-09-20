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
import type { TierCondition } from './billing-expr'
import { getTierConditionBounds } from './tier-expr'

export const TIER_CONDITION_LABELS = {
  p: 'Input',
  c: 'Output',
  len: 'Full input length',
  hour: 'Hour',
  minute: 'Minute',
  weekday: 'Weekday',
  month: 'Month number',
  day: 'Day',
} as const

type ConditionDisplayLine = {
  variable: TierCondition['var']
  value: string
  timezone?: string
}

const operators = { '>=': '≥', '<=': '≤', '>': '>', '<': '<' }

function matches(value: number, condition: TierCondition): boolean {
  switch (condition.op) {
    case '>=':
      return value >= condition.value
    case '>':
      return value > condition.value
    case '<=':
      return value <= condition.value
    case '<':
      return value < condition.value
  }
}

function weekdayName(day: number, locale: string): string {
  const date = new Date(Date.UTC(2024, 0, 7 + day)) // Sunday + weekday index
  try {
    return new Intl.DateTimeFormat(locale, {
      weekday: 'short',
      timeZone: 'UTC',
    }).format(date)
  } catch {
    return new Intl.DateTimeFormat('en', {
      weekday: 'short',
      timeZone: 'UTC',
    }).format(date)
  }
}

/** Read-only summaries of an AND group. Never merge groups or rewrite rules. */
export function summarizeTierConditionGroup(
  conditions: TierCondition[],
  locale: string
): {
  lines: ConditionDisplayLine[]
  impossible: boolean
} {
  const dimensions = new Map<string, TierCondition[]>()
  for (const condition of conditions) {
    const key = JSON.stringify([condition.var, condition.timezone])
    dimensions.set(key, [...(dimensions.get(key) ?? []), condition])
  }
  let impossible = false
  const lines = Array.from(dimensions.values()).map(
    (constraints): ConditionDisplayLine => {
      const first = constraints[0]
      const rawValue = constraints
        .map((condition) => `${operators[condition.op]} ${condition.value}`)
        .join(' ∧ ')
      const line: ConditionDisplayLine = {
        variable: first.var,
        value: rawValue,
        ...(first.timezone !== undefined ? { timezone: first.timezone } : {}),
      }
      const bounds = getTierConditionBounds(first.var)
      if (!bounds) return line

      // Time helpers return integers. Intersect their bounded domains to preserve
      // < versus <=, fractional boundaries and contradictory conditions exactly.
      const allowed = Array.from(
        { length: bounds.max - bounds.min + 1 },
        (_, i) => bounds.min + i
      ).filter((value) =>
        constraints.every((condition) => matches(value, condition))
      )
      if (!allowed.length) {
        impossible = true
        return line
      }
      const start = allowed[0]
      const end = allowed[allowed.length - 1]
      if (first.var === 'hour') {
        line.value = `${String(start).padStart(2, '0')}:00–${String(end + 1).padStart(2, '0')}:00`
      } else if (first.var === 'weekday') {
        line.value = allowed.map((day) => weekdayName(day, locale)).join(' · ')
      } else {
        line.value = start === end ? String(start) : `${start}–${end}`
      }
      return line
    }
  )
  return { lines, impossible }
}

/** Factor identical restrictions in alternative hour windows, and nothing else. */
export function summarizeTierConditionAlternatives(
  groups: TierCondition[][],
  locale: string,
  orLabel: string
): ReturnType<typeof summarizeTierConditionGroup>[] {
  const summaries = groups.map((group) =>
    summarizeTierConditionGroup(group, locale)
  )
  if (summaries.length < 2 || summaries.some((summary) => summary.impossible))
    return summaries
  const first = summaries[0]
  const hours = first.lines.filter((line) => line.variable === 'hour')
  if (hours.length !== 1) return summaries
  const common = JSON.stringify(
    first.lines.filter((line) => line.variable !== 'hour')
  )
  const windows: string[] = []
  for (const summary of summaries) {
    const hourLines = summary.lines.filter((line) => line.variable === 'hour')
    if (
      hourLines.length !== 1 ||
      hourLines[0].timezone !== hours[0].timezone ||
      JSON.stringify(
        summary.lines.filter((line) => line.variable !== 'hour')
      ) !== common
    )
      return summaries
    windows.push(hourLines[0].value)
  }
  return [
    {
      impossible: false,
      lines: first.lines.map((line) =>
        line.variable === 'hour'
          ? { ...line, value: [...new Set(windows)].join(` ${orLabel} `) }
          : line
      ),
    },
  ]
}
