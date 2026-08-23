import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { ModelPerformanceItem } from '../types'
import {
  sortModelPerformanceItems,
  type ModelPerformanceSortKey,
} from './model-sort'

function performanceItem(
  modelName: string,
  values: Partial<ModelPerformanceItem>
): ModelPerformanceItem {
  return {
    model_name: modelName,
    channel_count: 1,
    observed_count: 0,
    consume_log_count: 0,
    error_log_count: 0,
    consumed_quota: 0,
    retry_log_count: 0,
    latency_sample_count: 0,
    observed_success_rate: null,
    avg_logged_latency_ms: null,
    avg_tps: null,
    last_observed_at: 1,
    quality_flags: [],
    ...values,
  }
}

const rows = [
  performanceItem('Balanced model', {
    channel_count: 2,
    observed_count: 20,
    error_log_count: 2,
    consumed_quota: 800,
    observed_success_rate: 90,
    avg_logged_latency_ms: 1000,
    avg_tps: 60,
    retry_log_count: 1,
  }),
  performanceItem('Problem model', {
    channel_count: 1,
    observed_count: 5,
    error_log_count: 8,
    consumed_quota: 200,
    observed_success_rate: 40,
    avg_logged_latency_ms: 4500,
    avg_tps: 10,
    retry_log_count: 4,
  }),
  performanceItem('Unknown metrics', {
    observed_count: 10,
    error_log_count: 1,
    consumed_quota: 0,
    retry_log_count: null,
    avg_tps: null,
  }),
]

describe('sortModelPerformanceItems', () => {
  const cases: Array<{
    key: ModelPerformanceSortKey
    asc: string[]
    desc: string[]
  }> = [
    {
      key: 'model_name',
      asc: ['Balanced model', 'Problem model', 'Unknown metrics'],
      desc: ['Unknown metrics', 'Problem model', 'Balanced model'],
    },
    {
      key: 'channel_count',
      asc: ['Problem model', 'Unknown metrics', 'Balanced model'],
      desc: ['Balanced model', 'Problem model', 'Unknown metrics'],
    },
    {
      key: 'observed_count',
      asc: ['Problem model', 'Unknown metrics', 'Balanced model'],
      desc: ['Balanced model', 'Unknown metrics', 'Problem model'],
    },
    {
      key: 'error_log_count',
      asc: ['Unknown metrics', 'Balanced model', 'Problem model'],
      desc: ['Problem model', 'Balanced model', 'Unknown metrics'],
    },
    {
      key: 'consumed_quota',
      asc: ['Unknown metrics', 'Problem model', 'Balanced model'],
      desc: ['Balanced model', 'Problem model', 'Unknown metrics'],
    },
    {
      key: 'observed_success_rate',
      asc: ['Problem model', 'Balanced model', 'Unknown metrics'],
      desc: ['Balanced model', 'Problem model', 'Unknown metrics'],
    },
    {
      key: 'avg_logged_latency_ms',
      asc: ['Balanced model', 'Problem model', 'Unknown metrics'],
      desc: ['Problem model', 'Balanced model', 'Unknown metrics'],
    },
    {
      key: 'avg_tps',
      asc: ['Problem model', 'Balanced model', 'Unknown metrics'],
      desc: ['Balanced model', 'Problem model', 'Unknown metrics'],
    },
    {
      key: 'retry_log_count',
      asc: ['Balanced model', 'Problem model', 'Unknown metrics'],
      desc: ['Problem model', 'Balanced model', 'Unknown metrics'],
    },
  ]

  for (const { key, asc, desc } of cases) {
    test(`sorts ${key} in both directions and keeps missing values last`, () => {
      assert.deepEqual(
        sortModelPerformanceItems(rows, { key, direction: 'asc' }).map(
          (item) => item.model_name
        ),
        asc
      )
      assert.deepEqual(
        sortModelPerformanceItems(rows, { key, direction: 'desc' }).map(
          (item) => item.model_name
        ),
        desc
      )
    })
  }

  test('does not mutate the API result order', () => {
    const originalOrder = rows.map((item) => item.model_name)
    sortModelPerformanceItems(rows, {
      key: 'error_log_count',
      direction: 'desc',
    })
    assert.deepEqual(
      rows.map((item) => item.model_name),
      originalOrder
    )
  })
})
