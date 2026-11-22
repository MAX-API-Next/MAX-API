// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  emptyTaskTier,
  generateTaskVisualExpression,
  parseTaskVisualExpression,
  type TaskVisualExpression,
  type TaskVisualSchema,
} from './task-visual-expression'

const schema: TaskVisualSchema = {
  units: { type: 'number', unit: 'credit' },
  tokens: { type: 'number', unit: 'token' },
  audio: { type: 'boolean' },
  kind: { type: 'enum', enum: ['std', 'pro: "audio"'] },
}

test('schema-based task tiers preserve zero rates, omitted facts and token scaling', () => {
  const config: TaskVisualExpression = {
    prefix: 'v1:',
    tiers: [
      {
        label: 'pro: "audio"',
        flat: 0.05,
        conditions: [
          { field: 'units', operator: '>=', value: 1 },
          { field: 'kind', operator: '==', value: 'pro: "audio"' },
          { field: 'audio', operator: '!=', value: false },
        ],
        rates: { units: 0.2, tokens: 0 },
      },
      { ...emptyTaskTier(), rates: { units: 0.1 } },
    ],
  }
  const expression = generateTaskVisualExpression(config, schema)
  assert.match(expression, /u\("tokens"\) \* 0 \/ 1000000/)
  assert.match(expression, /tier\("default", u\("units"\) \* 0\.1\)$/)
  assert.deepEqual(parseTaskVisualExpression(expression, schema), config)
  assert.deepEqual(parseTaskVisualExpression('0', schema)?.tiers, [
    emptyTaskTier(''),
  ])
  assert.deepEqual(parseTaskVisualExpression('', schema)?.tiers, [
    emptyTaskTier(),
  ])
})

test('unlabelled task prices reopen and edit without adding a matched tier name', () => {
  const source = 'v1:0.05 + u("units") * 0.2'
  const parsed = parseTaskVisualExpression(source, schema)
  assert.ok(parsed)
  assert.equal(generateTaskVisualExpression(parsed, schema), source)
  parsed.tiers[0].rates.units = 0.5
  assert.equal(
    generateTaskVisualExpression(parsed, schema),
    'v1:0.05 + u("units") * 0.5'
  )
})

test('task readback refuses forms that would change evaluation or omit source text', () => {
  for (const expression of [
    'tier("default", u("tokens") * 2)',
    'tier("default", u("unavailable") * 0)',
    'tier("default", u("units") * 2 + u("units") * 3)',
    'tier("default", (u("units") * 2) + 1)',
    'tier("default", u("units") * 1e-999)',
    'u("audio") == true || u("kind") == "std" ? tier("a", 1) : tier("default", 0)',
    'u("audio") == 0 ? tier("a", 1) : tier("default", 0)',
    'tier("default", 1) :',
    'v2:tier("default", 1)',
    'tier("default", 1) * (hour("UTC") == 0 ? 2 : 1)',
  ]) {
    assert.equal(
      parseTaskVisualExpression(expression, schema),
      null,
      expression
    )
  }
})
