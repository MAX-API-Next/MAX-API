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
import { BILLING_CACHE_VAR_MAP, parseTiersFromExpr } from './billing-expr'
import {
  evalExprLocally,
  generateExprFromVisualConfig,
  normalizeVisualTier,
  createDefaultVisualConfig,
  tryParseVisualConfig,
  type ExtraTokenValues,
} from './tier-expr'

const emptyExtraTokens: ExtraTokenValues = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

describe('visual optional price presence', () => {
  for (const { field, exprVar } of BILLING_CACHE_VAR_MAP) {
    test(`preserves absent, zero and positive ${exprVar} prices through normalization and round trips`, () => {
      const absent = normalizeVisualTier({
        input_unit_cost: 3,
        output_unit_cost: 15,
      })
      assert.equal(absent[field], undefined)
      assert.doesNotMatch(
        generateExprFromVisualConfig({ tiers: [absent] }),
        new RegExp(`\\b${exprVar} \\*`)
      )
      for (const price of [0, 0.25]) {
        const source = `tier("base", p * 3 + c * 15 + ${exprVar} * ${price})`
        const parsed = tryParseVisualConfig(source)
        assert.ok(parsed)
        assert.equal(parsed.tiers[0][field], price)
        assert.equal(generateExprFromVisualConfig(parsed), source)
        assert.deepEqual(
          tryParseVisualConfig(generateExprFromVisualConfig(parsed)),
          parsed
        )
        const multi = `len < 100 ? ${source} : tier("other", p * 6 + c * 30)`
        const multiParsed = tryParseVisualConfig(multi)
        assert.ok(multiParsed)
        assert.equal(multiParsed.tiers[1][field], undefined)
        assert.equal(generateExprFromVisualConfig(multiParsed), multi)
      }
    })
  }

  test('does not create zero-priced subcategories in default tiers', () => {
    assert.equal(
      generateExprFromVisualConfig(createDefaultVisualConfig()),
      'tier("base", p * 0 + c * 0)'
    )
  })

  test('accepts equivalent numeric spellings when switching to visual mode', () => {
    for (const source of [
      'tier("base", p * 3.0 + c * 15.0)',
      'tier("base", p * 3e0 + c * 1.5e1)',
    ]) {
      const parsed = tryParseVisualConfig(source)
      assert.ok(parsed)
      assert.equal(parsed.tiers[0].input_unit_cost, 3)
      assert.equal(parsed.tiers[0].output_unit_cost, 15)
    }
  })

  test('preserves numeric spellings and zero presence in every optional category', () => {
    for (const { field, exprVar } of BILLING_CACHE_VAR_MAP) {
      for (const zero of ['0.0', '0e0']) {
        const source = `tier("base 3.0", p * 3.0 + c * 1.5e1 + ${exprVar} * ${zero})`
        for (const expression of [
          source,
          `len < 1e2 ? ${source} : tier("other", p * .5 + c * 2.0)`,
        ]) {
          const parsed = tryParseVisualConfig(expression)
          assert.ok(parsed, expression)
          assert.equal(parsed.tiers[0].label, 'base 3.0')
          assert.equal(parsed.tiers[0][field], 0)
          assert.equal(parsed.tiers[0].input_unit_cost, 3)
          assert.equal(parsed.tiers[0].output_unit_cost, 15)
          assert.deepEqual(
            tryParseVisualConfig(generateExprFromVisualConfig(parsed)),
            parsed
          )
        }
      }
    }
  })

  test('still rejects lossy or malformed visual conversions', () => {
    for (const source of [
      'tier("base", p * 1+2 + c * 15)',
      'tier("base", p * 1e999 + c * 15)',
      'tier("base", p * 1e-999 + c * 15)',
      'len < 9007199254740993 ? tier("base", p * 3 + c * 15) : tier("other", p * 6 + c * 30)',
      'tier("base", p * 3 + c * 15 + max(cr, 1))',
      'hour("Asia/Shanghai") > 99 ? tier("base", p * 3 + c * 15) : tier("other", p * 6 + c * 30)',
      'len < 100 && c == 5 ? tier("base", p * 3 + c * 15) : tier("other", p * 6 + c * 30)',
      ': tier("base", p * 3 + c * 15)',
      'tier("base", p * 3 + c * 15) :',
      'len < 100 ? tier("base", p * 3 + c * 15) : : tier("other", p * 6 + c * 30)',
    ])
      assert.equal(tryParseVisualConfig(source), null, source)
  })

  test('keeps every zero-priced subcategory referenced in the billing contract', () => {
    const source = `tier("base", p * 3 + c * 15${BILLING_CACHE_VAR_MAP.map(({ exprVar }) => ` + ${exprVar} * 0`).join('')})`
    const parsed = tryParseVisualConfig(source)
    assert.ok(parsed)
    assert.equal(generateExprFromVisualConfig(parsed), source)
  })
})

describe('evalExprLocally', () => {
  test('evaluates backend-style ternaries and logical operators', () => {
    const result = evalExprLocally(
      'v1:p <= 100 && c > 0 ? tier("small", p * 2 + c * 3) : tier("large", p * 4)',
      100,
      10,
      emptyExtraTokens
    )

    assert.deepEqual(result, { cost: 230, matchedTier: 'small', error: null })
  })

  test('evaluates time-based pricing expressions with backend time functions', () => {
    const result = evalExprLocally(
      'hour("Asia/Shanghai") < 9 || (hour("Asia/Shanghai") >= 12 && hour("Asia/Shanghai") < 14) || hour("Asia/Shanghai") >= 18 ? tier("平常时段 0-9/12-14/18-24", p * 4.5 + c * 13.5 + cr * 0.15) : tier("高峰期 9-12/14-18", p * 9 + c * 27 + cr * 0.3)',
      100,
      10,
      emptyExtraTokens
    )

    assert.equal(result.error, null)
    assert.ok(
      [
        {
          cost: 585,
          matchedTier: '平常时段 0-9/12-14/18-24',
        },
        {
          cost: 1170,
          matchedTier: '高峰期 9-12/14-18',
        },
      ].some(
        (expected) =>
          expected.cost === result.cost &&
          expected.matchedTier === result.matchedTier
      )
    )
  })

  test('keeps time helpers in backend ranges and falls back to UTC', () => {
    const utcFallbackChecks = ['hour', 'minute', 'weekday', 'month', 'day']
      .flatMap((helper) => [
        `${helper}("Invalid/Zone") == ${helper}("UTC")`,
        `${helper}("") == ${helper}("UTC")`,
      ])
      .join(' && ')
    const utcRangeChecks = [
      'hour("UTC") >= 0 && hour("UTC") <= 23',
      'minute("UTC") >= 0 && minute("UTC") <= 59',
      'weekday("UTC") >= 0 && weekday("UTC") <= 6',
      'month("UTC") >= 1 && month("UTC") <= 12',
      'day("UTC") >= 1 && day("UTC") <= 31',
    ].join(' && ')
    const result = evalExprLocally(
      `${utcFallbackChecks} && ${utcRangeChecks} ? tier("valid", p) : tier("invalid", 999)`,
      42,
      0,
      emptyExtraTokens
    )

    assert.deepEqual(result, { cost: 42, matchedTier: 'valid', error: null })
  })

  test('does not expose browser globals or member access', () => {
    const result = evalExprLocally(
      'globalThis.document ? 999 : 1',
      100,
      10,
      emptyExtraTokens
    )

    assert.equal(result.cost, 0)
    assert.equal(result.matchedTier, '')
    assert.ok(result.error)
  })
})

describe('visual tier conditions', () => {
  test('rejects unsupported or unsafe complete tier bodies', () => {
    for (const body of [
      'max(p, 1) * 3 + c * 15',
      'p * 2 * 3 + c * 4',
      'p * 2 - c * 4',
      'p * 2 + unknown * 3',
      'xp * 2 + c * 4',
      'p * 2 + p * 3',
      'p * 1e999 + c * 4',
      'p * 1e-999 + c * 4',
      'p * 9007199254740993 + c * 4',
      'p * 2 +',
      'p * 2 + 10',
      '',
    ]) {
      assert.deepEqual(parseTiersFromExpr(`tier("custom", ${body})`), [], body)
      assert.deepEqual(
        parseTiersFromExpr(
          `len < 100 ? tier("known", p * 1 + c * 2) : tier("custom", ${body})`
        ),
        [],
        body
      )
    }
  })

  test('retains coefficient presence, zero, and supported exponent notation', () => {
    const [tier] = parseTiersFromExpr(
      'tier("free", c * 1e+2 + p * .5 + cr * 0 + cc1h * 0e-99)'
    )
    assert.equal(tier.inputPrice, 0.5)
    assert.equal(tier.outputPrice, 100)
    assert.equal(tier.cacheReadPrice, 0)
    assert.equal(tier.cacheCreate1hPrice, 0)
    assert.equal(Object.hasOwn(tier, 'cacheCreatePrice'), false)
    for (const { exprVar } of BILLING_CACHE_VAR_MAP) {
      const absent = parseTiersFromExpr('tier("base", p * 2 + c * 4)')[0]
      const present = parseTiersFromExpr(
        `tier("base", p * 2 + c * 4 + ${exprVar} * 0)`
      )[0]
      assert.equal(
        Object.keys(present).length,
        Object.keys(absent).length + 1,
        exprVar
      )
    }
  })

  test('rejects structured displays with unsupported or missing branches', () => {
    for (const expression of [
      'len < 100 ? tier("peak", p * 2 + c * 4) : max(p, 1)',
      'len < 100 ? max(p, 1) : tier("off", p * 1 + c * 2)',
      'len < 100 ? tier("peak", p * 2 + c * 4) : : tier("off", p * 1 + c * 2)',
      ': tier("off", p * 1 + c * 2)',
      'tier("off", p * 1 + c * 2) :',
    ]) {
      assert.deepEqual(parseTiersFromExpr(expression), [], expression)
    }
  })

  test('rejects unsafe integer and underflowed condition literals', () => {
    for (const literal of [
      '9007199254740993',
      '-9007199254740993',
      '1e-999',
      '-1e-999',
    ]) {
      const expression = `len < ${literal} ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)`
      assert.deepEqual(parseTiersFromExpr(expression), [], literal)
      assert.equal(tryParseVisualConfig(expression), null, literal)
    }
    for (const literal of [
      '0',
      '-0',
      '0e-999',
      '1e3',
      '1.25',
      '9007199254740991',
    ]) {
      const expression = `len < ${literal} ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)`
      assert.equal(
        parseTiersFromExpr(expression)[0]?.conditions[0]?.value,
        Number(literal)
      )
    }
  })

  test('rejects incomplete structured displays for unsupported conditions', () => {
    for (const condition of [
      'len < 100 && c == 5',
      '(len < 100) || (c == 5)',
      'len < 100 && (c > 1 || c < 5)',
      'param("service_tier") == "priority" && len < 100',
      '(len < 100) || (true) || (c == 5)',
      'len < 1e999',
      'len < 1+2',
      'len < 100 &&',
      '|| len < 100',
      '(len < 100) || ()',
    ]) {
      assert.deepEqual(
        parseTiersFromExpr(
          `${condition} ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)`
        ),
        [],
        condition
      )
    }
  })

  test('preserves wrapped true AND members in supported display conditions', () => {
    const tiers = parseTiersFromExpr(
      '(true) && len < 100 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    )
    assert.deepEqual(tiers[0].conditions, [{ var: 'len', op: '<', value: 100 }])
  })

  for (const condition of [
    'true || len < 100',
    '(len < 100) || (len > 200) || ((true))',
    '((len < 100) || (true && (true)))',
  ]) {
    test(`displays an unconditional OR tier without restrictions: ${condition}`, () => {
      const source = `${condition} ? tier("always", p * 2 + c * 4) : tier("off", p * 1 + c * 2)`
      const display = parseTiersFromExpr(source)
      assert.equal(display.length, 2)
      assert.deepEqual(display[0].conditions, [])
      assert.equal(display[0].conditionGroups, undefined)
      // Display simplification must not enable a lossy visual rewrite.
      assert.equal(tryParseVisualConfig(source), null)
      assert.deepEqual(evalExprLocally(source, 150, 2, emptyExtraTokens), {
        cost: 308,
        matchedTier: 'always',
        error: null,
      })
    })
  }

  test('retains restrictions when true is an AND member rather than an OR branch', () => {
    const tiers = parseTiersFromExpr(
      '(true && len < 100) || (len > 200) ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    )
    assert.deepEqual(tiers[0].conditionGroups, [
      [{ var: 'len', op: '<', value: 100 }],
      [{ var: 'len', op: '>', value: 200 }],
    ])
  })

  for (const condition of [
    '((len < 100) || (len > 200))',
    '((((len < 100) || (len > 200))))',
    '((len < 100 && hour("Test/)||(:offset") >= 9) || (len > 200))',
  ]) {
    test(`preserves fully wrapped OR conditions: ${condition}`, () => {
      const source = `${condition} ? tier("peak", p * 2 + c * 4 + cr * 0) : tier("off", p * 1 + c * 2)`
      const display = parseTiersFromExpr(source)
      assert.equal(display[0]?.conditionGroups?.length, 2)
      assert.equal(display[0].conditionGroups![0][0].value, 100)
      assert.equal(display[0].conditionGroups![1][0].value, 200)
      const visual = tryParseVisualConfig(source)
      assert.ok(visual)
      assert.deepEqual(
        visual.tiers[0].conditionGroups?.map((group) => group.conditions),
        display[0].conditionGroups
      )
      assert.equal(visual.tiers[0].cache_read_unit_cost, 0)
      assert.equal(visual.tiers[1].cache_read_unit_cost, undefined)
      const regenerated = generateExprFromVisualConfig(visual)
      assert.deepEqual(tryParseVisualConfig(regenerated), visual)
      assert.deepEqual(parseTiersFromExpr(regenerated), display)
      for (const len of [0, 99, 100, 150, 200, 201]) {
        assert.deepEqual(
          evalExprLocally(regenerated, len, 2, emptyExtraTokens),
          evalExprLocally(source, len, 2, emptyExtraTokens)
        )
      }
    })
  }

  test('keeps rejecting unsupported or lossy conditions inside outer wrappers', () => {
    for (const condition of [
      '((len < 100 || len > 200) && c > 1)',
      '((len < 100) || (c == 5))',
      '((hour("UTC") > 99) || (len > 200))',
      '((len < 9007199254740993) || (len > 200))',
    ]) {
      assert.equal(
        tryParseVisualConfig(
          `${condition} ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)`
        ),
        null,
        condition
      )
    }
  })

  for (const timezone of ['Test/)||(', 'Test/)&&(', 'Test/:offset']) {
    test(`preserves quoted syntax in both pricing parsers: ${timezone}`, () => {
      const config = {
        tiers: [
          normalizeVisualTier({
            label: 'peak:night',
            conditions: [],
            conditionGroups: [
              { conditions: [{ var: 'hour', timezone, op: '>=', value: 9 }] },
              { conditions: [{ var: 'len', op: '<', value: 100 }] },
            ],
            input_unit_cost: 2,
            output_unit_cost: 4,
          }),
          normalizeVisualTier({
            label: 'off',
            input_unit_cost: 1,
            output_unit_cost: 2,
          }),
        ],
      }
      const expression = generateExprFromVisualConfig(config)
      const visual = tryParseVisualConfig(expression)
      const display = parseTiersFromExpr(expression)
      assert.equal(
        visual?.tiers[0].conditionGroups?.[0].conditions[0].timezone,
        timezone
      )
      assert.deepEqual(
        display[0]?.conditionGroups,
        config.tiers[0].conditionGroups?.map((group) => group.conditions)
      )
      assert.equal(display[0]?.label, 'peak:night')
      assert.equal(display.length, 2)
    })
  }

  test('supports time conditions without requiring a second tier', () => {
    const expr = generateExprFromVisualConfig({
      tiers: [
        {
          label: 'business-hours',
          conditions: [
            { var: 'hour', timezone: 'Asia/Shanghai', op: '>=', value: 9 },
          ],
          input_unit_cost: 1,
          output_unit_cost: 2,
          cache_mode: 'generic',
        },
      ],
    })
    assert.match(expr, /hour\("Asia\/Shanghai"\) >= 9/)
    assert.match(expr, /: tier\("business-hours_fallback", p \* 1 \+ c \* 2\)$/)
    const parsed = tryParseVisualConfig(expr)
    assert.equal(parsed?.tiers[0].conditions[0].var, 'hour')
    assert.equal(parsed?.tiers[0].conditions[0].timezone, 'Asia/Shanghai')
  })

  test('keeps time conditions visible in pricing breakdown parsing', () => {
    const tiers = parseTiersFromExpr(
      'hour("Asia/Shanghai") >= 9 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    )
    assert.equal(tiers[0]?.conditions[0]?.var, 'hour')
    assert.equal(tiers[0]?.conditions[0]?.timezone, 'Asia/Shanghai')
  })

  test('parses OR condition groups for pricing breakdowns', () => {
    const tiers = parseTiersFromExpr(
      '(len < 200000 && hour("Asia/Shanghai") >= 9) || (weekday("Asia/Shanghai") <= 5 && month("Asia/Shanghai") >= 1) ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    )
    assert.equal(tiers[0]?.conditions.length, 4)
    assert.equal(tiers[0]?.conditions[1]?.var, 'hour')
    assert.equal(tiers[0]?.conditions[3]?.var, 'month')
  })

  test('round-trips an explicit unconditional non-final tier', () => {
    const expr =
      'true ? tier("primary", p * 2 + c * 4) : tier("fallback", p * 1 + c * 2)'
    const parsed = tryParseVisualConfig(expr)
    assert.ok(parsed)
    assert.equal(parsed.tiers.length, 2)
    assert.equal(parsed.tiers[0].conditions.length, 0)
    assert.equal(generateExprFromVisualConfig(parsed), expr)
    assert.deepEqual(evalExprLocally(expr, 10, 2, emptyExtraTokens), {
      cost: 28,
      matchedTier: 'primary',
      error: null,
    })
  })

  test('normalizes time condition values to their calendar ranges', () => {
    const tier = normalizeVisualTier({
      conditions: [
        { var: 'month', op: '<=', value: 200000 },
        { var: 'weekday', op: '>=', value: -3 },
      ],
    })
    assert.equal(tier.conditions[0].value, 12)
    assert.equal(tier.conditions[1].value, 0)
  })

  test('generates more than two conditions without truncation', () => {
    const expr = generateExprFromVisualConfig({
      tiers: [
        {
          label: 'multi',
          conditions: [
            { var: 'len', op: '<', value: 200000 },
            { var: 'hour', op: '>=', value: 9 },
            { var: 'weekday', op: '<=', value: 5 },
          ],
          input_unit_cost: 1,
          output_unit_cost: 2,
          cache_mode: 'generic',
        },
      ],
    })
    assert.match(
      expr,
      /len < 200000 && hour\("Asia\/Shanghai"\) >= 9 && weekday\("Asia\/Shanghai"\) <= 5/
    )
  })

  test('round-trips OR condition groups with AND members', () => {
    const expr = generateExprFromVisualConfig({
      tiers: [
        {
          label: 'multi-group',
          conditions: [],
          conditionGroups: [
            {
              conditions: [
                { var: 'len', op: '<', value: 200000 },
                {
                  var: 'hour',
                  op: '>=',
                  value: 9,
                  timezone: 'Asia/Shanghai',
                },
              ],
            },
            {
              conditions: [
                {
                  var: 'weekday',
                  op: '<=',
                  value: 5,
                  timezone: 'Asia/Shanghai',
                },
                {
                  var: 'month',
                  op: '>=',
                  value: 1,
                  timezone: 'Asia/Shanghai',
                },
              ],
            },
          ],
          input_unit_cost: 1,
          output_unit_cost: 2,
          cache_mode: 'generic',
        },
      ],
    })
    assert.match(expr, /\) \|\| \(/)
    const parsed = tryParseVisualConfig(expr)
    assert.equal(parsed?.tiers[0].conditionGroups?.length, 2)
    assert.equal(parsed?.tiers[0].conditionGroups?.[0].conditions.length, 2)
    assert.equal(parsed?.tiers[0].conditionGroups?.[1].conditions.length, 2)
    assert.equal(generateExprFromVisualConfig(parsed!), expr)
  })

  test('resets invalid time values when normalizing a condition', () => {
    const tier = normalizeVisualTier({
      conditions: [{ var: 'month', op: '<=', value: 200000 }],
    })
    assert.equal(tier.conditions[0].value, 12)
  })
})
