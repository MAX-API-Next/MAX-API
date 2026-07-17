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
import type { QueryClient } from '@tanstack/react-query'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  createChannelTestCachePatch,
  updateChannelTestCache,
} from './channel-actions'

describe('channel test cache patch helpers', () => {
  test('creates a cache patch from a finite response time', () => {
    const originalNow = Date.now
    Date.now = () => 1_234_567_890_123

    try {
      assert.deepEqual(createChannelTestCachePatch(345), {
        responseTime: 345,
        testTime: 1_234_567_890,
      })
      assert.equal(createChannelTestCachePatch(undefined), undefined)
    } finally {
      Date.now = originalNow
    }
  })

  test('updates the matching channel entry in list caches', () => {
    const oldData = {
      success: true,
      data: {
        items: [
          { id: 1, response_time: 10, test_time: 11 },
          { id: 2, response_time: 20, test_time: 21 },
        ],
        total: 2,
        page: 1,
        page_size: 20,
      },
    }

    let nextData = oldData
    const queryClient = {
      setQueriesData: (
        _options: unknown,
        updater: (value: typeof oldData) => typeof oldData
      ) => {
        nextData = updater(nextData)
      },
    } as unknown as QueryClient

    updateChannelTestCache(queryClient, 2, {
      responseTime: 99,
      testTime: 123,
    })

    assert.equal(nextData.data.items[0].response_time, 10)
    assert.equal(nextData.data.items[0].test_time, 11)
    assert.equal(nextData.data.items[1].response_time, 99)
    assert.equal(nextData.data.items[1].test_time, 123)
  })
})
