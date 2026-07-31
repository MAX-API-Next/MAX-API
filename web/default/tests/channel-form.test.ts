import assert from 'node:assert/strict'
import { test } from 'bun:test'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  getChannelTypeScopedFieldDefaults,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from '../src/features/channels/lib/channel-form'
import type { Channel } from '../src/features/channels/types'

const legacyVideoTaskSettings = JSON.stringify({
  task_protocol: 'seedance_official_media',
  task_protocol_config: {
    submit_path: '/legacy/videos/create',
    query_path: '/legacy/videos/{task_id}',
    request_body_mode: 'media_generation',
  },
  keep_existing: true,
})

function buildSettings(overrides: Partial<ChannelFormValues>) {
  const payload = transformFormDataToUpdatePayload(
    {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'legacy video task channel',
      type: 1,
      models: 'video-model',
      group: ['default'],
      settings: legacyVideoTaskSettings,
      ...overrides,
    },
    1
  )

  return JSON.parse(String(payload.settings || '{}')) as Record<string, unknown>
}

function legacyChannel(): Channel {
  return {
    id: 1,
    type: 1,
    key: '',
    openai_organization: '',
    test_model: '',
    status: 1,
    name: 'legacy video task channel',
    weight: 0,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    base_url: '',
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'video-model',
    group: 'default',
    used_quota: 0,
    model_mapping: '',
    status_code_mapping: '',
    priority: 0,
    auto_ban: 1,
    other_info: '',
    tag: '',
    setting: '{}',
    param_override: '',
    header_override: '',
    remark: '',
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings: legacyVideoTaskSettings,
  }
}

test('loads legacy video task protocol as path override only', () => {
  const defaults = transformChannelToFormDefaults(legacyChannel())

  assert.equal(defaults.video_task_protocol_enabled, false)
  assert.equal(defaults.video_task_path_override_enabled, true)
  assert.equal(defaults.video_task_submit_path, '/legacy/videos/create')
  assert.equal(defaults.video_task_query_path, '/legacy/videos/{task_id}')
})

test('clears legacy video task protocol when saving path override', () => {
  const defaults = transformChannelToFormDefaults(legacyChannel())
  const savedSettings = buildSettings(defaults)

  assert.equal(savedSettings.keep_existing, true)
  assert.equal(savedSettings.task_protocol, undefined)
  assert.deepEqual(savedSettings.task_protocol_config, {
    submit_path: '/legacy/videos/create',
    query_path: '/legacy/videos/{task_id}',
  })
})

test('channel type switch clears scoped base url and other values', () => {
  assert.deepEqual(getChannelTypeScopedFieldDefaults(49), {
    base_url: '',
    other: '',
  })
  assert.deepEqual(getChannelTypeScopedFieldDefaults(45), {
    base_url: 'https://ark.cn-beijing.volces.com',
    other: '',
  })
  assert.deepEqual(getChannelTypeScopedFieldDefaults(18), {
    base_url: '',
    other: 'v2.1',
  })
})
