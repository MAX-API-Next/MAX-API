// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api } from '@/lib/api'
import {
  taskPluginAPI,
  PluginError,
  snapshotSchema,
  type PluginEntry,
} from './api'

const entry: PluginEntry = {
  key: 'kling',
  name: 'Kling',
  version: '1.2.0',
  digest: 'a'.repeat(64),
  format_version: 2,
  active: true,
  enabled: true,
  revoked: false,
  models: ['kling-v3'],
  protocols: [],
  routes: [],
  usage_schema: { units: { type: 'number', unit: 'credit' } },
  usage_profiles: [],
  execution_ready: false,
  admission_ready: false,
}
const original = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = original
})

test('known mapping repair errors preserve identifiers for reactive feedback', async () => {
  const mutate = () =>
    taskPluginAPI.mutate({
      action: 'delete',
      key: 'kling',
      version: '1.2.0',
      expected_revision: '4',
      deletion_nodes_ready: true,
    })
  for (const channelId of [
    23,
    -23,
    '0',
    '9007199254740993',
    '-9007199254740993',
  ]) {
    respond(
      {
        success: false,
        code: 'invalid_channel_mapping',
        channel_id: channelId,
        message: 'English fallback',
      },
      422
    )
    await assert.rejects(
      mutate,
      (e) =>
        e instanceof PluginError &&
        e.kind === 'rejected' &&
        e.message === 'English fallback' &&
        e.repair?.code === 'invalid_channel_mapping' &&
        e.repair.channel_id === String(channelId)
    )
  }
  for (const channelId of [
    Number.MIN_SAFE_INTEGER - 1,
    Number.MAX_SAFE_INTEGER + 1,
    '-0',
    '+23',
  ]) {
    respond(
      {
        success: false,
        code: 'invalid_channel_mapping',
        channel_id: channelId,
        message: 'English fallback',
      },
      422
    )
    await assert.rejects(mutate, (e) => e instanceof PluginError && !e.repair)
  }
})
function respond(data: unknown, status = 200) {
  const calls: InternalAxiosRequestConfig[] = []
  api.defaults.adapter = async (config) => {
    calls.push(config)
    const response = {
      config,
      status,
      statusText: 'fixture',
      headers: {},
      data,
    }
    if (status >= 400)
      throw new AxiosError(
        'controlled-error',
        undefined,
        config,
        undefined,
        response
      )
    return response
  }
  return calls
}

test('catalog API preserves decimal revisions and typed false readiness', async () => {
  const calls = respond({
    success: true,
    data: { revision: '9007199254740993', entries: [entry] },
  })
  const result = await taskPluginAPI.read()
  assert.equal(result.revision, '9007199254740993')
  assert.equal(result.entries[0].admission_ready, false)
  assert.equal(calls[0].disableDuplicate, true)
  assert.equal(
    snapshotSchema.safeParse({ revision: '9223372036854775808', entries: [] })
      .success,
    false
  )
  for (const invalid of ['abc', '-1', '01', 1])
    assert.equal(
      snapshotSchema.safeParse({ revision: invalid, entries: [] }).success,
      false
    )
})
test('validation rejects a receipt for different pinned bytes', async () => {
  respond({ success: true, data: entry })
  await assert.rejects(
    () =>
      taskPluginAPI.validate({
        document: 'synthetic',
        expected_sha256: 'b'.repeat(64),
      }),
    (e) =>
      e instanceof PluginError &&
      e.kind === 'read' &&
      e.message === 'Invalid task plugin response'
  )
})

test('API fallback errors retain the established translation keys', async () => {
  for (const [payload, message, kind] of [
    [{ success: false }, 'Invalid task plugin request', 'rejected'],
    [{ success: true, data: {} }, 'Invalid task plugin response', 'read'],
  ] as const) {
    const calls = respond(payload)
    await assert.rejects(
      () => taskPluginAPI.read(),
      (e) =>
        e instanceof PluginError && e.message === message && e.kind === kind
    )
    assert.equal(calls.length, 1)
  }
})

test('custom uploads may omit or leave the external SHA-256 empty', async () => {
  for (const upload of [
    { document: 'custom upload' },
    { document: 'custom upload', expected_sha256: '' },
  ]) {
    const calls = respond({ success: true, data: entry })
    const result = await taskPluginAPI.validate(upload)
    assert.equal(result.digest, entry.digest)
    assert.equal(calls.length, 1)
    const sent = JSON.parse(calls[0].data)
    assert.equal(
      Object.hasOwn(sent, 'expected_sha256'),
      Object.hasOwn(upload, 'expected_sha256')
    )
    if (Object.hasOwn(upload, 'expected_sha256'))
      assert.equal(sent.expected_sha256, '')
  }
})

test('official JavaScript validates its source pin independently of the stored envelope', async () => {
  respond({
    success: true,
    data: { ...entry, format_version: 3, source_sha256: 'b'.repeat(64) },
  })
  const result = await taskPluginAPI.validate({
    document: 'export const meta = {}',
    expected_sha256: 'b'.repeat(64),
  })
  assert.equal(result.format_version, 3)
  assert.equal(result.digest, 'a'.repeat(64))
  await taskPluginAPI.validate({
    document: 'synthetic stored envelope',
    expected_sha256: 'a'.repeat(64),
  })
  await assert.rejects(
    () =>
      taskPluginAPI.validate({
        document: 'synthetic',
        expected_sha256: 'c'.repeat(64),
      }),
    PluginError
  )
})
test('mutations classify conflicts and unknown receipts without retrying', async () => {
  for (const [status, kind] of [
    [409, 'conflict'],
    [400, 'rejected'],
    [422, 'rejected'],
    [408, 'unknown'],
    [499, 'unknown'],
    [503, 'unknown'],
  ] as const) {
    const calls = respond({ message: 'controlled-error' }, status)
    await assert.rejects(
      () =>
        taskPluginAPI.mutate({
          action: 'disable',
          key: 'kling',
          version: '1.2.0',
          expected_revision: '4',
        }),
      (e) => e instanceof PluginError && e.kind === kind
    )
    assert.equal(calls.length, 1)
  }
  for (const status of [408, 499, 503]) {
    const reads = respond({ message: 'controlled-error' }, status)
    await assert.rejects(
      () => taskPluginAPI.read(),
      (e) => e instanceof PluginError && e.kind === 'read'
    )
    assert.equal(reads.length, 1)
  }
  const calls = respond({
    success: true,
    data: {
      committed: true,
      revision: '8',
      execution_ready: false,
      admission_ready: false,
    },
  })
  await assert.rejects(
    () =>
      taskPluginAPI.mutate({
        action: 'delete',
        key: 'kling',
        version: '1.2.0',
        expected_revision: '4',
        deletion_nodes_ready: true,
      }),
    (e) => e instanceof PluginError && e.kind === 'unknown'
  )
  assert.equal(calls.length, 1)
})

test('only explicit not-committed 503 responses reject a mutation safely', async () => {
  for (const [payload, kind] of [
    [{ code: 'catalog_operation_not_committed', committed: false }, 'rejected'],
    [
      {
        code: 'catalog_operation_not_committed',
        success: true,
        committed: false,
      },
      'unknown',
    ],
    [
      {
        code: 'catalog_operation_not_committed',
        success: undefined,
        committed: false,
      },
      'unknown',
    ],
    [{ committed: false }, 'unknown'],
    [{ committed: true }, 'unknown'],
    [{ committed: 'false' }, 'unknown'],
    [{}, 'unknown'],
  ] as const) {
    const calls = respond({ success: false, ...payload }, 503)
    await assert.rejects(
      () =>
        taskPluginAPI.mutate({
          action: 'disable',
          key: 'kling',
          version: '1.2.0',
          expected_revision: '4',
        }),
      (e) => e instanceof PluginError && e.kind === kind
    )
    assert.equal(calls.length, 1)
  }
  respond({ success: false, committed: false }, 503)
  await assert.rejects(
    () => taskPluginAPI.read(),
    (e) => e instanceof PluginError && e.kind === 'read'
  )
})

test('known no-commit code maps to an established translated recovery instruction', async () => {
  respond(
    {
      success: false,
      code: 'catalog_operation_not_committed',
      committed: false,
      message:
        'Task plugin catalog mutation stopped before commit; refresh and retry',
    },
    503
  )
  await assert.rejects(
    () =>
      taskPluginAPI.mutate({
        action: 'disable',
        key: 'kling',
        version: '1.2.0',
        expected_revision: '4',
      }),
    (e) =>
      e instanceof PluginError &&
      e.kind === 'rejected' &&
      e.message ===
        'Task plugin catalog unavailable; reread before another mutation'
  )
})
