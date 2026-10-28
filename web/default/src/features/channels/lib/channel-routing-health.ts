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
import { z } from 'zod'

const nullableTime = z.string().datetime({ offset: true }).nullable()
const nullablePoints = z.number().int().nonnegative().nullable()

export const channelRoutingHealthSchema = z
  .object({
    schema_version: z.literal(1),
    status: z.enum([
      'normal',
      'penalized',
      'runtime_disabled',
      'policy_off',
      'unknown',
    ]),
    source: z.enum(['redis', 'policy_off', 'unavailable']),
    base_priority: z.number().int(),
    effective_priority: z.number().int().nullable(),
    stored_penalty: nullablePoints,
    active_penalty: nullablePoints,
    stored_runtime_disabled: z.boolean().nullable(),
    runtime_disabled: z.boolean().nullable(),
    penalty_until: nullableTime,
    disabled_until: nullableTime,
    penalty_recovery_mode: z.enum(['automatic', 'manual']).nullable(),
    disable_recovery_mode: z
      .enum(['probe_then_automatic', 'manual'])
      .nullable(),
    last_observed_at: nullableTime,
    evaluated_at: z.string().datetime({ offset: true }),
    unavailable_reason: z.string().nullable(),
  })
  .superRefine((health, ctx) => {
    let valid = true
    switch (health.status) {
      case 'normal':
        valid =
          health.source === 'redis' &&
          health.effective_priority !== null &&
          health.active_penalty === 0 &&
          health.runtime_disabled === false
        break
      case 'penalized':
        valid =
          health.source === 'redis' &&
          health.effective_priority !== null &&
          health.active_penalty !== null &&
          health.active_penalty > 0 &&
          health.runtime_disabled === false &&
          health.penalty_recovery_mode !== null
        break
      case 'runtime_disabled':
        valid =
          health.source === 'redis' &&
          health.effective_priority === null &&
          health.runtime_disabled === true
        break
      case 'policy_off':
        valid =
          health.source === 'policy_off' &&
          health.effective_priority === health.base_priority &&
          health.active_penalty === 0 &&
          health.runtime_disabled === false
        break
      case 'unknown':
        valid =
          health.source === 'unavailable' &&
          health.effective_priority === null &&
          health.active_penalty === null &&
          health.runtime_disabled === null
        break
    }
    if (!valid) {
      ctx.addIssue({ code: 'custom', message: 'Inconsistent runtime snapshot' })
    }
  })

export type ChannelRoutingHealth = z.infer<typeof channelRoutingHealthSchema>

export type ChannelRoutingHealthObservation =
  | { kind: 'known'; health: ChannelRoutingHealth }
  | { kind: 'missing' | 'unsupported' | 'invalid' }

// Validate the optional observation independently so a malformed or newer
// snapshot cannot prevent administrators from reading/editing configuration.
export function readChannelRoutingHealth(
  value: unknown
): ChannelRoutingHealthObservation {
  if (value == null) return { kind: 'missing' }
  if (
    typeof value === 'object' &&
    'schema_version' in value &&
    typeof value.schema_version === 'number' &&
    value.schema_version !== 1
  ) {
    return { kind: 'unsupported' }
  }
  const parsed = channelRoutingHealthSchema.safeParse(value)
  if (!parsed.success) return { kind: 'invalid' }
  return { kind: 'known', health: parsed.data }
}

export interface ChannelRoutingHealthSummary {
  total: number
  penalized: number
  disabled: number
  unknown: number
  effectivePriority: number | null
  multiplePriorities: boolean
}

export function summarizeChannelRoutingHealth(
  channels: ReadonlyArray<{ routing_health?: unknown }>
): ChannelRoutingHealthSummary {
  const summary: ChannelRoutingHealthSummary = {
    total: channels.length,
    penalized: 0,
    disabled: 0,
    unknown: 0,
    effectivePriority: null,
    multiplePriorities: false,
  }
  const priorities = new Set<number>()
  for (const channel of channels) {
    const observation = readChannelRoutingHealth(channel.routing_health)
    if (observation.kind !== 'known') {
      summary.unknown += 1
      continue
    }
    const health = observation.health
    if (health.status === 'unknown') summary.unknown += 1
    if (health.status === 'penalized') summary.penalized += 1
    if (health.status === 'runtime_disabled') summary.disabled += 1
    if (health.effective_priority !== null) {
      priorities.add(health.effective_priority)
    }
  }
  summary.multiplePriorities = priorities.size > 1
  if (priorities.size === 1) {
    summary.effectivePriority = priorities.values().next().value ?? null
  }
  return summary
}
