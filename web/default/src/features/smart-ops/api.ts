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
import { api } from '@/lib/api'
import { MAX_PERFORMANCE_LIMIT } from './lib/filters'
import type {
  BillingSettlementReconciliationResponse,
	BillingSettlementMutationResponse,
	BillingSettlementReviewRequest,
  ChannelPerformanceData,
  ChannelPerformanceQuery,
  ChannelPerformanceResponse,
  ModelPerformanceData,
  ModelPerformanceDetailData,
  ModelPerformanceDetailResponse,
  ModelPerformanceQuery,
  ModelPerformanceResponse,
  SmartOpsAlertsResponse,
} from './types'

export async function getSmartOpsAlerts(): Promise<SmartOpsAlertsResponse> {
  const response = await api.get<SmartOpsAlertsResponse>(
    '/api/smart-ops/alerts'
  )
  return response.data
}

export async function getBillingSettlementReconciliation(): Promise<BillingSettlementReconciliationResponse> {
  const response = await api.get<BillingSettlementReconciliationResponse>(
    '/api/smart-ops/billing-settlements'
  )
  return response.data
}

export async function updateBillingSettlementBlockingPolicy(
  blockUserByDefault: boolean
): Promise<BillingSettlementMutationResponse> {
  const response = await api.put<BillingSettlementMutationResponse>(
    '/api/smart-ops/billing-settlements/blocking-policy',
    { block_user_by_default: blockUserByDefault },
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return response.data
}

export async function reviewBillingSettlement(
  id: number,
  request: BillingSettlementReviewRequest
): Promise<BillingSettlementMutationResponse> {
  const response = await api.post<BillingSettlementMutationResponse>(
    `/api/smart-ops/billing-settlements/${id}/review`,
    request,
    { skipBusinessError: true, skipErrorHandler: true }
  )
  return response.data
}

export async function getChannelPerformance(
  query: ChannelPerformanceQuery
): Promise<ChannelPerformanceData> {
  const response = await api.get<ChannelPerformanceResponse>(
    '/api/smart-ops/channel-performance',
    {
      params: {
        hours: query.hours,
        limit: MAX_PERFORMANCE_LIMIT,
        channel_id: query.channelId,
        model: query.model || undefined,
        group: query.group || undefined,
      },
    }
  )
  return response.data.data
}

export async function getChannelPerformanceDetail(
  channelId: number
): Promise<ChannelPerformanceData> {
  const response = await api.get<ChannelPerformanceResponse>(
    '/api/smart-ops/channel-performance/detail',
    {
      params: { channel_id: channelId },
    }
  )
  return response.data.data
}

export async function getModelPerformanceDetail(
  modelName: string
): Promise<ModelPerformanceDetailData> {
  const response = await api.get<ModelPerformanceDetailResponse>(
    '/api/smart-ops/model-performance/detail',
    {
      params: { model: modelName },
    }
  )
  return response.data.data
}

export async function getModelPerformance(
  query: ModelPerformanceQuery
): Promise<ModelPerformanceData> {
  const response = await api.get<ModelPerformanceResponse>(
    '/api/smart-ops/model-performance',
    {
      params: {
        hours: query.hours,
        limit: MAX_PERFORMANCE_LIMIT,
        model: query.model || undefined,
        group: query.group || undefined,
      },
    }
  )
  return response.data.data
}
