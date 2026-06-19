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
import type { ChannelFormValues } from './channel-form'
import {
  BASE_URL_REQUIRED_TYPES,
  OTHER_REQUIRED_TYPES,
  hasVertexDefaultRegion,
  hasVideoTaskQueryPlaceholder,
} from './channel-config-rules'
import { isVideoTaskChannelType } from './channel-capabilities'

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
        message: '此 JSON 配置必须是 JSON 对象。',
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
        message: 'Vertex AI 区域配置必须是包含 default 字段的 JSON 对象。',
      })
    return
  }

  if (!hasVertexDefaultRegion(regionConfig)) {
    addIssue(issues, {
      id: 'vertex_region_missing_default',
      severity: 'error',
      field: 'other',
      message: 'Vertex AI 区域配置必须包含 default 字段。',
    })
  }

  if (values.vertex_key_type === 'api_key') {
    if (values.multi_key_mode && values.multi_key_mode !== 'single') {
      addIssue(issues, {
        id: 'vertex_api_key_batch_mode',
        severity: 'error',
        field: 'multi_key_mode',
        message: 'Vertex AI 的 API Key 模式不支持批量创建。',
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
        message: 'Vertex AI 服务账号密钥必须是有效的 JSON。',
      })
    }
  } catch {
    addIssue(issues, {
      id: 'vertex_key_invalid_json',
      severity: 'error',
      field: 'key',
      message: 'Vertex AI 服务账号密钥必须是有效的 JSON。',
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
      message: 'Codex 渠道不支持批量创建。',
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
        message: 'Codex 凭证必须包含 access_token 和 account_id 字段。',
      })
    }
  } catch {
    addIssue(issues, {
      id: 'codex_key_invalid_json',
      severity: 'error',
      field: 'key',
      message: 'Codex 凭证必须是有效的 JSON 对象。',
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
      message: '视频任务协议设置仅适用于支持视频任务的渠道类型。',
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
      message: '视频任务提交路径不能为空。',
    })
  }

  const queryPath = trim(values.video_task_query_path)
  if (!queryPath) {
    addIssue(issues, {
      id: 'video_task_missing_query_path',
      severity: 'error',
      field: 'video_task_query_path',
      message: '视频任务查询路径不能为空。',
    })
  } else if (!hasVideoTaskQueryPlaceholder(queryPath)) {
    addIssue(issues, {
      id: 'video_task_query_placeholder',
      severity: 'error',
      field: 'video_task_query_path',
      message:
        '视频任务查询路径必须包含 {task_id}、{operation_name} 或 {upstream_task_id}。',
    })
  }

  if (values.video_task_protocol_enabled) {
    if (!trim(values.video_task_task_id_path)) {
      addIssue(issues, {
        id: 'video_task_missing_task_id_path',
        severity: 'error',
        field: 'video_task_task_id_path',
        message: '启用完整任务协议时，必须填写任务 ID 响应路径。',
      })
    }
    if (!trim(values.video_task_status_path)) {
      addIssue(issues, {
        id: 'video_task_missing_status_path',
        severity: 'error',
        field: 'video_task_status_path',
        message: '启用完整任务协议时，必须填写状态响应路径。',
      })
    }
    if (!trim(values.video_task_result_url_paths)) {
      addIssue(issues, {
        id: 'video_task_missing_result_paths',
        severity: 'warning',
        field: 'video_task_result_url_paths',
        message: '未配置结果 URL 路径，视频任务完成后可能无法返回内容地址。',
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
      message: '新建渠道时 API Key 不能为空。',
    })
  }

  if (!trim(values.models)) {
    addIssue(issues, {
      id: 'models_required',
      severity: 'error',
      field: 'models',
      message: '该渠道至少需要发布一个模型。',
    })
  }

  if (BASE_URL_REQUIRED_TYPES.has(type) && !trim(values.base_url)) {
    addIssue(issues, {
      id: 'base_url_required',
      severity: 'error',
      field: 'base_url',
      message: '当前渠道类型需要填写 Base URL。',
    })
  }

  if (trim(values.base_url).endsWith('/v1')) {
    addIssue(issues, {
      id: 'base_url_v1_suffix',
      severity: 'warning',
      field: 'base_url',
      message:
        'Base URL 通常不应以 /v1 结尾，MAX API 会自动拼接上游路径。',
    })
  }

  if (OTHER_REQUIRED_TYPES.has(type) && !trim(values.other)) {
    addIssue(issues, {
      id: 'other_required',
      severity: 'error',
      field: 'other',
      message: '当前渠道类型需要填写额外的上游配置。',
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
      message: '当前渠道类型不支持上游模型发现，该功能不会运行。',
    })
  }

  validateJsonFields(values, issues)
  validateVertexConfig(values, issues, options)
  validateCodexConfig(values, issues, options)
  validateVideoTaskConfig(values, issues)

  return issues
}
