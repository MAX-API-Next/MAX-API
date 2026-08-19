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
import type { ChannelPerformanceItem } from '../types'
import {
  sortChannelPerformanceItems,
  type ChannelPerformanceSortKey,
} from './sort'

function performanceItem(
  channelName: string,
  values: Partial<ChannelPerformanceItem>
): ChannelPerformanceItem {
  return {
    channel_id: values.channel_id ?? 1,
    channel_name: channelName,
    channel_type: 1,
    channel_status: 1,
    model_name: 'test-model',
    effective_group: 'default',
    observed_count: 0,
    consume_log_count: 0,
    error_log_count: 0,
    consumed_quota: 0,
    retry_log_count: 0,
    latency_sample_count: 0,
    observed_success_rate: null,
    avg_logged_latency_ms: null,
    last_observed_at: 1,
    probe_latency_ms: null,
    probe_test_time: null,
    quality_flags: [],
    ...values,
    avg_tps: values.avg_tps ?? null,
  }
}

const rows = [
  performanceItem('Balanced', {
    channel_id: 1,
    observed_count: 20,
    error_log_count: 2,
    consumed_quota: 800,
    observed_success_rate: 90,
    avg_logged_latency_ms: 1000,
    retry_log_count: 1,
    probe_latency_ms: 80,
  }),
  performanceItem('Problematic', {
    channel_id: 2,
    observed_count: 5,
    error_log_count: 8,
    consumed_quota: 200,
    observed_success_rate: 40,
    avg_logged_latency_ms: 4500,
    retry_log_count: 4,
    probe_latency_ms: 900,
  }),
  performanceItem('Unknown metrics', {
    channel_id: 3,
    observed_count: 10,
    error_log_count: 1,
    consumed_quota: 0,
    retry_log_count: null,
  }),
]

describe('sortChannelPerformanceItems', () => {
  const cases: Array<{
    key: ChannelPerformanceSortKey
    asc: string[]
    desc: string[]
  }> = [
    {
      key: 'observed_count',
      asc: ['Problematic', 'Unknown metrics', 'Balanced'],
      desc: ['Balanced', 'Unknown metrics', 'Problematic'],
    },
    {
      key: 'error_log_count',
      asc: ['Unknown metrics', 'Balanced', 'Problematic'],
      desc: ['Problematic', 'Balanced', 'Unknown metrics'],
    },
    {
      key: 'consumed_quota',
      asc: ['Unknown metrics', 'Problematic', 'Balanced'],
      desc: ['Balanced', 'Problematic', 'Unknown metrics'],
    },
    {
      key: 'observed_success_rate',
      asc: ['Problematic', 'Balanced', 'Unknown metrics'],
      desc: ['Balanced', 'Problematic', 'Unknown metrics'],
    },
    {
      key: 'avg_logged_latency_ms',
      asc: ['Balanced', 'Problematic', 'Unknown metrics'],
      desc: ['Problematic', 'Balanced', 'Unknown metrics'],
    },
    {
      key: 'retry_log_count',
      asc: ['Balanced', 'Problematic', 'Unknown metrics'],
      desc: ['Problematic', 'Balanced', 'Unknown metrics'],
    },
    {
      key: 'probe_latency_ms',
      asc: ['Balanced', 'Problematic', 'Unknown metrics'],
      desc: ['Problematic', 'Balanced', 'Unknown metrics'],
    },
  ]

  for (const { key, asc, desc } of cases) {
    test(`sorts ${key} in both directions and keeps missing values last`, () => {
      assert.deepEqual(
        sortChannelPerformanceItems(rows, { key, direction: 'asc' }).map(
          (item) => item.channel_name
        ),
        asc
      )
      assert.deepEqual(
        sortChannelPerformanceItems(rows, { key, direction: 'desc' }).map(
          (item) => item.channel_name
        ),
        desc
      )
    })
  }

  test('does not mutate the API result order', () => {
    const originalOrder = rows.map((item) => item.channel_name)
    sortChannelPerformanceItems(rows, {
      key: 'error_log_count',
      direction: 'desc',
    })
    assert.deepEqual(
      rows.map((item) => item.channel_name),
      originalOrder
    )
  })
})
