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
import { MODEL_FETCHABLE_TYPES } from '../constants'
import { isVideoTaskChannelType } from './channel-capabilities'
import {
  BASE_URL_REQUIRED_TYPES,
  OTHER_REQUIRED_TYPES,
  hasVertexDefaultRegion,
  hasVideoTaskQueryPlaceholder,
} from './channel-config-rules'
import type { ChannelFormValues } from './channel-form'

export type ChannelConfigValidationSeverity = 'error' | 'warning' | 'info'

export type ChannelConfigValidationIssue = {
  id: string
  severity: ChannelConfigValidationSeverity
  message: string
  field?: keyof ChannelFormValues
}

export type ChannelConfigValidationOptions = {
  isEditing?: boolean
}

function trim(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function parseJson(value: string | undefined): unknown {
  const raw = trim(value)
  if (!raw) return undefined
  return JSON.parse(raw)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isValidJsonObject(value: string | undefined): boolean {
  try {
    const parsed = parseJson(value)
    return parsed === undefined || isRecord(parsed)
  } catch {
    return false
  }
}

function addIssue(
  issues: ChannelConfigValidationIssue[],
  issue: ChannelConfigValidationIssue
): void {
  issues.push(issue)
}

function validateJsonFields(
  values: ChannelFormValues,
  issues: ChannelConfigValidationIssue[]
) {
  const jsonFields: Array<keyof ChannelFormValues> = [
    'setting',
    'param_override',
    'header_override',
    'settings',
  ]

  for (const field of jsonFields) {
    const value = values[field]
    if (typeof value === 'string' && trim(value) && !isValidJsonObject(value)) {
      addIssue(issues, {
        id: `${field}_invalid_json`,
        severity: 'error',
        field,
        message: 'This JSON configuration must be a JSON object.',
      })
    }
  }
}

function validateVertexConfig(
  values: ChannelFormValues,
  issues: ChannelConfigValidationIssue[],
  options: ChannelConfigValidationOptions
) {
  if (values.type !== 41) return

  const otherValue = trim(values.other)
  if (!otherValue) return

  let regionConfig: unknown
  try {
    regionConfig = parseJson(otherValue)
  } catch {
    addIssue(issues, {
      id: 'vertex_region_invalid_json',
      severity: 'error',
      field: 'other',
      message:
        'Vertex AI region configuration must be a JSON object containing a default field.',
    })
    return
  }

  if (!hasVertexDefaultRegion(regionConfig)) {
    addIssue(issues, {
      id: 'vertex_region_missing_default',
      severity: 'error',
      field: 'other',
      message: 'Vertex AI region configuration must contain a default field.',
    })
  }

  if (values.vertex_key_type === 'api_key') {
    if (values.multi_key_mode && values.multi_key_mode !== 'single') {
      addIssue(issues, {
        id: 'vertex_api_key_batch_mode',
        severity: 'error',
        field: 'multi_key_mode',
        message: 'Vertex AI API Key mode does not support batch creation.',
      })
    }
    return
  }

  const key = trim(values.key)
  if (!key && options.isEditing) return
  if (!key) return

  try {
    const parsed = parseJson(key)
    if (!isRecord(parsed) && !Array.isArray(parsed)) {
      addIssue(issues, {
        id: 'vertex_key_invalid_shape',
        severity: 'error',
        field: 'key',
        message: 'Vertex AI service account credentials must be valid JSON.',
      })
    }
  } catch {
    addIssue(issues, {
      id: 'vertex_key_invalid_json',
      severity: 'error',
      field: 'key',
      message: 'Vertex AI service account credentials must be valid JSON.',
    })
  }
}

function validateCodexConfig(
  values: ChannelFormValues,
  issues: ChannelConfigValidationIssue[],
  options: ChannelConfigValidationOptions
) {
  if (values.type !== 57) return

  if (values.multi_key_mode && values.multi_key_mode !== 'single') {
    addIssue(issues, {
      id: 'codex_batch_mode',
      severity: 'error',
      field: 'multi_key_mode',
      message: 'Codex channels do not support batch creation.',
    })
  }

  const key = trim(values.key)
  if (!key && options.isEditing) return
  if (!key) return

  try {
    const parsed = parseJson(key)
    if (
      !isRecord(parsed) ||
      !trim(parsed.access_token) ||
      !trim(parsed.account_id)
    ) {
      addIssue(issues, {
        id: 'codex_key_missing_fields',
        severity: 'error',
        field: 'key',
        message: 'Codex credentials must contain access_token and account_id.',
      })
    }
  } catch {
    addIssue(issues, {
      id: 'codex_key_invalid_json',
      severity: 'error',
      field: 'key',
      message: 'Codex credentials must be a valid JSON object.',
    })
  }
}

function validateVideoTaskConfig(
  values: ChannelFormValues,
  issues: ChannelConfigValidationIssue[]
) {
  if (
    !isVideoTaskChannelType(values.type) &&
    (values.video_task_path_override_enabled ||
      values.video_task_protocol_enabled)
  ) {
    addIssue(issues, {
      id: 'video_task_unsupported_type',
      severity: 'warning',
      field: 'video_task_protocol_enabled',
      message:
        'Video task protocol settings are only available for channel types that support video tasks.',
    })
    return
  }

  const usesVideoTaskConfig =
    isVideoTaskChannelType(values.type) &&
    (values.video_task_path_override_enabled ||
      values.video_task_protocol_enabled)

  if (!usesVideoTaskConfig) return

  if (!trim(values.video_task_submit_path)) {
    addIssue(issues, {
      id: 'video_task_missing_submit_path',
      severity: 'error',
      field: 'video_task_submit_path',
      message: 'Video task submission path is required.',
    })
  }

  const queryPath = trim(values.video_task_query_path)
  if (!queryPath) {
    addIssue(issues, {
      id: 'video_task_missing_query_path',
      severity: 'error',
      field: 'video_task_query_path',
      message: 'Video task query path is required.',
    })
  } else if (!hasVideoTaskQueryPlaceholder(queryPath)) {
    addIssue(issues, {
      id: 'video_task_query_placeholder',
      severity: 'error',
      field: 'video_task_query_path',
      message:
        'Video task query path must contain {task_id}, {operation_name}, or {upstream_task_id}.',
    })
  }

  if (values.video_task_protocol_enabled) {
    if (!trim(values.video_task_task_id_path)) {
      addIssue(issues, {
        id: 'video_task_missing_task_id_path',
        severity: 'error',
        field: 'video_task_task_id_path',
        message:
          'Task ID response path is required when the full task protocol is enabled.',
      })
    }
    if (!trim(values.video_task_status_path)) {
      addIssue(issues, {
        id: 'video_task_missing_status_path',
        severity: 'error',
        field: 'video_task_status_path',
        message:
          'Status response path is required when the full task protocol is enabled.',
      })
    }
    if (!trim(values.video_task_result_url_paths)) {
      addIssue(issues, {
        id: 'video_task_missing_result_paths',
        severity: 'warning',
        field: 'video_task_result_url_paths',
        message:
          'Without a result URL path, completed video tasks may not return a content URL.',
      })
    }
  }
}

export function getChannelConfigValidationIssues(
  values: ChannelFormValues,
  options: ChannelConfigValidationOptions = {}
): ChannelConfigValidationIssue[] {
  const issues: ChannelConfigValidationIssue[] = []
  const type = Number(values.type)

  if (!options.isEditing && !trim(values.key)) {
    addIssue(issues, {
      id: 'key_required_create',
      severity: 'error',
      field: 'key',
      message: 'API Key is required when creating a channel.',
    })
  }

  if (!trim(values.models)) {
    addIssue(issues, {
      id: 'models_required',
      severity: 'error',
      field: 'models',
      message: 'At least one model must be published for this channel.',
    })
  }

  if (BASE_URL_REQUIRED_TYPES.has(type) && !trim(values.base_url)) {
    addIssue(issues, {
      id: 'base_url_required',
      severity: 'error',
      field: 'base_url',
      message: 'Base URL is required for this channel type.',
    })
  }

  if (trim(values.base_url).endsWith('/v1')) {
    addIssue(issues, {
      id: 'base_url_v1_suffix',
      severity: 'warning',
      field: 'base_url',
      message:
        'Base URL should not usually end with /v1 because MAX API appends the upstream path automatically.',
    })
  }

  if (OTHER_REQUIRED_TYPES.has(type) && !trim(values.other)) {
    addIssue(issues, {
      id: 'other_required',
      severity: 'error',
      field: 'other',
      message:
        'Additional upstream configuration is required for this channel type.',
    })
  }

  if (
    !MODEL_FETCHABLE_TYPES.has(type) &&
    (values.upstream_model_update_check_enabled ||
      values.upstream_model_update_auto_sync_enabled)
  ) {
    addIssue(issues, {
      id: 'model_discovery_not_supported',
      severity: 'warning',
      field: 'upstream_model_update_check_enabled',
      message:
        'Upstream model discovery is not supported for this channel type and will not run.',
    })
  }

  validateJsonFields(values, issues)
  validateVertexConfig(values, issues, options)
  validateCodexConfig(values, issues, options)
  validateVideoTaskConfig(values, issues)

  return issues
}
