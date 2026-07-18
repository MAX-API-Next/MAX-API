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
  getTurnstileHeaders,
  TURNSTILE_TOKEN_HEADER,
} from './turnstile-request'

describe('getTurnstileHeaders', () => {
  test('moves a non-empty token into the dedicated header', () => {
    assert.deepEqual(getTurnstileHeaders(' token-value '), {
      [TURNSTILE_TOKEN_HEADER]: 'token-value',
    })
  })

  test('omits an empty token', () => {
    assert.deepEqual(getTurnstileHeaders(''), {})
  })
})
