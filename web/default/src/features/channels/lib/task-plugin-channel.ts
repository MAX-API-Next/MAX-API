// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Settings reference: QuantumNous/new-api 8542c9d6 (AGPL-3.0-or-later).
import { z } from 'zod'

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
