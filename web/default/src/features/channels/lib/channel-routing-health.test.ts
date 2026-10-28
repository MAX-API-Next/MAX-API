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
import { channelSchema } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from './channel-form'
import {
  readChannelRoutingHealth,
  summarizeChannelRoutingHealth,
  type ChannelRoutingHealth,
} from './channel-routing-health'
import { aggregateChannelsByTag, isTagAggregateRow } from './channel-utils'

function health(
  overrides: Partial<ChannelRoutingHealth> = {}
): ChannelRoutingHealth {
  return {
    schema_version: 1,
    status: 'normal',
    source: 'redis',
    base_priority: 10,
    effective_priority: 10,
    stored_penalty: 0,
    active_penalty: 0,
    stored_runtime_disabled: false,
    runtime_disabled: false,
    penalty_until: null,
    disabled_until: null,
    penalty_recovery_mode: null,
    disable_recovery_mode: 'probe_then_automatic',
    last_observed_at: null,
    evaluated_at: new Date().toISOString(),
    unavailable_reason: null,
    ...overrides,
  }
}

function channel(id: number, priority: number | null, routingHealth?: unknown) {
  return channelSchema.parse({
    id,
    priority,
    routing_health: routingHealth,
    name: `channel-${id}`,
    type: 1,
    key: '',
    status: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    tag: 'shared',
  })
}

describe('channel routing-health read contract', () => {
  test('preserves explicit zero, false and null without guessing unknown values', () => {
    const zero = health({ base_priority: 0, effective_priority: 0 })
    const parsed = readChannelRoutingHealth(channel(1, 0, zero).routing_health)
    assert.equal(parsed.kind, 'known')
    if (parsed.kind !== 'known') throw new Error('Expected known snapshot')
    assert.equal(parsed.health.effective_priority, 0)
    assert.equal(parsed.health.runtime_disabled, false)
    assert.equal(parsed.health.penalty_until, null)

    const unknown = readChannelRoutingHealth(
      health({
        status: 'unknown',
        source: 'unavailable',
        effective_priority: null,
        stored_penalty: null,
        active_penalty: null,
        stored_runtime_disabled: null,
        runtime_disabled: null,
        unavailable_reason: 'redis_read_failed',
      })
    )
    assert.equal(unknown.kind, 'known')
    if (unknown.kind !== 'known')
      throw new Error('Expected known unknown state')
    assert.equal(unknown.health.effective_priority, null)
    assert.equal(unknown.health.active_penalty, null)
  })

  test('keeps configuration usable for old, unsupported and invalid observations', () => {
    assert.equal(readChannelRoutingHealth(undefined).kind, 'missing')
    assert.equal(readChannelRoutingHealth(null).kind, 'missing')
    assert.equal(
      readChannelRoutingHealth({ schema_version: 2 }).kind,
      'unsupported'
    )
    assert.equal(
      readChannelRoutingHealth(health({ effective_priority: null })).kind,
      'invalid'
    )
    assert.equal(
      readChannelRoutingHealth({ ...health(), evaluated_at: 'broken' }).kind,
      'invalid'
    )
    assert.equal(channel(1, 10, { schema_version: 2 }).priority, 10)
    assert.equal(
      channel(1, 10, { schema_version: 1, status: 'broken' }).priority,
      10
    )
  })

  test('summarizes only supplied children with mixed, paused and unknown states', () => {
    const result = summarizeChannelRoutingHealth([
      channel(1, 10, health()),
      channel(
        2,
        10,
        health({
          status: 'penalized',
          effective_priority: 9,
          stored_penalty: 1,
          active_penalty: 1,
          penalty_recovery_mode: 'manual',
        })
      ),
      channel(
        3,
        10,
        health({
          status: 'runtime_disabled',
          effective_priority: null,
          stored_runtime_disabled: true,
          runtime_disabled: true,
        })
      ),
      channel(4, 10),
      channel(5, 10, { schema_version: 2 }),
    ])
    assert.deepEqual(result, {
      total: 5,
      penalized: 1,
      disabled: 1,
      unknown: 2,
      effectivePriority: null,
      multiplePriorities: true,
    })
    assert.equal(
      summarizeChannelRoutingHealth([
        channel(1, 0, health({ base_priority: 0, effective_priority: 0 })),
      ]).effectivePriority,
      0
    )
  })

  test('parent rows do not inherit the first child snapshot or misuse negative priorities', () => {
    const first = channel(
      1,
      -1,
      health({ base_priority: -1, effective_priority: -1 })
    )
    const second = channel(
      2,
      -2,
      health({ base_priority: -2, effective_priority: -2 })
    )
    const rows = aggregateChannelsByTag([first, second])
    assert.equal(rows.length, 1)
    const parent = rows[0]!
    assert.ok(isTagAggregateRow(parent))
    assert.equal(parent.routing_health, undefined)
    assert.equal(parent.priority, null)
    assert.equal(parent.children[0], first)
    assert.equal(first.priority, -1)
    assert.equal(readChannelRoutingHealth(first.routing_health).kind, 'known')
    assert.equal(
      summarizeChannelRoutingHealth(parent.children).multiplePriorities,
      true
    )
  })

  test('empty tags and default zero priorities have a stable aggregate', () => {
    const rows = aggregateChannelsByTag([
      { ...channel(1, null), tag: '' },
      { ...channel(2, 0), tag: null },
    ])
    assert.equal(rows.length, 1)
    assert.equal(rows[0]!.priority, 0)
    assert.equal(rows[0]!.routing_health, undefined)
  })

  test('editing a channel submits configured priority and excludes the response snapshot', () => {
    const source = channel(
      1,
      10,
      health({
        status: 'penalized',
        effective_priority: 9,
        stored_penalty: 1,
        active_penalty: 1,
        penalty_recovery_mode: 'manual',
      })
    )
    const defaults = transformChannelToFormDefaults(source)
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        ...defaults,
        priority: 12,
      },
      source.id
    )
    assert.equal(payload.priority, 12)
    assert.equal(Object.hasOwn(payload, 'routing_health'), false)
    assert.equal(Object.hasOwn(payload, 'effective_priority'), false)
    assert.equal(source.priority, 10)
  })
})
