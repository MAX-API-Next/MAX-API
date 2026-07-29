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
import { createSessionVerifier } from './session-verifier'

type TestUser = { id: number; username: string }

function createAuthState(initialUser: TestUser | null = null) {
  const state = {
    user: initialUser,
    setUser(user: TestUser | null) {
      state.user = user
    },
    reset() {
      state.user = null
    },
  }
  return state
}

describe('session verifier', () => {
  test('recovers a cold-start session from the backend', async () => {
    const auth = createAuthState()
    let calls = 0
    const verifier = createSessionVerifier({
      getAuthState: () => auth,
      getSelf: async () => {
        calls++
        return { success: true, data: { id: 1, username: 'session-user' } }
      },
      getUserIdentity: (user) => user.id,
      isUnauthorized: () => false,
    })

    assert.equal(await verifier.verify(), true)
    assert.deepEqual(auth.user, { id: 1, username: 'session-user' })
    assert.equal(calls, 1)
  })

  test('revalidates after the bounded verification window', async () => {
    const auth = createAuthState({ id: 1, username: 'session-user' })
    let now = 1_000
    let calls = 0
    const verifier = createSessionVerifier({
      getAuthState: () => auth,
      getSelf: async () => {
        calls++
        return { success: true, data: auth.user ?? undefined }
      },
      getUserIdentity: (user) => user.id,
      isUnauthorized: () => false,
      now: () => now,
      revalidateAfterMs: 100,
    })

    assert.equal(await verifier.verify(), true)
    now += 99
    assert.equal(await verifier.verify(), true)
    assert.equal(calls, 1)

    now += 1
    assert.equal(await verifier.verify(), true)
    assert.equal(calls, 2)
  })

  test('revalidates when the in-memory user changes inside the cache window', async () => {
    const auth = createAuthState({ id: 1, username: 'first-user' })
    let calls = 0
    const verifier = createSessionVerifier({
      getAuthState: () => auth,
      getSelf: async () => {
        calls++
        return { success: true, data: auth.user ?? undefined }
      },
      getUserIdentity: (user) => user.id,
      isUnauthorized: () => false,
      revalidateAfterMs: 30_000,
    })

    assert.equal(await verifier.verify(), true)
    auth.setUser({ id: 2, username: 'second-user' })
    assert.equal(await verifier.verify(), true)
    assert.equal(calls, 2)
    assert.deepEqual(auth.user, { id: 2, username: 'second-user' })
  })

  test('deduplicates concurrent backend verification', async () => {
    const auth = createAuthState()
    let calls = 0
    let resolveResponse!: (value: { success: boolean; data: TestUser }) => void
    const response = new Promise<{ success: boolean; data: TestUser }>(
      (resolve) => {
        resolveResponse = resolve
      }
    )
    const verifier = createSessionVerifier({
      getAuthState: () => auth,
      getSelf: () => {
        calls++
        return response
      },
      getUserIdentity: (user) => user.id,
      isUnauthorized: () => false,
    })

    const first = verifier.verify()
    const second = verifier.verify()
    resolveResponse({
      success: true,
      data: { id: 1, username: 'session-user' },
    })

    assert.equal(await first, true)
    assert.equal(await second, true)
    assert.equal(calls, 1)
  })

  test('does not reuse or apply an in-flight response after the user changes', async () => {
    const firstUser = { id: 1, username: 'first-user' }
    const secondUser = { id: 2, username: 'second-user' }
    const auth = createAuthState(firstUser)
    let calls = 0
    let resolveFirst!: (value: { success: boolean; data: TestUser }) => void
    const firstResponse = new Promise<{ success: boolean; data: TestUser }>(
      (resolve) => {
        resolveFirst = resolve
      }
    )
    const verifier = createSessionVerifier({
      getAuthState: () => auth,
      getSelf: () => {
        calls++
        if (calls === 1) return firstResponse
        return Promise.resolve({ success: true, data: secondUser })
      },
      getUserIdentity: (user) => user.id,
      isUnauthorized: () => false,
    })

    const first = verifier.verify()
    auth.setUser(secondUser)
    const second = verifier.verify()
    resolveFirst({ success: true, data: firstUser })

    assert.deepEqual(await Promise.all([first, second]), [true, true])
    assert.equal(calls, 2)
    assert.deepEqual(auth.user, secondUser)
  })

  test('clears memory state when the backend reports an expired session', async () => {
    const auth = createAuthState({ id: 1, username: 'session-user' })
    const verifier = createSessionVerifier({
      getAuthState: () => auth,
      getSelf: async () => {
        throw { response: { status: 401 } }
      },
      getUserIdentity: (user) => user.id,
      isUnauthorized: (error) =>
        (error as { response?: { status?: number } })?.response?.status === 401,
    })

    assert.equal(await verifier.verify(), false)
    assert.equal(auth.user, null)
  })
})
