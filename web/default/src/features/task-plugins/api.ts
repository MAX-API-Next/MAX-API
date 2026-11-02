// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Interaction reference: QuantumNous/new-api 70e6af64 task-plugins/api.ts.
// Independently adapted to MAX's pinned package transport and catalog CAS.
import { z } from 'zod'
import { isAxiosError } from 'axios'
import { api } from '@/lib/api'

const revision = z
  .string()
  .regex(/^(0|[1-9][0-9]*)$/)
  .refine(
    (value) =>
      /^(0|[1-9][0-9]*)$/.test(value) &&
      value.length <= 19 &&
      BigInt(value) <= 9223372036854775807n
  )
const digest = z.string().regex(/^[a-f0-9]{64}$/)
const usage = z.object({
  type: z.enum(['number', 'boolean', 'enum']),
  unit: z.string().optional(),
  enum: z.array(z.string()).optional(),
})
export const pluginSchema = z.object({
  key: z.string(),
  name: z.string(),
  version: z.string(),
  digest,
  format_version: z.union([z.literal(1), z.literal(2)]),
  active: z.boolean(),
  enabled: z.boolean(),
  revoked: z.boolean(),
  models: z.array(z.string()),
  protocols: z.array(
    z.object({ name: z.string(), supports: z.array(z.string()) })
  ),
  routes: z.array(
    z.object({ method: z.string(), path: z.string(), type: z.string() })
  ),
  usage_schema: z.record(z.string(), usage),
  usage_profiles: z.array(
    z.object({
      models: z.array(z.string()),
      schema: z.record(z.string(), usage),
    })
  ),
  execution_ready: z.literal(false),
  admission_ready: z.literal(false),
})
export const snapshotSchema = z.object({
  revision,
  entries: z.array(pluginSchema),
})
const receiptSchema = z.object({
  committed: z.literal(true),
  revision,
  execution_ready: z.literal(false),
  admission_ready: z.literal(false),
})
export type PluginEntry = z.infer<typeof pluginSchema>
export type PluginSnapshot = z.infer<typeof snapshotSchema>
export type PluginAction = 'activate' | 'enable' | 'disable' | 'revoke'
export type PluginUpload = { document: string; expected_sha256: string }
export type PluginMutation = {
  expected_revision: string
  format2_nodes_ready?: boolean
} & (
  | ({ action: 'install' } & PluginUpload)
  | { action: PluginAction; key: string; version: string }
)

export class PluginError extends Error {
  constructor(
    public readonly kind: 'conflict' | 'rejected' | 'unknown' | 'read',
    message: string
  ) {
    super(message)
  }
}
const options = { skipBusinessError: true, skipErrorHandler: true }
const endpoint = '/api/task_plugins'
async function request<T>(
  run: () => Promise<{ data: unknown }>,
  schema: z.ZodType<T>,
  write = false
): Promise<T> {
  try {
    const raw = z
      .object({
        success: z.boolean(),
        message: z.string().optional(),
        data: z.unknown().optional(),
      })
      .parse((await run()).data)
    if (!raw.success)
      throw new PluginError(
        'rejected',
        raw.message || 'Invalid task plugin request'
      )
    return schema.parse(raw.data)
  } catch (error) {
    if (error instanceof PluginError) throw error
    if (isAxiosError(error)) {
      const status = error.response?.status
      const message: unknown = error.response?.data?.message
      const kind =
        status === 409
          ? 'conflict'
          : status &&
              status >= 400 &&
              status < 500 &&
              status !== 408 &&
              status !== 499
            ? 'rejected'
            : write
              ? 'unknown'
              : 'read'
      throw new PluginError(
        kind,
        typeof message === 'string' ? message : 'Task plugin request failed'
      )
    }
    throw new PluginError(
      write ? 'unknown' : 'read',
      'Invalid task plugin response'
    )
  }
}
export const taskPluginAPI = {
  read: (signal?: AbortSignal) =>
    request(
      () => api.get(endpoint, { ...options, signal, disableDuplicate: true }),
      snapshotSchema
    ),
  validate: async (upload: PluginUpload) => {
    const entry = await request(
      () => api.post(`${endpoint}/validate`, upload, options),
      pluginSchema
    )
    if (entry.digest !== upload.expected_sha256)
      throw new PluginError('read', 'Invalid task plugin response')
    return entry
  },
  mutate: async (command: PluginMutation) => {
    const receipt = await request(
      () => api.post(`${endpoint}/mutate`, command, options),
      receiptSchema,
      true
    )
    if (BigInt(receipt.revision) !== BigInt(command.expected_revision) + 1n)
      throw new PluginError('unknown', 'Invalid task plugin response')
    return receipt
  },
}
export type TaskPluginAPI = typeof taskPluginAPI

// Serial UI observations: superseded reads/file validation cannot overwrite a
// newer input, and an ambiguous mutation never becomes success after a GET.
export class PluginRequestOrder {
  private generation = 0
  next() {
    return ++this.generation
  }
  current(id: number) {
    return id === this.generation
  }
}
