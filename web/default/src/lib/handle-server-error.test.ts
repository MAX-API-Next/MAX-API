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
import {
  getSafeErrorDebugInfo,
  resolveServerErrorMessage,
} from './handle-server-error'

describe('getSafeErrorDebugInfo', (): void => {
  test('does not include request headers or config', (): void => {
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

describe('resolveServerErrorMessage', (): void => {
  test('uses the localized fallback for ordinary errors', (): void => {
    assert.equal(
      resolveServerErrorMessage(
        new Error('account disabled'),
        'Verification unavailable',
        'Content not found.'
      ),
      'Verification unavailable'
    )
  })

  test('uses an Axios response message before the fallback', (): void => {
    const error = new AxiosError('request failed', 'ERR_BAD_REQUEST')
    error.response = {
      status: 503,
      statusText: 'Service Unavailable',
      headers: {},
      config: {} as InternalAxiosRequestConfig,
      data: { message: 'verification service unavailable' },
    }

    assert.equal(
      resolveServerErrorMessage(
        error,
        'Verification unavailable',
        'Content not found.'
      ),
      'verification service unavailable'
    )
  })

  test('uses the localized fallback for Axios transport failures', (): void => {
    const error = new AxiosError('Network Error', 'ERR_NETWORK')

    assert.equal(
      resolveServerErrorMessage(
        error,
        'Verification unavailable',
        'Content not found.'
      ),
      'Verification unavailable'
    )
  })

  test('keeps an Axios response title ahead of other messages', (): void => {
    const error = new AxiosError('request failed', 'ERR_BAD_REQUEST')
    error.response = {
      status: 400,
      statusText: 'Bad Request',
      headers: {},
      config: {} as InternalAxiosRequestConfig,
      data: {
        title: 'Invalid verification request',
        message: 'verification failed',
      },
    }

    assert.equal(
      resolveServerErrorMessage(
        error,
        'Verification unavailable',
        'Content not found.'
      ),
      'Invalid verification request'
    )
  })

  test('ignores empty error messages and uses the fallback', (): void => {
    assert.equal(
      resolveServerErrorMessage(
        new Error('   '),
        'Verification unavailable',
        'Content not found.'
      ),
      'Verification unavailable'
    )
  })

  test('preserves the content-not-found message for status 204', (): void => {
    assert.equal(
      resolveServerErrorMessage(
        { status: 204 },
        'Verification unavailable',
        'Content not found.'
      ),
      'Content not found.'
    )
  })

  test('preserves the content-not-found message for an Axios 204 response', (): void => {
    const error = new AxiosError('no content', 'ERR_BAD_REQUEST')
    error.response = {
      status: 204,
      statusText: 'No Content',
      headers: {},
      config: {} as InternalAxiosRequestConfig,
      data: { message: 'unexpected response message' },
    }

    assert.equal(
      resolveServerErrorMessage(
        error,
        'Verification unavailable',
        'Content not found.'
      ),
      'Content not found.'
    )
  })
})
