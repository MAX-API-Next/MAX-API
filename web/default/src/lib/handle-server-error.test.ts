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
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getSafeErrorDebugInfo } from './handle-server-error'

describe('getSafeErrorDebugInfo', () => {
  test('does not include request headers or config', () => {
    const error = new AxiosError('request failed', 'ERR_BAD_REQUEST')
    error.config = {
      headers: { Authorization: 'Bearer secret-value' },
    } as unknown as InternalAxiosRequestConfig

    const debugInfo = getSafeErrorDebugInfo(error)
    assert.deepEqual(debugInfo, {
      name: 'AxiosError',
      message: 'request failed',
      code: 'ERR_BAD_REQUEST',
      status: undefined,
    })
    assert.doesNotMatch(JSON.stringify(debugInfo), /secret-value/)
  })
})
