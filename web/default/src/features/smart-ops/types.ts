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
import type {
  PerformanceAggregate,
  PerformanceCollectionState,
  PerformanceCoverage,
  PerformanceGroup,
} from '@/features/performance-metrics/types'

export type SmartOpsAlert = {
  key: string
  status: 'firing'
  severity: 'warning'
  component: string
  node?: string
  current_value: number
  threshold: number
  observed_at: string
  message: string
}

export type SmartOpsAlertsResponse = {
  success: boolean
  message?: string
  data: SmartOpsAlert[]
}

export type BillingSettlementReconciliationItem = {
  id: number
  revision: number
  operation_key: string
  status: 'pending' | 'manual'
  source: 'wallet' | 'subscription'
  user_id: number
  subscription_id: number
  token_id: number
  task_id: number
  funding_delta: number
  applied_funding_delta: number
  token_delta: number
  applied_token_delta: number
  attempts: number
  last_error: string
  next_attempt: number
  created_at: number
  updated_at: number
  reconciliation_reviewed_at: number
  reconciliation_reviewed_by: number
  reconciliation_review_note: string
  user_blocking_override: boolean | null
  record_blocks_user: boolean
  blocks_user: boolean
}

export type BillingSettlementReconciliationData = {
  // Counts and items cover open administrator alerts only. Reviewed financial
  // records remain durable in the backend but are excluded from this view.
  total_count: number
  pending_count: number
  manual_count: number
  open_alert_count: number
  blocking_record_count: number
  blocked_user_count: number
  block_user_by_default: boolean
  oldest_created_at: number
  truncated: boolean
  generated_at: number
  items: BillingSettlementReconciliationItem[]
}

export type BillingSettlementMutationResponse = {
  success: boolean
  message?: string
  data?: Record<string, unknown>
}

export type BillingSettlementReviewTarget = {
  id: number
  revision: number
}

export type BillingSettlementReviewRequest = {
  items: BillingSettlementReviewTarget[]
}

export type BillingSettlementReconciliationResponse = {
  success: boolean
  message?: string
  data: BillingSettlementReconciliationData
}

export type ChannelPerformanceQuery = {
  hours: number
  channelId?: number
  model?: string
  group?: string
}

export type ChannelPerformanceItem = {
  channel_id: number
  channel_name: string
  channel_type: number | null
  channel_status: number | null
  model_name: string
  effective_group: string
  observed_count: number
  consume_log_count: number
  error_log_count: number
  consumed_quota: number
  retry_log_count: number | null
  latency_sample_count: number
  observed_success_rate: number | null
  avg_logged_latency_ms: number | null
  last_observed_at: number
  probe_latency_ms: number | null
  probe_test_time: number | null
  quality_flags: string[]
}

export type ChannelPerformanceData = {
  storage_mode: 'legacy_log'
  source: 'log_db'
  partial: boolean
  quality_flags: string[]
  time_range: {
    start_at: number
    end_at: number
    hours: number
  }
  summary: {
    channel_count: number
    observed_count: number
    consume_log_count: number
    error_log_count: number
    consumed_quota: number
    retry_log_count: number | null
    latency_sample_count: number
    observed_success_rate: number | null
    avg_logged_latency_ms: number | null
    last_observed_at: number
  }
  items: ChannelPerformanceItem[]
  truncated: boolean
  generated_at: number
}

export type ChannelPerformanceResponse = {
  success: boolean
  message?: string
  data: ChannelPerformanceData
}

export type ModelPerformanceQuery = {
  hours: number
  model?: string
  group?: string
}

export type ModelPerformanceItem = {
  model_name: string
  channel_count: number
  observed_count: number
  consume_log_count: number
  error_log_count: number
  consumed_quota: number
  retry_log_count: number | null
  latency_sample_count: number
  observed_success_rate: number | null
  avg_logged_latency_ms: number | null
  avg_tps: number | null
  last_observed_at: number
  quality_flags: string[]
}

export type ModelPerformanceData = {
  storage_mode: 'legacy_log'
  source: 'log_db'
  partial: boolean
  quality_flags: string[]
  time_range: {
    start_at: number
    end_at: number
    hours: number
  }
  summary: {
    model_count: number
    channel_count: number
    observed_count: number
    consume_log_count: number
    error_log_count: number
    consumed_quota: number
    retry_log_count: number | null
    latency_sample_count: number
    observed_success_rate: number | null
    avg_logged_latency_ms: number | null
    last_observed_at: number
  }
  throughput: {
    collection_state: PerformanceCollectionState
    coverage: PerformanceCoverage
  }
  items: ModelPerformanceItem[]
  truncated: boolean
  generated_at: number
}

export type ModelPerformanceResponse = {
  success: boolean
  message?: string
  data: ModelPerformanceData
}

export type ModelPerformanceDetailData = {
  model_name: string
  series_schema?: string
  summary?: PerformanceAggregate
  collection_state?: PerformanceCollectionState
  coverage?: PerformanceCoverage
  groups: PerformanceGroup[]
}

export type ModelPerformanceDetailResponse = {
  success: boolean
  message?: string
  data: ModelPerformanceDetailData
}
