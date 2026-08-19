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
import { describe, test } from 'node:test'
import {
  DEFAULT_FILTERS,
  DEFAULT_MODEL_FILTERS,
  DEFAULT_PERFORMANCE_HOURS,
  MAX_PERFORMANCE_HOURS,
  normalizeFilters,
  normalizePerformanceHours,
  toModelQuery,
  toQuery,
} from './filters'

describe('production performance filters', () => {
  test('defaults the initial view and invalid fallback to the latest hour', () => {
    assert.equal(DEFAULT_FILTERS.hours, '1')
    assert.equal(toQuery(DEFAULT_FILTERS).hours, DEFAULT_PERFORMANCE_HOURS)
    assert.equal(
      toQuery({ ...DEFAULT_FILTERS, hours: '' }).hours,
      DEFAULT_PERFORMANCE_HOURS
    )
  })

  test('accepts a custom integer hour range', () => {
    assert.equal(toQuery({ ...DEFAULT_FILTERS, hours: '37' }).hours, 37)
    assert.equal(
      toModelQuery({ ...DEFAULT_MODEL_FILTERS, hours: '37' }).hours,
      37
    )
  })

  test('normalizes custom hours to the supported query window', () => {
    assert.equal(normalizePerformanceHours('0'), DEFAULT_PERFORMANCE_HOURS)
    assert.equal(normalizePerformanceHours('-5'), DEFAULT_PERFORMANCE_HOURS)
    assert.equal(normalizePerformanceHours('12.8'), 12)
    assert.equal(normalizePerformanceHours('999'), MAX_PERFORMANCE_HOURS)
    assert.equal(
      normalizeFilters({ ...DEFAULT_FILTERS, hours: '999' }).hours,
      String(MAX_PERFORMANCE_HOURS)
    )
  })
})
