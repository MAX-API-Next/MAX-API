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

let storageAccesses = 0
const forbiddenLocalStorage = {
  getItem: () => {
    storageAccesses++
    throw new Error('auth store must not read localStorage')
  },
  removeItem: () => {
    storageAccesses++
    throw new Error('auth store must not write localStorage')
  },
  setItem: () => {
    storageAccesses++
    throw new Error('auth store must not write localStorage')
  },
}

const previousWindowDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'window'
)
Object.defineProperty(globalThis, 'window', {
  configurable: true,
  value: { localStorage: forbiddenLocalStorage },
})

const { useAuthStore } =
  (await import('./auth-store')) as typeof import('./auth-store')

after(() => {
  if (previousWindowDescriptor) {
    Object.defineProperty(globalThis, 'window', previousWindowDescriptor)
  } else {
    delete (globalThis as Partial<typeof globalThis>).window
  }
})

describe('auth store', () => {
  beforeEach(() => {
    storageAccesses = 0
    useAuthStore.setState((state) => ({
      ...state,
      auth: { ...state.auth, user: null },
    }))
  })

  test('keeps user state in memory without localStorage access', () => {
    const user = { id: 1, username: 'session-user', role: 1 }

    useAuthStore.getState().auth.setUser(user)
    assert.deepEqual(useAuthStore.getState().auth.user, user)

    useAuthStore.getState().auth.reset()
    assert.equal(useAuthStore.getState().auth.user, null)
    assert.equal(storageAccesses, 0)
  })
})
