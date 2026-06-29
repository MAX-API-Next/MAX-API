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
import { after, beforeEach, describe, test } from 'node:test'

const storage = new Map<string, string>()
const testLocalStorage = {
  getItem: (key: string) => storage.get(key) ?? null,
  removeItem: (key: string) => {
    storage.delete(key)
  },
  setItem: (key: string, value: string) => {
    storage.set(key, value)
  },
}
const previousLocalStorageDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'localStorage'
)
const previousWindowDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'window'
)

Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: testLocalStorage,
})
Object.defineProperty(globalThis, 'window', {
  configurable: true,
  value: {
    localStorage: testLocalStorage,
  },
})

const { useNotificationStore } = (await import(
  './notification-store'
)) as typeof import('./notification-store')
/* eslint-disable no-console */
const originalWarn = console.warn

console.warn = (...args: unknown[]) => {
  if (
    typeof args[0] === 'string' &&
    args[0].includes("Unable to update item 'notification-storage'")
  ) {
    return
  }

  originalWarn(...args)
}

after(() => {
  console.warn = originalWarn

  if (previousLocalStorageDescriptor) {
    Object.defineProperty(
      globalThis,
      'localStorage',
      previousLocalStorageDescriptor
    )
  } else {
    delete (globalThis as Partial<typeof globalThis>).localStorage
  }

  if (previousWindowDescriptor) {
    Object.defineProperty(globalThis, 'window', previousWindowDescriptor)
  } else {
    delete (globalThis as Partial<typeof globalThis>).window
  }
})
/* eslint-enable no-console */

describe('notification store', () => {
  beforeEach(() => {
    storage.clear()
    useNotificationStore.setState({
      byUser: {},
      autoOpenedSignatures: [],
    })
  })

  test('keeps read announcement state scoped by user', () => {
    const store = useNotificationStore.getState()

    store.markAnnouncementsRead('user:1', ['id:10'])

    assert.equal(store.isAnnouncementRead('user:1', 'id:10'), true)
    assert.equal(store.isAnnouncementRead('user:2', 'id:10'), false)
  })

  test('keeps close-for-today state scoped by user', () => {
    const today = new Date().toDateString()
    const store = useNotificationStore.getState()

    store.setClosedUntilDate('user:1', today)

    assert.equal(store.isNoticeClosed('user:1'), true)
    assert.equal(store.isNoticeClosed('user:2'), false)
  })

  test('migrates legacy persisted notification state to anonymous user scope', async () => {
    storage.set(
      'notification-storage',
      JSON.stringify({
        state: {
          lastReadNotice: 'legacy notice',
          readAnnouncementKeys: ['id:10'],
          closedUntilDate: 'Mon Jun 29 2026',
        },
        version: 0,
      })
    )

    await useNotificationStore.persist.rehydrate()

    const anonymousState =
      useNotificationStore.getState().getUserState('anonymous')
    assert.deepEqual(anonymousState, {
      lastReadNotice: 'legacy notice',
      readAnnouncementKeys: ['id:10'],
      closedUntilDate: 'Mon Jun 29 2026',
    })
  })

  test('remembers every scoped auto-open signature seen in this page session', () => {
    const store = useNotificationStore.getState()
    const userOneSignature = 'user:1:new-notice'
    const userTwoSignature = 'user:2:new-notice'

    assert.equal(store.rememberAutoOpenedSignature(userOneSignature), true)
    assert.equal(store.rememberAutoOpenedSignature(userTwoSignature), true)
    assert.equal(store.rememberAutoOpenedSignature(userOneSignature), false)
  })
})
