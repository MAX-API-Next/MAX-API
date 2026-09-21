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
import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { TierCondition } from './billing-expr'
import {
  summarizeTierConditionGroup,
  summarizeTierConditionAlternatives,
} from './tier-condition-display'

function hour(
  op: TierCondition['op'],
  value: number,
  timezone = 'Asia/Shanghai'
): TierCondition {
  return { var: 'hour', op, value, timezone }
}

test('formats hour intervals with exact inclusive and exclusive semantics', () => {
  for (const [upperOp, end] of [
    ['<', '12:00'],
    ['<=', '13:00'],
  ] as const) {
    const source = [hour('>=', 9), hour(upperOp, 12)]
    const snapshot = structuredClone(source)
    assert.deepEqual(summarizeTierConditionGroup(source, 'en'), {
      impossible: false,
      lines: [
        { variable: 'hour', value: `09:00–${end}`, timezone: 'Asia/Shanghai' },
      ],
    })
    assert.deepEqual(source, snapshot)
  }
  assert.equal(
    summarizeTierConditionGroup([hour('>', 9.5), hour('<', 12.5)], 'en')
      .lines[0].value,
    '10:00–13:00'
  )
  assert.equal(
    summarizeTierConditionGroup([hour('>=', 22)], 'en').lines[0].value,
    '22:00–24:00'
  )
})

test('never turns incompatible AND conditions into alternative time windows', () => {
  const result = summarizeTierConditionGroup(
    [hour('>=', 9), hour('<', 12), hour('>=', 14), hour('<', 18)],
    'en'
  )
  assert.equal(result.impossible, true)
  assert.equal(result.lines[0].value, '≥ 9 ∧ < 12 ∧ ≥ 14 ∧ < 18')
})

test('shares weekday restrictions only when every OR window has identical restrictions', () => {
  const weekday: TierCondition = {
    var: 'weekday',
    op: '>=',
    value: 1,
    timezone: 'Asia/Shanghai',
  }
  const groups = [
    [hour('>=', 9), hour('<', 12), weekday],
    [hour('>=', 14), hour('<', 18), weekday],
  ]
  const snapshot = structuredClone(groups)
  const summaries = summarizeTierConditionAlternatives(groups, 'zh', '或')
  assert.equal(summaries.length, 1)
  assert.equal(summaries[0].lines[0].value, '09:00–12:00 或 14:00–18:00')
  assert.deepEqual(groups, snapshot)
  groups[1][2] = { ...weekday, value: 5 }
  assert.equal(summarizeTierConditionAlternatives(groups, 'zh', '或').length, 2)
  assert.equal(
    summarizeTierConditionAlternatives(
      [[hour('>=', 9), hour('<', 3)], groups[0]],
      'en',
      'OR'
    ).length,
    2
  )
  assert.equal(
    summarizeTierConditionAlternatives(
      [[hour('>=', 9, 'UTC')], [hour('>=', 14)]],
      'en',
      'OR'
    ).length,
    2
  )
})

test('does not intersect different timezones or drop non-time conditions', () => {
  const result = summarizeTierConditionGroup(
    [
      hour('>=', 9),
      hour('<', 3, 'UTC'),
      { var: 'len', op: '<=', value: 200001 },
    ],
    'en'
  )
  assert.equal(result.impossible, false)
  assert.deepEqual(
    result.lines.map((line) => line.value),
    ['09:00–24:00', '00:00–03:00', '≤ 200001']
  )
  assert.deepEqual(
    result.lines.map((line) => line.timezone),
    ['Asia/Shanghai', 'UTC', undefined]
  )
})

test('keeps calendar months, month days, weekdays and minutes distinct', () => {
  const result = summarizeTierConditionGroup(
    [
      { var: 'month', op: '>=', value: 9, timezone: 'UTC' },
      { var: 'day', op: '<=', value: 15, timezone: 'UTC' },
      { var: 'minute', op: '<', value: 30, timezone: 'UTC' },
      { var: 'weekday', op: '>=', value: 1, timezone: 'UTC' },
      { var: 'weekday', op: '<', value: 6, timezone: 'UTC' },
    ],
    'zh'
  )
  assert.deepEqual(
    result.lines.slice(0, 3).map((line) => line.value),
    ['9–12', '1–15', '0–29']
  )
  assert.equal(result.lines[3].value, '周一 · 周二 · 周三 · 周四 · 周五')
  assert.equal(result.impossible, false)
})

test('all summarized hour ranges agree with the original integer comparisons', () => {
  const evaluate = (
    value: number,
    op: TierCondition['op'],
    boundary: number
  ): boolean => {
    if (op === '<') return value < boundary
    if (op === '<=') return value <= boundary
    if (op === '>') return value > boundary
    return value >= boundary
  }
  for (const op of ['<', '<=', '>', '>='] as const) {
    for (const boundary of [0, 1, 9, 9.5, 12, 23, 24, 30]) {
      const summary = summarizeTierConditionGroup([hour(op, boundary)], 'en')
      const range = summary.lines[0].value.match(/^(\d+):00–(\d+):00$/)
      for (let value = 0; value < 24; value++) {
        const displayedMatch =
          !summary.impossible &&
          range !== null &&
          value >= Number(range[1]) &&
          value < Number(range[2])
        assert.equal(
          displayedMatch,
          evaluate(value, op, boundary),
          `${value} ${op} ${boundary}`
        )
      }
    }
  }
})
