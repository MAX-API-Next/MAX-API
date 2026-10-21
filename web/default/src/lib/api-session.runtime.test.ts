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
import { QueryCache, QueryClient } from '@tanstack/react-query'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { appSessionVerifier } from '@/features/auth/lib/app-session-verifier'
import { api } from './api'
import { handleQueryAuthenticationError } from './auth-errors'

test('anonymous no-op assignment preserves a pending GET while explicit reset invalidates it', async () => {
  const adapter = api.defaults.adapter,
    user = useAuthStore.getState().auth.user
  const releases: Array<() => void> = [],
    calls: Array<Promise<unknown>> = []
  let transports = 0
  api.defaults.adapter = async (config) => {
    transports += 1
    await new Promise<void>((resolve) => releases.push(resolve))
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true },
    }
  }
  try {
    useAuthStore.getState().auth.reset()
    const revision = useAuthStore.getState().sessionRevision
    const first = api.get('/test/anonymous-no-op')
    calls.push(first)
    useAuthStore.getState().auth.setUser(null)
    assert.equal(useAuthStore.getState().sessionRevision, revision)
    assert.equal(api.get('/test/anonymous-no-op'), first)
    useAuthStore.getState().auth.reset()
    assert.equal(useAuthStore.getState().sessionRevision, revision + 1)
    const next = api.get('/test/anonymous-no-op')
    calls.push(next)
    assert.notEqual(next, first)
    assert.equal(transports, 2)
  } finally {
    releases.forEach((resolve) => resolve())
    await Promise.allSettled(calls)
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(user)
  }
})

test('GET dedup stays within a login session, including same-user logout and login', async () => {
  const adapter = api.defaults.adapter
  const user = useAuthStore.getState().auth.user
  const pending: Array<() => void> = []
  let transports = 0
  api.defaults.adapter = async (config) => {
    const owner = useAuthStore.getState().auth.user?.id
    transports += 1
    await new Promise<void>((resolve) => pending.push(resolve))
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { owner },
    }
  }
  const calls: Array<Promise<unknown>> = []
  try {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'first', role: 1 })
    const first = api.get('/test/session-data')
    calls.push(first)
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(transports, 1)
    useAuthStore.getState().auth.setUser({ id: 2, username: 'second', role: 1 })
    const second = api.get('/test/session-data')
    calls.push(second)
    assert.notEqual(second, first)
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(transports, 2)
    pending[0]()
    assert.equal((await first).data.owner, 1)
    assert.equal(api.get('/test/session-data'), second)
    useAuthStore
      .getState()
      .auth.setUser({ id: 2, username: 'second', role: 1, quota: 0 })
    assert.equal(api.get('/test/session-data'), second)
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'second',
      role: 1,
      permissions: { sidebar_settings: false },
    })
    const permissionsChanged = api.get('/test/session-data')
    calls.push(permissionsChanged)
    assert.notEqual(permissionsChanged, second)
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(transports, 3)
    useAuthStore.getState().auth.reset()
    useAuthStore.getState().auth.setUser({ id: 2, username: 'second', role: 1 })
    const relogin = api.get('/test/session-data')
    calls.push(relogin)
    assert.notEqual(relogin, permissionsChanged)
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(transports, 4)
  } finally {
    pending.forEach((resolve) => resolve())
    await Promise.allSettled(calls)
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(user)
  }
})

test('a late 401 rejects the original call without logging out a newer session', async () => {
  const adapter = api.defaults.adapter
  const user = useAuthStore.getState().auth.user
  let rejectRequest: (() => void) | undefined
  let started!: () => void
  const ready = new Promise<void>((resolve) => {
    started = resolve
  })
  api.defaults.adapter = (config: InternalAxiosRequestConfig) =>
    new Promise((_, reject) => {
      rejectRequest = () =>
        reject(
          new AxiosError('Expired', 'ERR_BAD_REQUEST', config, undefined, {
            config,
            status: 401,
            statusText: 'Unauthorized',
            headers: {},
            data: {},
          })
        )
      started()
    })
  try {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'old', role: 1 })
    const old = api.post('/test/no-retry', {}, { skipErrorHandler: true })
    const rejected = assert.rejects(old, /Expired/)
    await ready
    useAuthStore.getState().auth.setUser({ id: 2, username: 'new', role: 1 })
    rejectRequest!()
    await rejected
    assert.equal(useAuthStore.getState().auth.user?.id, 2)
    const current = api.get('/test/current', { skipErrorHandler: true })
    const currentRejected = assert.rejects(current, /Expired/)
    await new Promise<void>((resolve) => setImmediate(resolve))
    rejectRequest!()
    await currentRejected
    assert.equal(useAuthStore.getState().auth.user, null)
  } finally {
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(user)
  }
})

test('query-cache error handling must not let an old 401 expire a newer session', async () => {
  const adapter = api.defaults.adapter,
    user = useAuthStore.getState().auth.user
  let rejectRequest!: () => void,
    started!: () => void,
    expired = 0
  const ready = new Promise<void>((resolve) => {
    started = resolve
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
    queryCache: new QueryCache({
      onError: (error) =>
        handleQueryAuthenticationError(error, () => {
          expired += 1
          useAuthStore.getState().auth.reset()
        }),
    }),
  })
  api.defaults.adapter = (config) =>
    new Promise((_, reject) => {
      rejectRequest = () =>
        reject(
          new AxiosError(
            'Expired query',
            'ERR_BAD_REQUEST',
            config,
            undefined,
            {
              config,
              status: 401,
              statusText: 'Unauthorized',
              headers: {},
              data: {},
            }
          )
        )
      started()
    })
  try {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'old', role: 1 })
    const failed = assert.rejects(
      client.fetchQuery({
        queryKey: ['old-query'],
        queryFn: () => api.get('/test/old-query', { skipErrorHandler: true }),
      }),
      /Expired query/
    )
    await ready
    useAuthStore.getState().auth.setUser({ id: 2, username: 'new', role: 1 })
    rejectRequest()
    await failed
    assert.equal(expired, 0)
    assert.equal(useAuthStore.getState().auth.user?.id, 2)
    const currentFailed = assert.rejects(
      client.fetchQuery({
        queryKey: ['current-query'],
        queryFn: () =>
          api.get('/test/current-query', { skipErrorHandler: true }),
      }),
      /Expired query/
    )
    await new Promise<void>((resolve) => setImmediate(resolve))
    rejectRequest()
    await currentFailed
    assert.equal(expired, 1)
    assert.equal(useAuthStore.getState().auth.user, null)
  } finally {
    client.clear()
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(user)
  }
})

test('session verification cache is invalidated by same-user logout and login', async () => {
  const adapter = api.defaults.adapter,
    user = useAuthStore.getState().auth.user
  let requests = 0
  api.defaults.adapter = async (config) => {
    requests += 1
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: { id: 1, username: 'verified-user', role: 1 },
      },
    }
  }
  try {
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'verified-user', role: 1 })
    assert.equal(await appSessionVerifier.verify(), true)
    assert.equal(requests, 1)
    useAuthStore.getState().auth.reset()
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'verified-user', role: 1 })
    assert.equal(await appSessionVerifier.verify(), true)
    assert.equal(requests, 2)
  } finally {
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(user)
  }
})

test('an anonymous verification cannot publish an older session user after reset', async () => {
  const adapter = api.defaults.adapter,
    user = useAuthStore.getState().auth.user
  let release!: () => void,
    started!: () => void,
    requests = 0
  const ready = new Promise<void>((resolve) => {
    started = resolve
  })
  api.defaults.adapter = async (config) => {
    const id = ++requests === 1 ? 11 : 22
    if (id === 11) {
      started()
      await new Promise<void>((resolve) => {
        release = resolve
      })
    }
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: { id, username: `verified-${id}`, role: 1 },
      },
    }
  }
  try {
    useAuthStore.getState().auth.reset()
    const verifying = appSessionVerifier.verify()
    await ready
    useAuthStore.getState().auth.reset()
    release()
    assert.equal(await verifying, true)
    assert.equal(useAuthStore.getState().auth.user?.id, 22)
    assert.equal(requests, 2)
  } finally {
    release?.()
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(user)
  }
})
