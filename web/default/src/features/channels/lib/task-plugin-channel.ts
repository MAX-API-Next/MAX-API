// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Settings reference: QuantumNous/new-api 8542c9d6 (AGPL-3.0-or-later).
import { z } from 'zod'
import type { PluginEntry } from '@/features/task-plugins/api'

const settingsSchema = z.object({
  task_plugin_key: z.string().default(''),
  task_plugin_upstream: z.string().default('vendor'),
  task_plugin_config: z
    .record(z.string(), z.union([z.string(), z.number(), z.boolean()]))
    .default({}),
})

export function readTaskPluginSettings(setting?: string) {
  try {
    return settingsSchema.parse(JSON.parse(setting || '{}'))
  } catch {
    return settingsSchema.parse({})
  }
}

export function taskPluginUpstreams(upstreams?: string[]): string[] {
  return upstreams?.length ? upstreams : ['vendor', 'new_api']
}

type ChannelConfig = NonNullable<PluginEntry['channel_config']>
export type TaskPluginParameterField = ChannelConfig['properties'][string]
type ParameterValues = ReturnType<
  typeof readTaskPluginSettings
>['task_plugin_config']

export function requiredTaskPluginParameters(schema?: ChannelConfig): string[] {
  if (!schema) return []
  return (schema.required ?? []).filter(
    (name) => schema.properties[name]?.default === undefined
  )
}

export function missingTaskPluginParameters(
  schema: ChannelConfig | undefined,
  config: ParameterValues
): string[] {
  return requiredTaskPluginParameters(schema).filter(
    (name) => !Object.hasOwn(config, name)
  )
}

export function acceptsTaskPluginParameter(
  field: TaskPluginParameterField,
  value: ParameterValues[string]
): boolean {
  if (field.type === 'string')
    return (
      typeof value === 'string' && (!field.enum || field.enum.includes(value))
    )
  if (field.type === 'boolean') return typeof value === 'boolean'
  return (
    typeof value === 'number' &&
    Number.isFinite(value) &&
    (field.type !== 'integer' || Number.isSafeInteger(value)) &&
    (field.minimum === undefined || value >= field.minimum) &&
    (field.maximum === undefined || value <= field.maximum)
  )
}

export function invalidTaskPluginParameters(
  schema: ChannelConfig | undefined,
  config: ParameterValues
): string[] {
  const fields = schema?.properties ?? {}
  return Object.keys(config).filter(
    (name) =>
      !Object.hasOwn(fields, name) ||
      !acceptsTaskPluginParameter(fields[name], config[name])
  )
}
