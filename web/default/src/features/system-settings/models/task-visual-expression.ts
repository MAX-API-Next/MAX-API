// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Tier conversion adapted from QuantumNous/new-api 8542c9d6,
// web/src/features/pricing/lib/task-expr.ts (AGPL-3.0-or-later).
import {
  isSupportedNumericLiteral,
  splitTopLevelExpression,
} from '@/features/pricing/lib/expression-syntax'
import type { TaskUsageField } from './task-pricing-contract'

type Scalar = number | boolean | string
export type TaskVisualSchema = Record<string, TaskUsageField>
export type TaskCondition = {
  field: string
  operator: '==' | '!=' | '>' | '>=' | '<' | '<='
  value: Scalar
}
export type TaskTier = {
  label: string
  flat: number
  rates: Record<string, number>
  conditions: TaskCondition[]
}
export type TaskVisualExpression = {
  tiers: TaskTier[]
  prefix: string
  plain?: true
}

const tokenScale = 1_000_000
const quoted = String.raw`"(?:[^"\\]|\\.)*"`
const use = String.raw`u\(\s*(${quoted})\s*\)`
const literal = String.raw`[\d.eE+-]+`
const tierPattern = new RegExp(
  String.raw`^tier\(\s*(${quoted})\s*,\s*(.+)\)$`,
  's'
)
const ratePattern = new RegExp(
  String.raw`^${use}\s*\*\s*(${literal})(?:\s*/\s*(1000000))?$`
)
const conditionPattern = new RegExp(
  String.raw`^${use}\s*(==|!=|>=|<=|>|<)\s*(${quoted}|true|false|${literal})$`
)

export function emptyTaskTier(label = 'default'): TaskTier {
  return { label, flat: 0, rates: {}, conditions: [] }
}

export function taskConditionFor(
  field: string,
  definition: TaskUsageField
): TaskCondition {
  if ('unit' in definition) return { field, operator: '>=', value: 0 }
  if (definition.type === 'boolean')
    return { field, operator: '==', value: true }
  return { field, operator: '==', value: definition.enum[0] }
}

export function generateTaskVisualExpression(
  config: TaskVisualExpression,
  schema: TaskVisualSchema
): string {
  const branches = config.tiers.map((tier, index) => {
    const parts = tier.flat > 0 ? [String(tier.flat)] : []
    for (const [field, price] of Object.entries(tier.rates)) {
      const definition = schema[field]
      if (!definition || !('unit' in definition)) continue
      const divisor = definition.unit === 'token' ? ` / ${tokenScale}` : ''
      parts.push(`u(${JSON.stringify(field)}) * ${price}${divisor}`)
    }
    const amount = parts.join(' + ') || '0'
    if (config.plain && config.tiers.length === 1 && !tier.label) return amount
    const call = `tier(${JSON.stringify(tier.label)}, ${amount})`
    if (index === config.tiers.length - 1) return call
    const conditions = tier.conditions.map(
      (condition) =>
        `u(${JSON.stringify(condition.field)}) ${condition.operator} ${JSON.stringify(condition.value)}`
    )
    return `${conditions.join(' && ')} ? ${call}`
  })
  return config.prefix + branches.join(' : ')
}

function readCondition(
  text: string,
  schema: TaskVisualSchema
): TaskCondition | null {
  const match = conditionPattern.exec(text)
  if (!match) return null
  const field: string = JSON.parse(match[1])
  if (!Object.hasOwn(schema, field)) return null
  const definition = schema[field]
  const operator = match[2] as TaskCondition['operator']
  const source = match[3]
  if ('unit' in definition) {
    if (!isSupportedNumericLiteral(source) || Number(source) < 0) return null
    return { field, operator, value: Number(source) }
  }
  if (operator !== '==' && operator !== '!=') return null
  const value: unknown = JSON.parse(source)
  if (
    definition.type === 'boolean'
      ? typeof value !== 'boolean'
      : typeof value !== 'string' || !definition.enum.includes(value)
  )
    return null
  return { field, operator, value: value as Scalar }
}

function readTier(text: string, schema: TaskVisualSchema): TaskTier | null {
  const match = tierPattern.exec(text)
  if (!match) return null
  return readAmount(match[2], schema, JSON.parse(match[1]))
}

function readAmount(
  text: string,
  schema: TaskVisualSchema,
  label: string
): TaskTier | null {
  const tier = emptyTaskTier(label)
  for (const [index, term] of splitTopLevelExpression(
    text,
    '+',
    true
  ).entries()) {
    if (index === 0 && isSupportedNumericLiteral(term) && Number(term) >= 0) {
      tier.flat = Number(term)
      continue
    }
    const rate = ratePattern.exec(term)
    if (!rate || !isSupportedNumericLiteral(rate[2]) || Number(rate[2]) < 0)
      return null
    const field: string = JSON.parse(rate[1])
    const definition = Object.hasOwn(schema, field) ? schema[field] : undefined
    if (
      !definition ||
      !('unit' in definition) ||
      (definition.unit === 'token') !== Boolean(rate[3]) ||
      Object.hasOwn(tier.rates, field)
    )
      return null
    tier.rates = { ...tier.rates, [field]: Number(rate[2]) }
  }
  return tier
}

// Compare without changing any quoted data or arithmetic ordering.
const removeSpace = (value: string) =>
  value.replace(/"(?:[^"\\]|\\.)*"|\s+/g, (part) =>
    part.startsWith('"') ? part : ''
  )

export function parseTaskVisualExpression(
  expression: string,
  schema: TaskVisualSchema
): TaskVisualExpression | null {
  const source = expression.trim()
  const prefix = source.startsWith('v1:') ? 'v1:' : ''
  const body = source.slice(prefix.length).trim()
  if (!body) return source ? null : { prefix, tiers: [emptyTaskTier()] }
  try {
    const amount = readAmount(body, schema, '')
    if (amount) {
      const config = { tiers: [amount], prefix, plain: true as const }
      if (
        removeSpace(generateTaskVisualExpression(config, schema)) ===
        removeSpace(source)
      )
        return config
    }
    const branches = splitTopLevelExpression(body, ':', true)
    const tiers: TaskTier[] = []
    for (const [index, branch] of branches.entries()) {
      const parts = splitTopLevelExpression(branch, '?', true)
      const isFallback = index === branches.length - 1
      if (parts.length !== (isFallback ? 1 : 2)) return null
      const tier = readTier(parts[parts.length - 1], schema)
      if (!tier) return null
      if (!isFallback) {
        for (const text of splitTopLevelExpression(parts[0], '&&', true)) {
          const condition = readCondition(text, schema)
          if (!condition) return null
          tier.conditions.push(condition)
        }
      }
      tiers.push(tier)
    }
    const config = { tiers, prefix }
    return removeSpace(generateTaskVisualExpression(config, schema)) ===
      removeSpace(source)
      ? config
      : null
  } catch {
    return null
  }
}
