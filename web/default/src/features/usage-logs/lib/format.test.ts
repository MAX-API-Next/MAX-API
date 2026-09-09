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
import { renderAuditContent } from './format'

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
})
