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
import { z } from 'zod'
import {
  CHANNEL_STATUS,
  CHANNEL_TYPE_DOUBAO_VIDEO,
  CHANNEL_TYPE_VOLCENGINE,
  CHANNEL_TYPE_XUNFEI,
  ERROR_MESSAGES,
  MODEL_FETCHABLE_TYPES,
} from '../constants'
import type { Channel } from '../types'
import {
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  advancedCustomConfigUsesRelativeUpstreamPath,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
} from './advanced-custom'
import { VIDEO_TASK_CHANNEL_TYPES } from './channel-capabilities'
import {
  BASE_URL_REQUIRED_TYPES,
  OTHER_REQUIRED_TYPES,
  hasVertexDefaultRegion,
  hasVideoTaskQueryPlaceholder,
} from './channel-config-rules'

const VIDEO_TASK_PROTOCOL = 'generic_video_task'
const CONFIGURED_VIDEO_TASK_PROTOCOLS = new Set([VIDEO_TASK_PROTOCOL])
const DEFAULT_VIDEO_TASK_SUBMIT_PATH = '/v1/videos/create'
const DEFAULT_VIDEO_TASK_QUERY_PATH = '/v1/videos/{task_id}'
const DEFAULT_VIDEO_TASK_TASK_ID_PATH = 'task_id'
const DEFAULT_VIDEO_TASK_STATUS_PATH = 'status'
const DEFAULT_VIDEO_TASK_PROGRESS_PATH = 'progress'
const DEFAULT_VIDEO_TASK_CREATED_AT_PATH = 'created_at'
const DEFAULT_VIDEO_TASK_UPDATED_AT_PATH = 'updated_at'
const DEFAULT_VIDEO_TASK_RESULT_URL_PATHS =
  'result.primary_url,result.urls.0,result.url,result.video_url,result.output_url,data.result.primary_url,data.result.urls.0,data.result.url,data.result.video_url,data.result.output_url,url,video_url,output_url,file_url,download_url,result'
const DEFAULT_VIDEO_TASK_ERROR_MESSAGE_PATH = 'error_message'
const DEFAULT_VIDEO_TASK_STATUS_SUBMITTED = 'submitted,created'
const DEFAULT_VIDEO_TASK_STATUS_QUEUED = 'queued,pending'
const DEFAULT_VIDEO_TASK_STATUS_RUNNING = 'running,processing,in_progress'
const DEFAULT_VIDEO_TASK_STATUS_SUCCEEDED =
  'succeeded,success,completed,complete'
const DEFAULT_VIDEO_TASK_STATUS_FAILED = 'failed,failure,error'

// ============================================================================
// Form Validation Schema
// ============================================================================

function parseOptionalJson(value: string | undefined): unknown {
  if (!value?.trim()) return undefined
  return JSON.parse(value)
}

function isJsonObjectValue(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOptionalJsonObject(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    return parsed === undefined || isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function isOptionalModelMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.values(parsed).every((item) => typeof item === 'string')
  } catch {
    return false
  }
}

function isOptionalStatusCodeMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    const seenSourceCodes = new Set<number>()
    return Object.entries(parsed).every(([from, to]) => {
      const normalizedFrom = from.trim()
      const fromCode = Number(from)
      const toCode = Number(to)
      const valid =
        /^[1-5]\d{2}$/.test(normalizedFrom) &&
        Number.isInteger(fromCode) &&
        Number.isInteger(toCode) &&
        fromCode >= 100 &&
        fromCode <= 599 &&
        toCode >= 100 &&
        toCode <= 599 &&
        !seenSourceCodes.has(fromCode)
      seenSourceCodes.add(fromCode)
      return valid
    })
  } catch {
    return false
  }
}

function isCodexCredential(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    return (
      isJsonObjectValue(parsed) &&
      typeof parsed.access_token === 'string' &&
      parsed.access_token.trim().length > 0 &&
      typeof parsed.account_id === 'string' &&
      parsed.account_id.trim().length > 0
    )
  } catch {
    return false
  }
}

function isVertexJsonKey(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (Array.isArray(parsed)) {
      return parsed.every((item) => isJsonObjectValue(item))
    }
    return isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function isVertexRegionConfig(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    return hasVertexDefaultRegion(parsed)
  } catch {
    return false
  }
}

function addRequiredIssue(
  ctx: z.RefinementCtx,
  path: string,
  message: string
): void {
  ctx.addIssue({
    code: z.ZodIssueCode.custom,
    path: [path],
    message,
  })
}

export const channelFormSchema = z
  .object({
    name: z.string().min(1, ERROR_MESSAGES.REQUIRED_NAME),
    type: z.number().min(0, ERROR_MESSAGES.REQUIRED_TYPE),
    base_url: z.string().optional(),
    key: z.string(),
    openai_organization: z.string().optional(),
    models: z.string().min(1, ERROR_MESSAGES.REQUIRED_MODELS),
    group: z.array(z.string()).min(1, ERROR_MESSAGES.REQUIRED_GROUP),
    model_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalModelMapping,
        'Model mapping must be a JSON object with string values'
      ),
    priority: z.number().optional(),
    weight: z.number().optional(),
    test_model: z.string().optional(),
    auto_ban: z.number().optional(),
    status: z.number(),
    status_code_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalStatusCodeMapping,
        'Status code mapping must use valid HTTP status codes'
      ),
    tag: z.string().optional(),
    remark: z
      .string()
      .max(255, 'Remark must be less than 255 characters')
      .optional(),
    setting: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    param_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    header_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    settings: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    advanced_custom: z.string().optional(),
    other: z.string().optional(),
    // Multi-key options (not sent to backend directly)
    multi_key_mode: z.enum(['single', 'batch', 'multi_to_single']).optional(),
    multi_key_type: z.enum(['random', 'polling']).optional(),
    batch_add_set_key_prefix_2_name: z.boolean().optional(),
    key_mode: z.enum(['append', 'replace']).optional(), // For editing multi-key channels
    // Channel extra settings (stored in setting JSON, not sent directly)
    force_format: z.boolean().optional(),
    thinking_to_content: z.boolean().optional(),
    proxy: z.string().optional(),
    pass_through_body_enabled: z.boolean().optional(),
    system_prompt: z.string().optional(),
    system_prompt_override: z.boolean().optional(),
    // Type-specific settings (stored in settings JSON)
    is_enterprise_account: z.boolean().optional(), // OpenRouter specific
    vertex_key_type: z.enum(['json', 'api_key']).optional(), // Vertex AI specific
    aws_key_type: z.enum(['ak_sk', 'api_key']).optional(), // AWS specific
    azure_responses_version: z.string().optional(), // Azure specific
    // Field passthrough controls (stored in settings JSON)
    allow_service_tier: z.boolean().optional(), // OpenAI/Anthropic
    disable_store: z.boolean().optional(), // OpenAI only
    allow_safety_identifier: z.boolean().optional(), // OpenAI only
    allow_include_obfuscation: z.boolean().optional(), // OpenAI: include usage obfuscation
    allow_inference_geo: z.boolean().optional(), // OpenAI/Anthropic: inference geography
    allow_speed: z.boolean().optional(), // Anthropic: speed mode control
    claude_beta_query: z.boolean().optional(), // Anthropic: beta query passthrough
    // Upstream model update settings (stored in settings JSON)
    upstream_model_update_check_enabled: z.boolean().optional(),
    upstream_model_update_auto_sync_enabled: z.boolean().optional(),
    upstream_model_update_ignored_models: z.string().optional(),
    // Video task protocol settings (stored in settings JSON)
    video_task_path_override_enabled: z.boolean().optional(),
    video_task_protocol_enabled: z.boolean().optional(),
    video_task_delta_settlement_enabled: z.boolean().optional(),
    video_task_submit_path: z.string().optional(),
    video_task_query_path: z.string().optional(),
    video_task_task_id_path: z.string().optional(),
    video_task_status_path: z.string().optional(),
    video_task_progress_path: z.string().optional(),
    video_task_created_at_path: z.string().optional(),
    video_task_updated_at_path: z.string().optional(),
    video_task_result_url_paths: z.string().optional(),
    video_task_error_message_path: z.string().optional(),
    video_task_status_submitted: z.string().optional(),
    video_task_status_queued: z.string().optional(),
    video_task_status_running: z.string().optional(),
    video_task_status_succeeded: z.string().optional(),
    video_task_status_failed: z.string().optional(),
  })
  .superRefine((data, ctx) => {
    if (BASE_URL_REQUIRED_TYPES.has(data.type) && !data.base_url?.trim()) {
      addRequiredIssue(
        ctx,
        'base_url',
        'Base URL is required for this channel type'
      )
    }

    if (OTHER_REQUIRED_TYPES.has(data.type) && !data.other?.trim()) {
      addRequiredIssue(
        ctx,
        'other',
        'This channel type requires additional configuration'
      )
    }

    if (data.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
      const advancedCustomConfig = parseAdvancedCustomConfig(
        data.advanced_custom
      )
      const advancedCustomError =
        validateAdvancedCustomConfig(advancedCustomConfig)
      if (advancedCustomError) {
        addRequiredIssue(ctx, 'advanced_custom', advancedCustomError.message)
      }
      if (
        advancedCustomConfigUsesRelativeUpstreamPath(advancedCustomConfig) &&
        !data.base_url?.trim()
      ) {
        addRequiredIssue(
          ctx,
          'base_url',
          'Base URL is required when a route upstream path is relative'
        )
      }
    }

    if (data.type === 57) {
      if (data.multi_key_mode && data.multi_key_mode !== 'single') {
        addRequiredIssue(
          ctx,
          'multi_key_mode',
          'Codex channels do not support batch creation'
        )
      }
      if (data.key?.trim() && !isCodexCredential(data.key)) {
        addRequiredIssue(
          ctx,
          'key',
          'Codex credential must be a JSON object with access_token and account_id'
        )
      }
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'json' &&
      data.key?.trim() &&
      !isVertexJsonKey(data.key)
    ) {
      addRequiredIssue(
        ctx,
        'key',
        'Vertex AI service account key must be valid JSON'
      )
    }

    if (
      data.type === 41 &&
      data.other?.trim() &&
      !isVertexRegionConfig(data.other)
    ) {
      addRequiredIssue(
        ctx,
        'other',
        'Vertex AI region configuration must include a default region.'
      )
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'api_key' &&
      data.multi_key_mode &&
      data.multi_key_mode !== 'single'
    ) {
      addRequiredIssue(
        ctx,
        'multi_key_mode',
        'Vertex AI API Key mode does not support batch creation'
      )
    }

    const usesVideoTaskPaths =
      VIDEO_TASK_CHANNEL_TYPES.has(data.type) &&
      (data.video_task_path_override_enabled ||
        data.video_task_protocol_enabled)

    if (usesVideoTaskPaths) {
      if (!data.video_task_submit_path?.trim()) {
        addRequiredIssue(
          ctx,
          'video_task_submit_path',
          'Submit path is required'
        )
      }
      const queryPath = data.video_task_query_path?.trim()
      if (!queryPath) {
        addRequiredIssue(ctx, 'video_task_query_path', 'Query path is required')
      } else if (!hasVideoTaskQueryPlaceholder(queryPath)) {
        addRequiredIssue(
          ctx,
          'video_task_query_path',
          'Query path must include one of {task_id}, {operation_name}, or {upstream_task_id}'
        )
      }
    }
  })

export type ChannelFormValues = z.infer<typeof channelFormSchema>

// ============================================================================
// Default Form Values
// ============================================================================

export const CHANNEL_FORM_DEFAULT_VALUES: ChannelFormValues = {
  name: '',
  type: 1,
  base_url: '',
  key: '',
  openai_organization: '',
  models: '',
  group: ['default'],
  model_mapping: '',
  priority: 0,
  weight: 0,
  test_model: '',
  auto_ban: 1,
  status: CHANNEL_STATUS.ENABLED,
  status_code_mapping: '',
  tag: '',
  remark: '',
  setting: '',
  param_override: '',
  header_override: '',
  settings: '{}',
  advanced_custom: '',
  other: '',
  multi_key_mode: 'single',
  multi_key_type: 'random',
  batch_add_set_key_prefix_2_name: false,
  key_mode: 'append',
  // Channel extra settings
  force_format: false,
  thinking_to_content: false,
  proxy: '',
  pass_through_body_enabled: false,
  system_prompt: '',
  system_prompt_override: false,
  // Type-specific settings
  is_enterprise_account: false,
  vertex_key_type: 'json',
  aws_key_type: 'ak_sk',
  azure_responses_version: '',
  // Field passthrough controls
  allow_service_tier: false,
  disable_store: false,
  allow_safety_identifier: false,
  allow_include_obfuscation: false,
  allow_inference_geo: false,
  allow_speed: false,
  claude_beta_query: false,
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
  video_task_path_override_enabled: false,
  video_task_protocol_enabled: false,
  video_task_delta_settlement_enabled: true,
  video_task_submit_path: DEFAULT_VIDEO_TASK_SUBMIT_PATH,
  video_task_query_path: DEFAULT_VIDEO_TASK_QUERY_PATH,
  video_task_task_id_path: DEFAULT_VIDEO_TASK_TASK_ID_PATH,
  video_task_status_path: DEFAULT_VIDEO_TASK_STATUS_PATH,
  video_task_progress_path: DEFAULT_VIDEO_TASK_PROGRESS_PATH,
  video_task_created_at_path: DEFAULT_VIDEO_TASK_CREATED_AT_PATH,
  video_task_updated_at_path: DEFAULT_VIDEO_TASK_UPDATED_AT_PATH,
  video_task_result_url_paths: DEFAULT_VIDEO_TASK_RESULT_URL_PATHS,
  video_task_error_message_path: DEFAULT_VIDEO_TASK_ERROR_MESSAGE_PATH,
  video_task_status_submitted: DEFAULT_VIDEO_TASK_STATUS_SUBMITTED,
  video_task_status_queued: DEFAULT_VIDEO_TASK_STATUS_QUEUED,
  video_task_status_running: DEFAULT_VIDEO_TASK_STATUS_RUNNING,
  video_task_status_succeeded: DEFAULT_VIDEO_TASK_STATUS_SUCCEEDED,
  video_task_status_failed: DEFAULT_VIDEO_TASK_STATUS_FAILED,
}

export function getChannelTypeScopedFieldDefaults(
  type: number
): Pick<ChannelFormValues, 'base_url' | 'other'> {
  return {
    base_url:
      type === CHANNEL_TYPE_VOLCENGINE
        ? 'https://ark.cn-beijing.volces.com'
        : '',
    other: type === CHANNEL_TYPE_XUNFEI ? 'v2.1' : '',
  }
}

function splitCSV(value: string | undefined, fallback = ''): string[] {
  const source = value?.trim() ? value : fallback
  return String(source || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function joinCSV(values: unknown, fallback: string): string {
  if (Array.isArray(values)) {
    const items = values
      .map((item) => String(item || '').trim())
      .filter(Boolean)
    return items.length > 0 ? items.join(',') : fallback
  }
  if (typeof values === 'string' && values.trim()) {
    return values.trim()
  }
  return fallback
}

function normalizeTaskStatus(value: unknown): string {
  const status = String(value || '')
    .trim()
    .toUpperCase()
  switch (status) {
    case 'SUBMITTED':
      return 'SUBMITTED'
    case 'QUEUED':
    case 'PENDING':
      return 'QUEUED'
    case 'IN_PROGRESS':
    case 'RUNNING':
    case 'PROCESSING':
      return 'IN_PROGRESS'
    case 'SUCCESS':
    case 'SUCCEEDED':
    case 'COMPLETED':
      return 'SUCCESS'
    case 'FAILURE':
    case 'FAILED':
      return 'FAILURE'
    default:
      return ''
  }
}

function getVideoTaskStatusList(
  statusMap: unknown,
  targetStatus: string,
  fallback: string
): string {
  if (!isJsonObjectValue(statusMap)) return fallback
  const values = Object.entries(statusMap)
    .filter(([, value]) => normalizeTaskStatus(value) === targetStatus)
    .map(([key]) => key.trim())
    .filter(Boolean)
  return values.length > 0 ? values.join(',') : fallback
}

function setVideoTaskStatusMap(
  statusMap: Record<string, string>,
  statuses: string | undefined,
  targetStatus: string
): void {
  for (const status of splitCSV(statuses)) {
    statusMap[status] = targetStatus
  }
}

// ============================================================================
// Transform Functions
// ============================================================================

/**
 * Transform Channel from API to Form default values
 */
export function transformChannelToFormDefaults(
  channel: Channel
): ChannelFormValues {
  // Parse channel extra settings from setting field
  let extraSettings = {
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    pass_through_body_enabled: false,
    system_prompt: '',
    system_prompt_override: false,
  }

  if (channel.setting) {
    try {
      const parsed = JSON.parse(channel.setting)
      extraSettings = {
        force_format: parsed.force_format || false,
        thinking_to_content: parsed.thinking_to_content || false,
        proxy: parsed.proxy || '',
        pass_through_body_enabled: parsed.pass_through_body_enabled || false,
        system_prompt: parsed.system_prompt || '',
        system_prompt_override: parsed.system_prompt_override || false,
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel setting:', error)
    }
  }

  // Parse type-specific settings from settings field
  let vertexKeyType: 'json' | 'api_key' = 'json'
  let azureResponsesVersion = ''
  let isEnterpriseAccount = false
  let awsKeyType: 'ak_sk' | 'api_key' = 'ak_sk'
  let allowServiceTier = false
  let disableStore = false
  let allowSafetyIdentifier = false
  let allowIncludeObfuscation = false
  let allowInferenceGeo = false
  let allowSpeed = false
  let claudeBetaQuery = false
  let upstreamModelUpdateCheckEnabled = false
  let upstreamModelUpdateAutoSyncEnabled = false
  let upstreamModelUpdateIgnoredModels = ''
  let videoTaskPathOverrideEnabled = false
  let videoTaskProtocolEnabled = false
  let videoTaskDeltaSettlementEnabled = true
  let videoTaskSubmitPath = DEFAULT_VIDEO_TASK_SUBMIT_PATH
  let videoTaskQueryPath = DEFAULT_VIDEO_TASK_QUERY_PATH
  let videoTaskIDPath = DEFAULT_VIDEO_TASK_TASK_ID_PATH
  let videoTaskStatusPath = DEFAULT_VIDEO_TASK_STATUS_PATH
  let videoTaskProgressPath = DEFAULT_VIDEO_TASK_PROGRESS_PATH
  let videoTaskCreatedAtPath = DEFAULT_VIDEO_TASK_CREATED_AT_PATH
  let videoTaskUpdatedAtPath = DEFAULT_VIDEO_TASK_UPDATED_AT_PATH
  let videoTaskResultURLPaths = DEFAULT_VIDEO_TASK_RESULT_URL_PATHS
  let videoTaskErrorMessagePath = DEFAULT_VIDEO_TASK_ERROR_MESSAGE_PATH
  let videoTaskStatusSubmitted = DEFAULT_VIDEO_TASK_STATUS_SUBMITTED
  let videoTaskStatusQueued = DEFAULT_VIDEO_TASK_STATUS_QUEUED
  let videoTaskStatusRunning = DEFAULT_VIDEO_TASK_STATUS_RUNNING
  let videoTaskStatusSucceeded = DEFAULT_VIDEO_TASK_STATUS_SUCCEEDED
  let videoTaskStatusFailed = DEFAULT_VIDEO_TASK_STATUS_FAILED
  let advancedCustom = ''

  if (channel.settings) {
    try {
      const parsed = JSON.parse(channel.settings)
      vertexKeyType = parsed.vertex_key_type || 'json'
      azureResponsesVersion = parsed.azure_responses_version || ''
      isEnterpriseAccount = parsed.openrouter_enterprise === true
      awsKeyType = parsed.aws_key_type || 'ak_sk'
      allowServiceTier = parsed.allow_service_tier === true
      disableStore = parsed.disable_store === true
      allowSafetyIdentifier = parsed.allow_safety_identifier === true
      allowIncludeObfuscation = parsed.allow_include_obfuscation === true
      allowInferenceGeo = parsed.allow_inference_geo === true
      allowSpeed = parsed.allow_speed === true
      claudeBetaQuery = parsed.claude_beta_query === true
      upstreamModelUpdateCheckEnabled =
        parsed.upstream_model_update_check_enabled === true
      upstreamModelUpdateAutoSyncEnabled =
        parsed.upstream_model_update_auto_sync_enabled === true
      upstreamModelUpdateIgnoredModels = Array.isArray(
        parsed.upstream_model_update_ignored_models
      )
        ? parsed.upstream_model_update_ignored_models.join(',')
        : ''
      videoTaskDeltaSettlementEnabled =
        parsed.disable_task_delta_settlement !== true
      if (parsed.advanced_custom) {
        advancedCustom = stringifyAdvancedCustomConfig(parsed.advanced_custom)
      }
      const taskProtocolConfig = isJsonObjectValue(parsed.task_protocol_config)
        ? parsed.task_protocol_config
        : {}
      const parsedSubmitPath = String(
        taskProtocolConfig.submit_path || ''
      ).trim()
      const parsedQueryPath = String(taskProtocolConfig.query_path || '').trim()
      const taskProtocol = String(parsed.task_protocol || '')
        .trim()
        .toLowerCase()
      const hasConfiguredVideoTaskProtocol =
        CONFIGURED_VIDEO_TASK_PROTOCOLS.has(taskProtocol)
      const hasConfiguredVideoTaskPaths = Boolean(
        parsedSubmitPath || parsedQueryPath
      )
      videoTaskProtocolEnabled = hasConfiguredVideoTaskProtocol
      videoTaskPathOverrideEnabled =
        hasConfiguredVideoTaskProtocol || hasConfiguredVideoTaskPaths
      videoTaskSubmitPath = parsedSubmitPath || DEFAULT_VIDEO_TASK_SUBMIT_PATH
      videoTaskQueryPath = parsedQueryPath || DEFAULT_VIDEO_TASK_QUERY_PATH

      if (videoTaskProtocolEnabled) {
        videoTaskIDPath =
          String(taskProtocolConfig.task_id_path || '').trim() ||
          DEFAULT_VIDEO_TASK_TASK_ID_PATH
        videoTaskStatusPath =
          String(taskProtocolConfig.status_path || '').trim() ||
          DEFAULT_VIDEO_TASK_STATUS_PATH
        videoTaskProgressPath =
          String(taskProtocolConfig.progress_path || '').trim() ||
          DEFAULT_VIDEO_TASK_PROGRESS_PATH
        videoTaskCreatedAtPath =
          String(taskProtocolConfig.created_at_path || '').trim() ||
          DEFAULT_VIDEO_TASK_CREATED_AT_PATH
        videoTaskUpdatedAtPath =
          String(taskProtocolConfig.updated_at_path || '').trim() ||
          DEFAULT_VIDEO_TASK_UPDATED_AT_PATH
        videoTaskResultURLPaths = joinCSV(
          taskProtocolConfig.result_url_paths,
          DEFAULT_VIDEO_TASK_RESULT_URL_PATHS
        )
        videoTaskErrorMessagePath =
          String(taskProtocolConfig.error_message_path || '').trim() ||
          DEFAULT_VIDEO_TASK_ERROR_MESSAGE_PATH
        videoTaskStatusSubmitted = getVideoTaskStatusList(
          taskProtocolConfig.status_map,
          'SUBMITTED',
          DEFAULT_VIDEO_TASK_STATUS_SUBMITTED
        )
        videoTaskStatusQueued = getVideoTaskStatusList(
          taskProtocolConfig.status_map,
          'QUEUED',
          DEFAULT_VIDEO_TASK_STATUS_QUEUED
        )
        videoTaskStatusRunning = getVideoTaskStatusList(
          taskProtocolConfig.status_map,
          'IN_PROGRESS',
          DEFAULT_VIDEO_TASK_STATUS_RUNNING
        )
        videoTaskStatusSucceeded = getVideoTaskStatusList(
          taskProtocolConfig.status_map,
          'SUCCESS',
          DEFAULT_VIDEO_TASK_STATUS_SUCCEEDED
        )
        videoTaskStatusFailed = getVideoTaskStatusList(
          taskProtocolConfig.status_map,
          'FAILURE',
          DEFAULT_VIDEO_TASK_STATUS_FAILED
        )
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel settings:', error)
    }
  }

  return {
    name: channel.name || '',
    type: channel.type,
    base_url: channel.base_url || '',
    key: '', // Never populate key from backend for security
    openai_organization: channel.openai_organization || '',
    models: channel.models || '',
    group: parseGroups(channel.group || 'default'),
    model_mapping: channel.model_mapping || '',
    priority: channel.priority || 0,
    weight: channel.weight || 0,
    test_model: channel.test_model || '',
    auto_ban: channel.auto_ban ?? 1,
    status: channel.status,
    status_code_mapping: channel.status_code_mapping || '',
    tag: channel.tag || '',
    remark: channel.remark || '',
    setting: channel.setting || '',
    param_override: channel.param_override || '',
    header_override: channel.header_override || '',
    settings: channel.settings || '{}',
    advanced_custom: advancedCustom,
    other: channel.other || '',
    multi_key_mode: 'single',
    multi_key_type: channel.channel_info.multi_key_mode || 'random',
    batch_add_set_key_prefix_2_name: false,
    key_mode: 'append', // Default to append mode for editing multi-key channels
    // Channel extra settings
    ...extraSettings,
    // Type-specific settings
    is_enterprise_account: isEnterpriseAccount,
    vertex_key_type: vertexKeyType,
    azure_responses_version: azureResponsesVersion,
    aws_key_type: awsKeyType,
    allow_service_tier: allowServiceTier,
    disable_store: disableStore,
    allow_include_obfuscation: allowIncludeObfuscation,
    allow_inference_geo: allowInferenceGeo,
    allow_speed: allowSpeed,
    claude_beta_query: claudeBetaQuery,
    allow_safety_identifier: allowSafetyIdentifier,
    upstream_model_update_check_enabled: upstreamModelUpdateCheckEnabled,
    upstream_model_update_auto_sync_enabled: upstreamModelUpdateAutoSyncEnabled,
    upstream_model_update_ignored_models: upstreamModelUpdateIgnoredModels,
    video_task_path_override_enabled: videoTaskPathOverrideEnabled,
    video_task_protocol_enabled: videoTaskProtocolEnabled,
    video_task_delta_settlement_enabled: videoTaskDeltaSettlementEnabled,
    video_task_submit_path: videoTaskSubmitPath,
    video_task_query_path: videoTaskQueryPath,
    video_task_task_id_path: videoTaskIDPath,
    video_task_status_path: videoTaskStatusPath,
    video_task_progress_path: videoTaskProgressPath,
    video_task_created_at_path: videoTaskCreatedAtPath,
    video_task_updated_at_path: videoTaskUpdatedAtPath,
    video_task_result_url_paths: videoTaskResultURLPaths,
    video_task_error_message_path: videoTaskErrorMessagePath,
    video_task_status_submitted: videoTaskStatusSubmitted,
    video_task_status_queued: videoTaskStatusQueued,
    video_task_status_running: videoTaskStatusRunning,
    video_task_status_succeeded: videoTaskStatusSucceeded,
    video_task_status_failed: videoTaskStatusFailed,
  }
}

/**
 * Build the setting JSON string from form extra settings
 */
function buildSettingJSON(formData: ChannelFormValues): string {
  const settingObj = {
    force_format: formData.force_format || false,
    thinking_to_content: formData.thinking_to_content || false,
    proxy: formData.proxy || '',
    pass_through_body_enabled: formData.pass_through_body_enabled || false,
    system_prompt: formData.system_prompt || '',
    system_prompt_override: formData.system_prompt_override || false,
  }
  return JSON.stringify(settingObj)
}

/**
 * Build the settings JSON string (for type-specific config like vertex_key_type)
 */
function buildSettingsJSON(formData: ChannelFormValues): string {
  let settingsObj: Record<string, unknown> = {}

  // Try to parse existing settings first
  if (formData.settings && formData.settings !== '{}') {
    try {
      settingsObj = JSON.parse(formData.settings)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse existing settings:', error)
    }
  }

  // Add vertex_key_type for Vertex AI channels (type 41)
  if (formData.type === 41) {
    settingsObj.vertex_key_type = formData.vertex_key_type || 'json'
  } else if ('vertex_key_type' in settingsObj) {
    delete settingsObj.vertex_key_type
  }

  // Add azure_responses_version for Azure channels (type 3)
  if (formData.type === 3 && formData.azure_responses_version) {
    settingsObj.azure_responses_version = formData.azure_responses_version
  } else if ('azure_responses_version' in settingsObj) {
    delete settingsObj.azure_responses_version
  }

  // Add enterprise account setting for OpenRouter (type 20)
  if (formData.type === 20) {
    settingsObj.openrouter_enterprise = formData.is_enterprise_account === true
  } else if ('openrouter_enterprise' in settingsObj) {
    delete settingsObj.openrouter_enterprise
  }

  // Add aws_key_type for AWS channels (type 33)
  if (formData.type === 33) {
    settingsObj.aws_key_type = formData.aws_key_type || 'ak_sk'
  } else if ('aws_key_type' in settingsObj) {
    delete settingsObj.aws_key_type
  }

  // Field passthrough controls:
  // - OpenAI (type 1) and Anthropic (type 14): allow_service_tier
  // - OpenAI only: disable_store, allow_safety_identifier
  if (formData.type === 1 || formData.type === 14) {
    settingsObj.allow_service_tier = formData.allow_service_tier === true
  } else if ('allow_service_tier' in settingsObj) {
    delete settingsObj.allow_service_tier
  }

  if (formData.type === 1) {
    settingsObj.disable_store = formData.disable_store === true
    settingsObj.allow_safety_identifier =
      formData.allow_safety_identifier === true
    settingsObj.allow_include_obfuscation =
      formData.allow_include_obfuscation === true
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
  } else {
    if ('disable_store' in settingsObj) delete settingsObj.disable_store
    if ('allow_safety_identifier' in settingsObj)
      delete settingsObj.allow_safety_identifier
    if ('allow_include_obfuscation' in settingsObj)
      delete settingsObj.allow_include_obfuscation
    if (formData.type !== 14 && 'allow_inference_geo' in settingsObj)
      delete settingsObj.allow_inference_geo
  }

  // Anthropic (type 14): claude_beta_query, allow_inference_geo, allow_speed
  if (formData.type === 14) {
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
    settingsObj.allow_speed = formData.allow_speed === true
    settingsObj.claude_beta_query = formData.claude_beta_query === true
  } else {
    if ('allow_speed' in settingsObj) delete settingsObj.allow_speed
    if ('claude_beta_query' in settingsObj) delete settingsObj.claude_beta_query
  }

  // Upstream model update settings (for model-fetchable channel types)
  if (MODEL_FETCHABLE_TYPES.has(formData.type)) {
    settingsObj.upstream_model_update_check_enabled =
      formData.upstream_model_update_check_enabled === true
    settingsObj.upstream_model_update_auto_sync_enabled =
      settingsObj.upstream_model_update_check_enabled === true &&
      formData.upstream_model_update_auto_sync_enabled === true
    settingsObj.upstream_model_update_ignored_models = Array.from(
      new Set(
        String(formData.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      )
    )
    if (
      !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
      settingsObj.upstream_model_update_check_enabled !== true
    ) {
      settingsObj.upstream_model_update_last_detected_models = []
    }
    if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
      settingsObj.upstream_model_update_last_check_time = 0
    }
  }

  const isVideoTaskChannel = VIDEO_TASK_CHANNEL_TYPES.has(formData.type)
  const isDoubaoVideoChannel = formData.type === CHANNEL_TYPE_DOUBAO_VIDEO
  if (
    isDoubaoVideoChannel &&
    formData.video_task_delta_settlement_enabled === false
  ) {
    settingsObj.disable_task_delta_settlement = true
  } else if ('disable_task_delta_settlement' in settingsObj) {
    delete settingsObj.disable_task_delta_settlement
  }

  const enableVideoTaskProtocol =
    isVideoTaskChannel && formData.video_task_protocol_enabled === true
  const enableVideoTaskPathOverride =
    isVideoTaskChannel &&
    (formData.video_task_path_override_enabled === true ||
      enableVideoTaskProtocol)

  if (enableVideoTaskProtocol) {
    const existingTaskProtocolConfig = isJsonObjectValue(
      settingsObj.task_protocol_config
    )
      ? settingsObj.task_protocol_config
      : {}
    const statusMap: Record<string, string> = {}
    setVideoTaskStatusMap(
      statusMap,
      formData.video_task_status_submitted,
      'SUBMITTED'
    )
    setVideoTaskStatusMap(
      statusMap,
      formData.video_task_status_queued,
      'QUEUED'
    )
    setVideoTaskStatusMap(
      statusMap,
      formData.video_task_status_running,
      'IN_PROGRESS'
    )
    setVideoTaskStatusMap(
      statusMap,
      formData.video_task_status_succeeded,
      'SUCCESS'
    )
    setVideoTaskStatusMap(
      statusMap,
      formData.video_task_status_failed,
      'FAILURE'
    )

    settingsObj.task_protocol = VIDEO_TASK_PROTOCOL
    settingsObj.task_protocol_config = {
      ...existingTaskProtocolConfig,
      submit_path:
        formData.video_task_submit_path?.trim() ||
        DEFAULT_VIDEO_TASK_SUBMIT_PATH,
      query_path:
        formData.video_task_query_path?.trim() || DEFAULT_VIDEO_TASK_QUERY_PATH,
      task_id_path:
        formData.video_task_task_id_path?.trim() ||
        DEFAULT_VIDEO_TASK_TASK_ID_PATH,
      status_path:
        formData.video_task_status_path?.trim() ||
        DEFAULT_VIDEO_TASK_STATUS_PATH,
      progress_path:
        formData.video_task_progress_path?.trim() ||
        DEFAULT_VIDEO_TASK_PROGRESS_PATH,
      created_at_path:
        formData.video_task_created_at_path?.trim() ||
        DEFAULT_VIDEO_TASK_CREATED_AT_PATH,
      updated_at_path:
        formData.video_task_updated_at_path?.trim() ||
        DEFAULT_VIDEO_TASK_UPDATED_AT_PATH,
      result_url_paths: splitCSV(
        formData.video_task_result_url_paths,
        DEFAULT_VIDEO_TASK_RESULT_URL_PATHS
      ),
      error_message_path:
        formData.video_task_error_message_path?.trim() ||
        DEFAULT_VIDEO_TASK_ERROR_MESSAGE_PATH,
      status_map: statusMap,
    }
  } else if (enableVideoTaskPathOverride) {
    delete settingsObj.task_protocol
    settingsObj.task_protocol_config = {
      submit_path:
        formData.video_task_submit_path?.trim() ||
        DEFAULT_VIDEO_TASK_SUBMIT_PATH,
      query_path:
        formData.video_task_query_path?.trim() || DEFAULT_VIDEO_TASK_QUERY_PATH,
    }
  } else if (isVideoTaskChannel && 'task_protocol_config' in settingsObj) {
    delete settingsObj.task_protocol_config
  } else if (!isVideoTaskChannel && 'task_protocol_config' in settingsObj) {
    delete settingsObj.task_protocol_config
  }

  if (!enableVideoTaskProtocol && 'task_protocol' in settingsObj) {
    delete settingsObj.task_protocol
  }

  if (formData.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
    const advancedCustomConfig = parseAdvancedCustomConfig(
      formData.advanced_custom
    )
    if (advancedCustomConfig) {
      settingsObj.advanced_custom = advancedCustomConfig
    }
  } else if ('advanced_custom' in settingsObj) {
    delete settingsObj.advanced_custom
  }

  return JSON.stringify(settingsObj)
}

function normalizeBaseUrl(value: string | undefined): string {
  return String(value || '')
    .trim()
    .replace(/\/+$/, '')
}

/**
 * Transform form data to API payload for creating channel
 */
export function transformFormDataToCreatePayload(formData: ChannelFormValues): {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
} {
  const mode = formData.multi_key_mode || 'single'

  const channel: Partial<Channel> = {
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    key: formData.key,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority || null,
    weight: formData.weight || null,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Clean up empty strings to null for optional fields
  Object.keys(channel).forEach((key) => {
    if (channel[key as keyof typeof channel] === '') {
      ;(channel as Record<string, unknown>)[key] = null
    }
  })

  return {
    mode,
    multi_key_mode:
      mode === 'multi_to_single' ? formData.multi_key_type : undefined,
    batch_add_set_key_prefix_2_name:
      mode === 'batch' ? formData.batch_add_set_key_prefix_2_name : undefined,
    channel,
  }
}

/**
 * Transform form data to API payload for updating channel
 */
export function transformFormDataToUpdatePayload(
  formData: ChannelFormValues,
  channelId: number
): Partial<Channel> {
  const payload: Partial<Channel> = {
    id: channelId,
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority ?? 0,
    weight: formData.weight ?? 0,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Only include key if it was changed (not empty)
  if (formData.key && formData.key.trim()) {
    payload.key = formData.key
  }

  // Clean up empty strings to null for optional fields
  Object.keys(payload).forEach((key) => {
    if (payload[key as keyof typeof payload] === '') {
      ;(payload as Record<string, unknown>)[key] = null
    }
  })

  // Send explicit empty strings for nullable fields so GORM updates can clear them.
  payload.base_url = normalizeBaseUrl(formData.base_url) || ''
  payload.openai_organization = formData.openai_organization || ''
  payload.test_model = formData.test_model || ''
  payload.tag = formData.tag || ''
  payload.remark = formData.remark || ''
  payload.model_mapping = formData.model_mapping || ''
  payload.status_code_mapping = formData.status_code_mapping || ''
  payload.param_override = formData.param_override || ''
  payload.header_override = formData.header_override || ''

  return payload
}

// ============================================================================
// Validation Helpers
// ============================================================================

/**
 * Validate JSON string
 */
export function validateJSON(value: string): boolean {
  if (!value || value.trim() === '') return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Validate model mapping format
 */
export function validateModelMapping(value: string): boolean {
  if (!value || value.trim() === '') return true
  return validateJSON(value)
}

/**
 * Parse models string to array
 */
export function parseModels(models: string): string[] {
  if (!models) return []
  return models
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
}

/**
 * Parse groups string to array
 */
export function parseGroups(groups: string): string[] {
  if (!groups) return []
  return groups
    .split(',')
    .map((g) => g.trim())
    .filter((g) => g.length > 0)
}

/**
 * Format models array to string
 */
export function formatModels(models: string[]): string {
  return models.join(',')
}

/**
 * Format groups array to string
 */
export function formatGroups(groups: string[]): string {
  return groups.join(',')
}
