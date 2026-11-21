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
import {
  CHANNEL_TYPE_DOUBAO_VIDEO,
  CHANNEL_TYPE_TASK_PLUGIN,
} from '../constants'
import { channelSchema } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  taskPluginMetadataPatch,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from './channel-form'

describe('channel form settings round trip', () => {
  test('task plugin writes keep options outside the stored settings and reopen without secrets', () => {
    const pluginSettings = {
      task_plugin_key: 'fixture',
      task_plugin_upstream: 'vendor',
      task_plugin_config: { retries: 0, highQuality: false },
    }
    const form = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: CHANNEL_TYPE_TASK_PLUGIN,
      multi_key_mode: 'batch' as const,
      name: 'plugin',
      models: 'alias',
      model_mapping: '{"alias":"declared-model"}',
      base_url: 'https://provider.example',
      setting: JSON.stringify(pluginSettings),
      key: 'synthetic-only',
      expected_task_plugin_digest: 'observed-digest',
    }
    const { mode, channel } = transformFormDataToCreatePayload(form)
    assert.equal(mode, 'single')
    assert.equal(channel.expected_task_plugin_digest, 'observed-digest')
    assert.ok(channel.setting)
    assert.equal(
      JSON.parse(channel.setting).task_plugin_key,
      pluginSettings.task_plugin_key
    )
    assert.equal(channel.setting.includes('observed-digest'), false)
    const reopened = transformChannelToFormDefaults(
      channelSchema.parse({
        ...channel,
        settings:
          '{"upstream_model_update_check_enabled":true,"upstream_model_update_auto_sync_enabled":true}',
        id: 8,
        created_time: 1,
        test_time: 0,
        response_time: 0,
        balance_updated_time: 0,
        other: '',
        remark: '',
      })
    )
    assert.equal(reopened.key, '')
    assert.equal(reopened.expected_task_plugin_digest, undefined)
    const initial = { ...reopened, other: 'us', remark: 'before' }
    assert.deepEqual(
      taskPluginMetadataPatch(
        { ...initial, remark: '', expected_task_plugin_digest: 'refreshed' },
        initial,
        8
      ),
      { remark: '' }
    )
    for (const changed of [
      { other: '' },
      { key: 'replacement-fixture' },
      { models: 'other' },
      { base_url: 'https://new.example' },
      { model_mapping: '{}' },
      {
        setting: JSON.stringify({ ...pluginSettings, task_plugin_config: {} }),
      },
    ]) {
      assert.equal(
        taskPluginMetadataPatch({ ...initial, ...changed }, initial, 8),
        undefined
      )
    }
    assert.equal(
      transformFormDataToUpdatePayload({ ...initial, other: '' }, 8).other,
      ''
    )
    reopened.expected_task_plugin_digest = 'refreshed-digest'
    const update = transformFormDataToUpdatePayload(reopened, 8)
    const savedSettings = JSON.parse(update.settings ?? '{}')
    assert.equal(savedSettings.upstream_model_update_check_enabled, undefined)
    assert.equal(
      savedSettings.upstream_model_update_auto_sync_enabled,
      undefined
    )
    assert.equal(update.key, undefined)
    assert.equal(update.model_mapping, form.model_mapping)
    assert.equal(update.expected_task_plugin_digest, 'refreshed-digest')
    assert.deepEqual(
      JSON.parse(update.setting ?? '{}').task_plugin_config,
      pluginSettings.task_plugin_config
    )
  })

  test('preserves status mapping and all configured video response paths', () => {
    const channel = channelSchema.parse({
      id: 7,
      type: CHANNEL_TYPE_DOUBAO_VIDEO,
      key: 'not-returned-to-form',
      status: 1,
      name: 'video channel',
      created_time: 1,
      test_time: 1,
      response_time: 1,
      balance_updated_time: 1,
      models: 'video-model',
      status_code_mapping: '{"429":"503"}',
      settings: JSON.stringify({
        task_protocol: 'generic_video_task',
        task_protocol_config: {
          submit_path: '/submit',
          query_path: '/tasks/{task_id}',
          task_id_path: 'data.id',
          status_path: 'data.status',
          progress_path: 'data.progress',
          result_url_paths: ['data.url'],
          error_message_path: 'data.error.message',
          created_at_path: 'data.created_at',
          updated_at_path: 'data.updated_at',
          status_map: { completed: 'SUCCESS' },
          vendor_extension: { version: 2 },
        },
      }),
    })

    const defaults = transformChannelToFormDefaults(channel)
    assert.equal(defaults.status_code_mapping, '{"429":"503"}')
    assert.equal(defaults.video_task_created_at_path, 'data.created_at')
    assert.equal(defaults.video_task_updated_at_path, 'data.updated_at')

    const payload = transformFormDataToUpdatePayload(defaults, channel.id)
    const settings = JSON.parse(String(payload.settings))
    assert.equal(payload.status_code_mapping, '{"429":"503"}')
    assert.equal(
      settings.task_protocol_config.created_at_path,
      'data.created_at'
    )
    assert.equal(
      settings.task_protocol_config.updated_at_path,
      'data.updated_at'
    )
    assert.deepEqual(settings.task_protocol_config.vendor_extension, {
      version: 2,
    })
  })

  test('rejects aliased numeric status-code source keys', () => {
    const result = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'channel',
      models: 'model',
      status_code_mapping: '{"429":503," 429":502}',
    })

    assert.equal(result.success, false)
  })
})
