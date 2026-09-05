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
import type {
  H3BillingPreview,
  H3BillingPreviewScenario,
  H3BillingProfile,
} from './billing/h3-billing-utils'
import type {
  ConfirmPaymentComplianceResponse,
  FetchUpstreamRatiosRequest,
  LogCleanupTask,
  SystemOptionsResponse,
  SystemTaskListResponse,
  SystemTaskResponse,
  UpdateOptionRequest,
  UpdateOptionResponse,
  UpstreamChannelsResponse,
  UpstreamRatiosResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  return res.data
}

export async function previewH3Billing(request: {
  profile: H3BillingProfile
  scenario: H3BillingPreviewScenario
  groupRatio: number
}) {
  const res = await api.post<{
    success: boolean
    message: string
    data?: H3BillingPreview
  }>('/api/option/h3_billing/preview', {
    profile: request.profile,
    resolution: request.scenario.resolution,
    output_duration_seconds: request.scenario.outputDurationSeconds,
    input_video_count: request.scenario.inputVideoCount,
    input_audio_count: request.scenario.inputAudioCount,
    input_image_count: request.scenario.inputImageCount,
    group_ratio: request.groupRatio,
    actual: request.scenario.actual
      ? {
          output_duration_ms: request.scenario.actual.outputDurationMs,
          input_video_duration_ms: request.scenario.actual.inputVideoDurationMs,
          input_audio_duration_ms: request.scenario.actual.inputAudioDurationMs,
          input_image_count: request.scenario.actual.inputImageCount,
        }
      : undefined,
  })
  return res.data
}

export async function updateTieredBillingConfig(request: {
  config: Record<string, { enabled: boolean; expr: string }>
}) {
  const res = await api.put<UpdateOptionResponse>(
    '/api/option/tiered_billing',
    request
  )
  return res.data
}

export async function confirmPaymentCompliance() {
  const res = await api.post<ConfirmPaymentComplianceResponse>(
    '/api/option/payment_compliance',
    { confirmed: true }
  )
  return res.data
}

export async function startLogCleanupTask(targetTimestamp: number) {
  const res = await api.post<SystemTaskResponse<LogCleanupTask>>(
    '/api/system-task/log-cleanup',
    null,
    {
      params: { target_timestamp: targetTimestamp },
    }
  )
  return res.data
}

export async function getCurrentLogCleanupTask() {
  const res = await api.get<SystemTaskResponse<LogCleanupTask | null>>(
    '/api/system-task/log-cleanup/current'
  )
  return res.data
}

export async function getLogCleanupTask(taskId: string) {
  const res = await api.get<SystemTaskResponse<LogCleanupTask>>(
    `/api/system-task/log-cleanup/${taskId}`
  )
  return res.data
}

export async function listSystemTasks(limit = 20) {
  const res = await api.get<SystemTaskListResponse>('/api/system-task/list', {
    params: { limit },
  })
  return res.data
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio'
  )
  return res.data
}

export async function getUpstreamChannels() {
  const res = await api.get<UpstreamChannelsResponse>(
    '/api/ratio_sync/channels'
  )
  return res.data
}

export async function fetchUpstreamRatios(request: FetchUpstreamRatiosRequest) {
  const res = await api.post<UpstreamRatiosResponse>(
    '/api/ratio_sync/fetch',
    request
  )
  return res.data
}
