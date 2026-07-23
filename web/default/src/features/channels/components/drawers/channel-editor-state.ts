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
import type { ChannelFormValues } from '../../lib'
import type { ChannelEditorSectionStatus } from './channel-editor-navigation'

export type ModelMappingGuardrail = {
  invalidJson: boolean
  entries: Array<{ source: string; target: string }>
  missingSourceModels: string[]
  exposedTargetModels: string[]
}

// Helper functions
export const createEmptyModelMappingGuardrail = (): ModelMappingGuardrail => ({
  invalidJson: false,
  entries: [],
  missingSourceModels: [],
  exposedTargetModels: [],
})

export function getModelMappingGuardrail(
  modelMapping: string | undefined,
  publishedModels: string[]
): ModelMappingGuardrail {
  if (!modelMapping?.trim()) {
    return createEmptyModelMappingGuardrail()
  }

  try {
    const parsed: unknown = JSON.parse(modelMapping)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
    }

    let hasInvalidTarget = false
    const entries = Object.entries(parsed).reduce<
      Array<{ source: string; target: string }>
    >((acc, [rawSource, rawTarget]) => {
      const source = String(rawSource).trim()
      if (typeof rawTarget !== 'string') {
        hasInvalidTarget = true
        return acc
      }
      const target = rawTarget.trim()

      if (source && target) {
        acc.push({ source, target })
      }
      return acc
    }, [])

    if (hasInvalidTarget) {
      return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
    }

    const publishedModelSet = new Set(publishedModels)
    const missingSourceModels = Array.from(
      new Set(
        entries
          .filter((entry) => !publishedModelSet.has(entry.source))
          .map((entry) => entry.source)
      )
    )
    const exposedTargetModels = Array.from(
      new Set(
        entries
          .filter((entry) => publishedModelSet.has(entry.target))
          .map((entry) => entry.target)
      )
    )

    return {
      invalidJson: false,
      entries,
      missingSourceModels,
      exposedTargetModels,
    }
  } catch {
    return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
  }
}

export const formatModelNames = (models: string[]): string =>
  models.map((model) => `"${model}"`).join(', ')

export const MODEL_MAPPING_PREVIEW_FALLBACK: Array<{
  source: string
  target: string
}> = [{ source: 'client-model', target: 'upstream-model' }]

export const ADVANCED_SETTINGS_EXPANDED_KEY =
  'channel-advanced-settings-expanded'
export const CHANNEL_EDITOR_SECTION_IDS = {
  identity: 'channel-section-identity',
  apiAccess: 'channel-section-api-access',
  models: 'channel-section-models',
  advanced: 'channel-section-advanced',
} as const
export const CHANNEL_EDITOR_MAIN_SECTION_IDS = [
  CHANNEL_EDITOR_SECTION_IDS.identity,
  CHANNEL_EDITOR_SECTION_IDS.apiAccess,
  CHANNEL_EDITOR_SECTION_IDS.models,
  CHANNEL_EDITOR_SECTION_IDS.advanced,
]
export const ADVANCED_SETTINGS_SECTION_IDS = {
  routingStrategy: 'channel-section-advanced-routing-strategy',
  internalNotes: 'channel-section-advanced-internal-notes',
  overrideRules: 'channel-section-advanced-override-rules',
  videoTaskProtocol: 'channel-section-advanced-video-task-protocol',
  videoTaskBilling: 'channel-section-advanced-video-task-billing',
  responseMapping: 'channel-section-advanced-response-mapping',
  fieldPassthrough: 'channel-section-advanced-field-passthrough',
  extraSettings: 'channel-section-advanced-extra-settings',
  upstreamModelDetection: 'channel-section-advanced-upstream-model-detection',
} as const
export const ADVANCED_SETTINGS_CHILD_SECTION_IDS: string[] = Object.values(
  ADVANCED_SETTINGS_SECTION_IDS
)
export const ADVANCED_ERROR_FIELDS = [
  'priority',
  'weight',
  'test_model',
  'auto_ban',
  'status_code_mapping',
  'tag',
  'remark',
  'param_override',
  'header_override',
  'force_format',
  'thinking_to_content',
  'proxy',
  'pass_through_body_enabled',
  'system_prompt',
  'system_prompt_override',
  'allow_service_tier',
  'disable_store',
  'allow_safety_identifier',
  'allow_include_obfuscation',
  'allow_inference_geo',
  'allow_speed',
  'claude_beta_query',
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
  'video_task_delta_settlement_enabled',
  'video_task_path_override_enabled',
  'video_task_protocol_enabled',
  'video_task_submit_path',
  'video_task_query_path',
  'video_task_task_id_path',
  'video_task_status_path',
  'video_task_progress_path',
  'video_task_result_url_paths',
  'video_task_error_message_path',
  'video_task_status_submitted',
  'video_task_status_queued',
  'video_task_status_running',
  'video_task_status_succeeded',
  'video_task_status_failed',
] satisfies (keyof ChannelFormValues)[]
export const UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT = 8

export function readAdvancedSettingsPreference(): boolean {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(ADVANCED_SETTINGS_EXPANDED_KEY) === 'true'
}

export function hasConfiguredOverrideValue(value: unknown): boolean {
  if (typeof value !== 'string') return false

  const trimmed = value.trim()
  if (!trimmed || trimmed === 'null') return false

  try {
    const parsed = JSON.parse(trimmed)
    if (parsed === null) return false
    if (Array.isArray(parsed)) return parsed.length > 0
    if (typeof parsed === 'object') return Object.keys(parsed).length > 0
  } catch {
    return true
  }

  return true
}

export function hasAdvancedSettingsValues(values: ChannelFormValues): boolean {
  return Boolean(
    values.model_mapping?.trim() ||
    values.advanced_custom?.trim() ||
    hasConfiguredOverrideValue(values.param_override) ||
    hasConfiguredOverrideValue(values.header_override) ||
    hasConfiguredOverrideValue(values.status_code_mapping) ||
    values.tag?.trim() ||
    values.remark?.trim() ||
    values.priority ||
    values.weight ||
    values.proxy?.trim() ||
    values.system_prompt?.trim() ||
    values.force_format ||
    values.thinking_to_content ||
    values.pass_through_body_enabled ||
    values.system_prompt_override ||
    values.claude_beta_query ||
    values.video_task_delta_settlement_enabled === false ||
    values.video_task_path_override_enabled ||
    values.video_task_protocol_enabled ||
    values.upstream_model_update_check_enabled ||
    values.upstream_model_update_auto_sync_enabled ||
    values.upstream_model_update_ignored_models?.trim()
  )
}

export function parseSettingsRecord(
  settings: string | undefined
): Record<string, unknown> {
  if (!settings?.trim()) return {}
  try {
    const parsed = JSON.parse(settings)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

export function formatUnixTime(timestamp: unknown): string {
  const seconds = Number(timestamp)
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  return new Date(seconds * 1000).toLocaleString()
}

export function getCompletionStatus(
  hasErrors: boolean,
  isComplete: boolean
): ChannelEditorSectionStatus {
  if (hasErrors) return 'error'
  if (isComplete) return 'complete'
  return 'idle'
}

export function getSectionStatusLabel(
  status: ChannelEditorSectionStatus,
  t: (key: string) => string
): string {
  if (status === 'error') return t('Error')
  if (status === 'complete' || status === 'configured') return t('Ready')
  return t('Incomplete')
}
