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
import fr from '@/i18n/locales/fr.json'
import ja from '@/i18n/locales/ja.json'
import ru from '@/i18n/locales/ru.json'
import vi from '@/i18n/locales/vi.json'
import zh from '@/i18n/locales/zh.json'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  formatDurationSeconds,
  formatLegacyLatency,
  formatLocalizedCount,
  formatPercent,
} from './format'

describe('formatLegacyLatency', () => {
  test('keeps a recorded zero-second average distinct from missing data', () => {
    assert.equal(formatLegacyLatency(0), '0ms')
    assert.equal(formatLegacyLatency(null), '—')
  })

  test('uses the shared latency format for positive values', () => {
    assert.equal(formatLegacyLatency(500), '500ms')
    assert.equal(formatLegacyLatency(1_500), '1.50s')
  })
})

describe('formatPercent', () => {
  test('uses locale-aware percent symbols and spacing', () => {
    assert.equal(formatPercent(12.3, 'en-US'), '12.3%')
    assert.equal(formatPercent(12.3, 'fr-FR'), '12,3\u00a0%')
  })
})

describe('formatLocalizedCount', () => {
  test('uses singular and plural French labels from the shared locale catalog', async () => {
    const instance = i18next.createInstance()
    await instance.init({
      lng: 'fr',
      fallbackLng: 'en',
      resources: { fr },
      interpolation: { escapeValue: false },
    })

    assert.equal(
      formatLocalizedCount(
        1,
        'fr',
        instance.t,
        '{{count}} attempt',
        '{{count}} attempts'
      ),
      '1 tentative'
    )
    assert.equal(
      formatLocalizedCount(
        2,
        'fr',
        instance.t,
        '{{count}} attempt',
        '{{count}} attempts'
      ),
      '2 tentatives'
    )
  })

  test('uses count-neutral Russian labels across one, few, and many values', async () => {
    const instance = i18next.createInstance()
    await instance.init({
      lng: 'ru',
      fallbackLng: 'en',
      resources: { ru },
      interpolation: { escapeValue: false },
    })

    assert.equal(
      formatDurationSeconds(90_061, 'ru', instance.t),
      'Дней: 1 Часов: 1'
    )
    assert.equal(
      formatDurationSeconds(180_122, 'ru', instance.t),
      'Дней: 2 Часов: 2'
    )
    assert.equal(
      formatDurationSeconds(61, 'ru', instance.t),
      'Минут: 1 Секунд: 1'
    )
    assert.equal(
      formatLocalizedCount(
        5,
        'ru',
        instance.t,
        'Reviewed record: {{count}}',
        'Reviewed records: {{count}}'
      ),
      'Проверено записей: 5'
    )
  })

  test('keeps reconciliation terminology localized and consistent', (): void => {
    const reviewSummaryKey =
      'Review and close operational alerts without changing the underlying pending or manual financial settlement.'
    const emptyStateKey = 'No unresolved reconciliation records.'
    const blockingPolicyKey = 'Block affected users by default'
    const userAccessHeadingKey = 'User access'
    const userAccessKey = 'User access while unresolved'
    const rootPolicyKey =
      'Only root administrators can change the default blocking policy.'
    const closeAlertKey =
      'Closing this alert records an administrator review only. It does not mark the settlement as applied or change any balance.'
    const positiveSettlementKey =
      'When enabled, unresolved positive final settlements block new paid requests unless a reviewed record explicitly allows the user to continue.'

    assert.doesNotMatch(ru.translation[reviewSummaryKey], /pending|manual/i)
    assert.doesNotMatch(
      ja.translation[reviewSummaryKey],
      /決済|pending|manual/i
    )
    assert.equal(fr.translation['Token #{{id}}'], 'Jeton n° {{id}}')
    assert.equal(
      fr.translation['Administrator review'],
      'Examen par l’administrateur'
    )
    assert.doesNotMatch(fr.translation['Administrator review'], /vérification/i)
    assert.equal(fr.translation.Reviewed, 'Examiné')
    assert.doesNotMatch(ru.translation[closeAlertKey], /\bapplied\b/i)
    assert.equal(
      ru.translation['Manual: {{count}}'],
      'Ручная обработка: {{count}}'
    )
    assert.equal(ru.translation['Open alert'], 'Открыть оповещение')
    assert.match(vi.translation[positiveSettlementKey], /có số tiền dương/)
    assert.match(vi.translation[rootPolicyKey], /quản trị viên Root/)
    assert.equal(zh.translation[emptyStateKey], '没有未解决的对账记录。')
    assert.equal(zh.translation[blockingPolicyKey], '默认阻止受影响用户')
    assert.equal(zh.translation[userAccessHeadingKey], '用户访问状态')
    assert.equal(zh.translation[userAccessKey], '未解决期间的用户访问权限')
  })
})
