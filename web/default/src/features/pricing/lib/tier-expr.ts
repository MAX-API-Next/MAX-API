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
import { Parser, type Value } from 'expr-eval'
import { BILLING_CACHE_VAR_MAP } from './billing-expr'
import {
  splitTopLevelExpression,
  unwrapConditionParens,
} from './expression-syntax'

export const CACHE_MODE_TIMED = 'timed'
export const CACHE_MODE_GENERIC = 'generic'
export type CacheMode = typeof CACHE_MODE_TIMED | typeof CACHE_MODE_GENERIC

export type TierConditionInput = {
  var: 'p' | 'c' | 'len' | 'hour' | 'minute' | 'weekday' | 'month' | 'day'
  op: '<' | '<=' | '>' | '>='
  value: number | string
  timezone?: string
}

export type TierConditionGroup = {
  conditions: TierConditionInput[]
}

export const TIME_CONDITION_BOUNDS: Record<
  Extract<
    TierConditionInput['var'],
    'hour' | 'minute' | 'weekday' | 'month' | 'day'
  >,
  { min: number; max: number; defaultValue: number }
> = {
  hour: { min: 0, max: 23, defaultValue: 9 },
  minute: { min: 0, max: 59, defaultValue: 0 },
  weekday: { min: 0, max: 6, defaultValue: 1 },
  month: { min: 1, max: 12, defaultValue: 1 },
  day: { min: 1, max: 31, defaultValue: 1 },
}

export function getTierConditionBounds(variable: TierConditionInput['var']) {
  return TIME_CONDITION_BOUNDS[variable as keyof typeof TIME_CONDITION_BOUNDS]
}

export function normalizeTierCondition(
  condition: TierConditionInput
): TierConditionInput {
  const bounds = getTierConditionBounds(condition.var)
  if (!bounds) return condition
  const numericValue = Number(condition.value)
  const value = Number.isFinite(numericValue)
    ? Math.min(bounds.max, Math.max(bounds.min, numericValue))
    : bounds.defaultValue
  return {
    ...condition,
    value,
    timezone: condition.timezone ?? 'Asia/Shanghai',
  }
}

export type VisualTier = {
  label: string
  conditions: TierConditionInput[]
  conditionGroups?: TierConditionGroup[]
  input_unit_cost: number
  output_unit_cost: number
  cache_mode: CacheMode
  cache_read_unit_cost?: number
  cache_create_unit_cost?: number
  cache_create_1h_unit_cost?: number
  image_unit_cost?: number
  image_output_unit_cost?: number
  audio_input_unit_cost?: number
  audio_output_unit_cost?: number
  [field: string]: unknown
}

export function getTierConditionGroups(
  tier: Pick<VisualTier, 'conditions' | 'conditionGroups'>
): TierConditionGroup[] {
  if (Array.isArray(tier.conditionGroups) && tier.conditionGroups.length > 0) {
    const groups = tier.conditionGroups.filter((group) =>
      Array.isArray(group.conditions)
    )
    if (groups.length > 0) return groups
  }
  return tier.conditions.length > 0 ? [{ conditions: tier.conditions }] : []
}

export type VisualConfig = {
  tiers: VisualTier[]
}

export function getTierCacheMode(
  tier: Partial<VisualTier> | null | undefined
): CacheMode {
  if (tier?.cache_mode === CACHE_MODE_TIMED) return CACHE_MODE_TIMED
  if (tier?.cache_mode === CACHE_MODE_GENERIC) return CACHE_MODE_GENERIC
  return tier?.cache_create_1h_unit_cost != null
    ? CACHE_MODE_TIMED
    : CACHE_MODE_GENERIC
}

export function normalizeVisualTier(
  tier: Partial<VisualTier> = {}
): VisualTier {
  return {
    label: tier.label ?? '',
    input_unit_cost: Number(tier.input_unit_cost) || 0,
    output_unit_cost: Number(tier.output_unit_cost) || 0,
    cache_mode: getTierCacheMode(tier),
    ...tier,
    conditions: Array.isArray(tier.conditions)
      ? tier.conditions.map((condition) => normalizeTierCondition(condition))
      : [],
    ...(Array.isArray(tier.conditionGroups)
      ? {
          conditionGroups: tier.conditionGroups
            .filter((group) => Array.isArray(group.conditions))
            .map((group) => ({
              conditions: group.conditions.map((condition) =>
                normalizeTierCondition(condition)
              ),
            })),
        }
      : {}),
    // Presence is part of the billing contract: an omitted subcategory stays
    // in p/c, while an explicit zero excludes it and prices it for free.
    ...Object.fromEntries(
      BILLING_CACHE_VAR_MAP.map(({ field }) => [
        field,
        tier[field] == null || tier[field] === ''
          ? undefined
          : Number(tier[field]),
      ])
    ),
  }
}

export function createDefaultVisualConfig(): VisualConfig {
  return {
    tiers: [
      normalizeVisualTier({
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
        label: 'base',
        cache_mode: CACHE_MODE_GENERIC,
      }),
    ],
  }
}

export function normalizeVisualConfig(
  config: VisualConfig | null | undefined
): VisualConfig {
  if (!config || !Array.isArray(config.tiers) || config.tiers.length === 0) {
    return createDefaultVisualConfig()
  }
  return {
    ...config,
    tiers: config.tiers.map((tier) => normalizeVisualTier(tier)),
  }
}

function buildConditionStr(conditions: TierConditionInput[]): string {
  if (!conditions || conditions.length === 0) return ''
  return conditions
    .filter((c) => c.var && c.op && c.value != null && c.value !== '')
    .map((c) => {
      const lhs = ['hour', 'minute', 'weekday', 'month', 'day'].includes(c.var)
        ? `${c.var}("${String(c.timezone || 'Asia/Shanghai').replace(/"/g, '\\"')}")`
        : c.var
      return `${lhs} ${c.op} ${c.value}`
    })
    .join(' && ')
}

function parseTierConditionGroups(conditionStr: string): TierConditionGroup[] {
  if (!conditionStr || conditionStr.trim() === 'true') return []
  const atomPattern =
    /^(?:(p|c|len)|((?:hour|minute|weekday|month|day))\("([^"\\]+)"\))\s*(<=|>=|<|>)\s*([\d.eE+-]+)$/
  return splitTopLevelExpression(conditionStr, '||')
    .map((groupStr) => unwrapConditionParens(groupStr))
    .map((groupStr) => {
      const conditions: TierConditionInput[] = []
      for (const atom of splitTopLevelExpression(groupStr, '&&')) {
        const match = atom.trim().match(atomPattern)
        if (!match) continue
        conditions.push({
          var: (match[1] || match[2]) as TierConditionInput['var'],
          op: match[4] as TierConditionInput['op'],
          value: Number(match[5]),
          ...(match[2] ? { timezone: match[3] } : {}),
        })
      }
      return { conditions }
    })
    .filter((group) => group.conditions.length > 0)
}

function buildConditionExpr(tier: VisualTier): string {
  const groups = getTierConditionGroups(tier)
    .map((group) => buildConditionStr(group.conditions))
    .filter(Boolean)
  if (groups.length === 0) return ''
  if (groups.length === 1) return groups[0]
  return groups.map((group) => `(${group})`).join(' || ')
}

function buildTierBodyExpr(tier: VisualTier): string {
  const parts: string[] = []
  const ic = Number(tier.input_unit_cost) || 0
  const oc = Number(tier.output_unit_cost) || 0
  parts.push(`p * ${ic}`)
  parts.push(`c * ${oc}`)
  for (const cv of BILLING_CACHE_VAR_MAP) {
    const value = tier[cv.field]
    if (value != null && value !== '') {
      parts.push(`${cv.exprVar} * ${Number(value)}`)
    }
  }
  return parts.join(' + ')
}

export function generateExprFromVisualConfig(
  config: VisualConfig | null | undefined
): string {
  if (!config || !config.tiers || config.tiers.length === 0) {
    return 'p * 0 + c * 0'
  }
  const tiers = config.tiers

  if (tiers.length === 1) {
    const tier = tiers[0]
    const label = tier.label || 'default'
    const body = `tier("${label}", ${buildTierBodyExpr(tier)})`
    const cond = buildConditionExpr(tier)
    if (cond) {
      return `${cond} ? ${body} : tier("${label}_fallback", ${buildTierBodyExpr(tier)})`
    }
    return body
  }

  const parts: string[] = []
  for (let i = 0; i < tiers.length; i++) {
    const tier = tiers[i]
    const label = tier.label || `tier_${i + 1}`
    const body = `tier("${label}", ${buildTierBodyExpr(tier)})`
    const cond = buildConditionExpr(tier)

    if (cond) {
      parts.push(`${cond} ? ${body}`)
    } else {
      // An unconditional non-final tier is an intentional always-match
      // branch, such as after removing its last condition.
      parts.push(i < tiers.length - 1 ? `true ? ${body}` : body)
    }
  }
  const last = tiers[tiers.length - 1]
  if (buildConditionExpr(last)) {
    parts.push(
      `tier("${last.label || 'fallback'}_fallback", ${buildTierBodyExpr(last)})`
    )
  }
  return parts.join(' : ')
}

// Compare the visual subset without changing quoted labels or accepting
// integer rounding/underflow as a harmless spelling difference.
function canonicalizeExprForComparison(source: string): string {
  let result = ''
  let quote = ''
  let escaped = false
  let index = 0

  while (index < source.length) {
    const char = source[index]
    if (quote) {
      result += char
      if (escaped) escaped = false
      else if (char === '\\') escaped = true
      else if (char === quote) quote = ''
      index += 1
      continue
    }
    if (char === '"' || char === "'") {
      quote = char
      result += char
      index += 1
      continue
    }
    if (/\s/.test(char)) {
      index += 1
      continue
    }

    const previous = source[index - 1]
    const isNumberStart =
      /[0-9.]/.test(char) &&
      !/[A-Za-z0-9_.]/.test(previous || '') &&
      (/[0-9]/.test(char) || /[0-9]/.test(source[index + 1] || ''))
    if (isNumberStart) {
      const match = source
        .slice(index)
        .match(/^(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?/)
      if (match) {
        const numeric = Number(match[0])
        const safeNumber =
          Number.isFinite(numeric) &&
          (!Number.isInteger(numeric) || Number.isSafeInteger(numeric)) &&
          (numeric !== 0 || !/[1-9]/.test(match[0].split(/[eE]/)[0]))
        result += safeNumber ? String(numeric) : match[0]
        index += match[0].length
        continue
      }
    }
    result += char
    index += 1
  }
  return result
}

export function tryParseVisualConfig(
  exprStr: string | null | undefined
): VisualConfig | null {
  if (!exprStr) return null
  try {
    let body = exprStr
    const versionMatch = body.match(/^v\d+:([\s\S]*)$/)
    if (versionMatch) body = versionMatch[1]
    const cacheVarNames = BILLING_CACHE_VAR_MAP.map((cv) => cv.exprVar)
    const optCacheStr = cacheVarNames
      .map((v) => `(?:\\s*\\+\\s*${v}\\s*\\*\\s*([\\d.eE+-]+))?`)
      .join('')

    const bodyPat = `p\\s*\\*\\s*([\\d.eE+-]+)\\s*\\+\\s*c\\s*\\*\\s*([\\d.eE+-]+)${optCacheStr}`

    const singleRe = new RegExp(`^tier\\("([^"]*)",\\s*${bodyPat}\\)$`)
    const simple = body.match(singleRe)
    if (simple) {
      const tier: Record<string, unknown> = {
        conditions: [],
        input_unit_cost: Number(simple[2]),
        output_unit_cost: Number(simple[3]),
        label: simple[1],
      }
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = simple[4 + i]
        if (val != null) tier[cv.field] = Number(val)
      })
      const config = normalizeVisualConfig({
        tiers: [normalizeVisualTier(tier as Partial<VisualTier>)],
      })
      // Like the multi-tier parser, refuse a lossy visual conversion.
      return canonicalizeExprForComparison(
        generateExprFromVisualConfig(config)
      ) === canonicalizeExprForComparison(body)
        ? config
        : null
    }

    const tierRe = new RegExp(`^tier\\("([^"]*)",\\s*${bodyPat}\\)$`)
    const tiers: VisualTier[] = []
    for (const branch of splitTopLevelExpression(body, ':')) {
      const parts = branch.match(/^(.*?)\s*\?\s*(tier\("[^"]*",[\s\S]+\))$/)
      const condStr = parts?.[1]?.trim() || ''
      const tierSource = parts?.[2] || branch
      const match = tierRe.exec(tierSource)
      if (!match) continue
      const conditionGroups = parseTierConditionGroups(condStr)
      const conditions = conditionGroups[0]?.conditions ?? []
      const tier: Record<string, unknown> = {
        conditions,
        ...(conditionGroups.length > 1 ? { conditionGroups } : {}),
        input_unit_cost: Number(match[2]),
        output_unit_cost: Number(match[3]),
        label: match[1],
      }
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = match[4 + i]
        if (val != null) tier[cv.field] = Number(val)
      })
      tiers.push(normalizeVisualTier(tier as Partial<VisualTier>))
    }
    if (tiers.length === 0) return null

    const cfg = normalizeVisualConfig({ tiers })
    const regenerated = generateExprFromVisualConfig(cfg)
    if (
      canonicalizeExprForComparison(regenerated) !==
      canonicalizeExprForComparison(body)
    ) {
      return null
    }
    return cfg
  } catch {
    return null
  }
}

// ---------------------------------------------------------------------------
// Local cost evaluator (for the estimator preview)
// ---------------------------------------------------------------------------

const ESTIMATOR_VARS = [
  { var: 'cr', stateKey: 'cacheReadTokens' },
  { var: 'cc', stateKey: 'cacheCreateTokens' },
  { var: 'cc1h', stateKey: 'cacheCreate1hTokens' },
  { var: 'img', stateKey: 'imageTokens' },
  { var: 'img_o', stateKey: 'imageOutputTokens' },
  { var: 'ai', stateKey: 'audioInputTokens' },
  { var: 'ao', stateKey: 'audioOutputTokens' },
] as const

export type ExtraTokenValues = Record<
  (typeof ESTIMATOR_VARS)[number]['stateKey'],
  number
>

export type EvalResult = {
  cost: number
  matchedTier: string
  error: string | null
}

const estimatorParser = new Parser({
  allowMemberAccess: false,
  operators: {
    assignment: false,
    fndef: false,
  },
})

type EstimatorTimeParts = {
  hour: number
  minute: number
  weekday: number
  month: number
  day: number
}

type EstimatorTimeFunctions = {
  hour: (timezone: string) => number
  minute: (timezone: string) => number
  weekday: (timezone: string) => number
  month: (timezone: string) => number
  day: (timezone: string) => number
}

const ESTIMATOR_WEEKDAYS: Record<string, number> = {
  Sun: 0,
  Mon: 1,
  Tue: 2,
  Wed: 3,
  Thu: 4,
  Fri: 5,
  Sat: 6,
}

function createEstimatorTimeFunctions(): EstimatorTimeFunctions {
  const now = new Date()
  const timePartsCache = new Map<string, EstimatorTimeParts>()

  const getUtcTimeParts = (): EstimatorTimeParts => ({
    hour: now.getUTCHours(),
    minute: now.getUTCMinutes(),
    weekday: now.getUTCDay(),
    month: now.getUTCMonth() + 1,
    day: now.getUTCDate(),
  })

  const getTimeParts = (timezone: string): EstimatorTimeParts => {
    const normalizedTimezone = timezone.trim() || 'UTC'
    const cached = timePartsCache.get(normalizedTimezone)
    if (cached) return cached

    try {
      const parts = new Intl.DateTimeFormat('en-US-u-ca-gregory-nu-latn', {
        timeZone: normalizedTimezone,
        hourCycle: 'h23',
        hour: '2-digit',
        minute: '2-digit',
        weekday: 'short',
        month: 'numeric',
        day: 'numeric',
      }).formatToParts(now)
      const partValues = Object.fromEntries(
        parts.map((part) => [part.type, part.value])
      )
      const result = {
        hour: Number(partValues.hour),
        minute: Number(partValues.minute),
        weekday: ESTIMATOR_WEEKDAYS[partValues.weekday] ?? 0,
        month: Number(partValues.month),
        day: Number(partValues.day),
      }
      timePartsCache.set(normalizedTimezone, result)
      return result
    } catch {
      const fallback = timePartsCache.get('UTC') ?? getUtcTimeParts()
      timePartsCache.set('UTC', fallback)
      timePartsCache.set(normalizedTimezone, fallback)
      return fallback
    }
  }

  return {
    hour: (timezone: string) => getTimeParts(timezone).hour,
    minute: (timezone: string) => getTimeParts(timezone).minute,
    weekday: (timezone: string) => getTimeParts(timezone).weekday,
    month: (timezone: string) => getTimeParts(timezone).month,
    day: (timezone: string) => getTimeParts(timezone).day,
  }
}

function normalizeEstimatorExpression(exprStr: string): string {
  const body = exprStr.trim().replace(/^v\d+:/, '')
  let quote = ''
  let escaped = false
  let normalized = ''

  for (let index = 0; index < body.length; index += 1) {
    const char = body[index]
    if (quote) {
      normalized += char
      if (escaped) {
        escaped = false
      } else if (char === '\\') {
        escaped = true
      } else if (char === quote) {
        quote = ''
      }
      continue
    }
    if (char === '"' || char === "'") {
      quote = char
      normalized += char
      continue
    }
    if (char === '&' && body[index + 1] === '&') {
      normalized += ' and '
      index += 1
      continue
    }
    if (char === '|' && body[index + 1] === '|') {
      normalized += ' or '
      index += 1
      continue
    }
    if (char === '!' && body[index + 1] !== '=') {
      normalized += ' not '
      continue
    }
    normalized += char
  }
  return normalized
}

export function evalExprLocally(
  exprStr: string,
  promptTokens: number,
  completionTokens: number,
  extraTokenValues: ExtraTokenValues
): EvalResult {
  try {
    if (!exprStr || !exprStr.trim()) {
      return { cost: 0, matchedTier: '', error: null }
    }
    let matchedTier = ''
    const tierFn = (name: string, value: number) => {
      matchedTier = name
      return value
    }
    const cacheReadTokens = extraTokenValues.cacheReadTokens || 0
    const cacheCreateTokens = extraTokenValues.cacheCreateTokens || 0
    const cacheCreate1hTokens = extraTokenValues.cacheCreate1hTokens || 0
    const len =
      promptTokens + cacheReadTokens + cacheCreateTokens + cacheCreate1hTokens
    const timeFunctions = createEstimatorTimeFunctions()
    const env: Record<string, unknown> = {
      p: promptTokens,
      c: completionTokens,
      len,
      tier: tierFn,
      max: Math.max,
      min: Math.min,
      abs: Math.abs,
      ceil: Math.ceil,
      floor: Math.floor,
      ...timeFunctions,
    }
    for (const field of ESTIMATOR_VARS) {
      env[field.var] = extraTokenValues[field.stateKey] || 0
    }
    const expression = estimatorParser.parse(
      normalizeEstimatorExpression(exprStr)
    )
    const rawCost: unknown = expression.evaluate(env as unknown as Value)
    const cost = Number(rawCost) || 0
    return { cost, matchedTier, error: null }
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    return { cost: 0, matchedTier: '', error: message }
  }
}

export function exprUsesExtraVars(exprStr: string): boolean {
  if (!exprStr) return false
  const varNames = ESTIMATOR_VARS.map((f) => f.var).join('|')
  return new RegExp(`\\b(${varNames})\\b`).test(exprStr)
}

export const ESTIMATOR_EXTRA_FIELDS = ESTIMATOR_VARS
