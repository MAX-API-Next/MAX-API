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
import ru from '@/i18n/locales/ru.json'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getTieredBillingSummary, renderAuditContent } from './format'

describe('tiered billing price summaries', () => {
  test('preserves configured zero prices and the cache-usage visibility policy', () => {
    const expr = 'tier("free", p * 0 + c * 2 + cr * 0 + img * 0)'
    const other = {
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(expr).toString('base64'),
      matched_tier: 'free',
    }
    const withoutCache = getTieredBillingSummary(other)
    assert.deepEqual(
      withoutCache?.priceEntries.map(({ field, price }) => [field, price]),
      [
        ['inputPrice', 0],
        ['outputPrice', 2],
        ['imagePrice', 0],
      ]
    )
    const withCache = getTieredBillingSummary({ ...other, cache_tokens: 10 })
    assert.deepEqual(
      withCache?.priceEntries.map(({ field, price }) => [field, price]),
      [
        ['inputPrice', 0],
        ['outputPrice', 2],
        ['cacheReadPrice', 0],
        ['imagePrice', 0],
      ]
    )
  })
})

describe('renderAuditContent', () => {
  test('localizes completed manual task billing settlements', () => {
    const rendered = renderAuditContent(
      {
        op: {
          action: 'billing.manual_task_settlement_complete',
          params: { settlement_id: 42, actual_quota: 1250000 },
        },
      },
      (key: string, opts?: Record<string, unknown>): string =>
        key
          .replace('{{settlement_id}}', String(opts?.settlement_id))
          .replace('{{actual_quota}}', String(opts?.actual_quota))
    )

    assert.equal(
      rendered,
      'Completed manual task billing settlement 42 with exact quota 1250000'
    )
  })

  test('labels the Russian audit value as a settlement id', () => {
    const key =
      'Completed manual task billing settlement {{settlement_id}} with exact quota {{actual_quota}}'
    const translated = ru.translation[key]

    assert.match(translated, /Идентификатор расчёта/)
    assert.match(translated, /{{settlement_id}}/)
    assert.match(translated, /{{actual_quota}}/)
  })

  test('localizes batch manual task billing settlements', () => {
    const rendered = renderAuditContent(
      {
        op: {
          action: 'billing.manual_task_settlement_batch_complete',
          params: { completed_count: 2, failed_count: 1 },
        },
      },
      (key: string, opts?: Record<string, unknown>): string =>
        key
          .replace('{{completed_count}}', String(opts?.completed_count))
          .replace('{{failed_count}}', String(opts?.failed_count))
    )

    assert.equal(
      rendered,
      'Completed 2 manual task billing settlements (1 failed)'
    )
  })

  test('localizes zero-quota batch manual task billing settlements', () => {
    const rendered = renderAuditContent(
      {
        op: {
          action: 'billing.manual_task_settlement_batch_zero',
          params: { completed_count: 3, failed_count: 0 },
        },
      },
      (key: string, opts?: Record<string, unknown>): string =>
        key
          .replace('{{completed_count}}', String(opts?.completed_count))
          .replace('{{failed_count}}', String(opts?.failed_count))
    )

    assert.equal(
      rendered,
      'Completed 3 manual task settlements with zero quota (0 failed)'
    )
  })
})
