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
import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../constants'
import { buildChatCompletionPayload } from './payload-builder'

describe('buildChatCompletionPayload', () => {
  test('sends enabled zero values and omits disabled or unset parameters', () => {
    const payload = buildChatCompletionPayload(
      [
        {
          key: 'user-1',
          from: 'user',
          versions: [{ id: 'version-1', content: 'Hello' }],
        },
      ],
      {
        ...DEFAULT_CONFIG,
        temperature: 0,
        frequency_penalty: 0,
        presence_penalty: 0,
        seed: null,
      },
      {
        ...DEFAULT_PARAMETER_ENABLED,
        temperature: true,
        frequency_penalty: true,
        presence_penalty: true,
        max_tokens: true,
        seed: true,
      }
    )

    assert.equal(payload.temperature, 0)
    assert.equal(payload.frequency_penalty, 0)
    assert.equal(payload.presence_penalty, 0)
    assert.equal(payload.max_tokens, DEFAULT_CONFIG.max_tokens)
    assert.equal('seed' in payload, false)
    assert.equal('top_p' in payload, true)
  })
})
