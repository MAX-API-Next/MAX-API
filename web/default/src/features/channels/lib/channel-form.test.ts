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
import { CHANNEL_TYPE_DOUBAO_VIDEO } from '../constants'
import { channelSchema } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from './channel-form'

describe('channel form settings round trip', () => {
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
      status_code_mapping: '{"429":503,"0429":502}',
    })

    assert.equal(result.success, false)
  })
})
