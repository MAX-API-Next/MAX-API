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
import type { ChannelPerformanceData, ChannelPerformanceItem } from '../types'
import { getChannelDetailObservation } from './channel-detail'

const selected: ChannelPerformanceItem = {
  channel_id: 12,
  channel_name: 'Test channel',
  channel_type: 1,
  channel_status: 1,
  model_name: 'test-model',
  effective_group: 'default',
  observed_count: 10,
  consume_log_count: 7,
  error_log_count: 3,
  consumed_quota: 750000,
  retry_log_count: 0,
  latency_sample_count: 7,
  observed_success_rate: 70,
  avg_logged_latency_ms: 1000,
  last_observed_at: 3600,
  probe_latency_ms: 100,
  probe_test_time: 3500,
  quality_flags: ['legacy'],
}

const detail: ChannelPerformanceData = {
  storage_mode: 'legacy_log',
  source: 'log_db',
  partial: true,
  quality_flags: ['legacy', 'partial'],
  time_range: { start_at: 1, end_at: 3601, hours: 1 },
  summary: {
    channel_count: 1,
    observed_count: 4,
    consume_log_count: 3,
    error_log_count: 1,
    consumed_quota: 100,
    retry_log_count: 0,
    latency_sample_count: 3,
    observed_success_rate: 75,
    avg_logged_latency_ms: 1000,
    last_observed_at: 7200,
  },
  items: [],
  truncated: false,
  generated_at: 7200,
}

describe('channel detail observation', () => {
  test('uses channel-scoped detail evidence instead of the clicked list row', () => {
    const observation = getChannelDetailObservation(selected, detail)

    assert.equal(observation.lastObservedAt, detail.summary.last_observed_at)
    assert.deepEqual(observation.qualityFlags, detail.quality_flags)
    assert.equal(observation.probeLatencyMs, selected.probe_latency_ms)
  })
})
