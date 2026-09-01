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

  test('keeps active reconciliation terminology localized and consistent', (): void => {
    const reviewSummaryKey =
      'Select one or more alerts and close them with one click. The underlying financial settlement record remains available for safe retry and audit.'
    const selectionKey = 'Review and close selected ({{count}})'
    const blockingPolicyKey = 'Block affected users by default'

    assert.doesNotMatch(ru.translation[reviewSummaryKey], /pending|manual/i)
    assert.doesNotMatch(ja.translation[reviewSummaryKey], /pending|manual/i)
    assert.equal(
      ru.translation['Open manual settlements: {{count}}'],
      'Открытые расчёты на ручной обработке: {{count}}'
    )
    assert.equal(
      ru.translation['Open pending settlements: {{count}}'],
      'Открытые расчёты в ожидании: {{count}}'
    )
    assert.equal(
      ja.translation['Open pending settlements: {{count}}'],
      '未対応の保留中精算：{{count}}件'
    )
    assert.equal(
      vi.translation['Open pending settlements: {{count}}'],
      'Quyết toán đang chờ có cảnh báo mở: {{count}}'
    )
    assert.equal(
      vi.translation['Open manual settlements: {{count}}'],
      'Quyết toán thủ công có cảnh báo mở: {{count}}'
    )
    assert.equal(
      zh.translation['Open pending settlements: {{count}}'],
      '未关闭的待结算：{{count}}'
    )
    assert.equal(
      zh.translation['Open manual settlements: {{count}}'],
      '未关闭的需人工处理的结算：{{count}}'
    )
    assert.doesNotMatch(
      zh.translation['Open pending settlements: {{count}}'],
      /重试|处理/
    )
    assert.doesNotMatch(
      ja.translation['Open pending settlements: {{count}}'],
      /再試行/
    )
    assert.doesNotMatch(
      ru.translation['Open pending settlements: {{count}}'],
      /повтор/i
    )
    assert.doesNotMatch(
      vi.translation['Open pending settlements: {{count}}'],
      /thử lại/i
    )
    assert.equal(
      vi.translation['Open billing reconciliation alerts'],
      'Cảnh báo đối soát tính phí đang mở'
    )
    assert.equal(
      ja.translation['Open billing reconciliation alerts'],
      '未対応の請求照合アラート'
    )
    assert.equal(
      ru.translation['Open billing reconciliation alerts'],
      'Открытые оповещения о сверке биллинга'
    )
    assert.doesNotMatch(
      ja.translation['Billing reconciliation alerts closed: {{count}}'],
      /課金照合/
    )
    assert.doesNotMatch(
      ru.translation['Billing reconciliation alerts closed: {{count}}'],
      /сверк[аеи] расчётов/i
    )
    assert.equal(
      zh.translation['No open reconciliation alerts.'],
      '没有未关闭的对账告警。'
    )
    assert.equal(zh.translation[blockingPolicyKey], '默认阻止受影响用户')
    assert.equal(
      zh.translation[selectionKey],
      '核对并关闭所选告警（{{count}}）'
    )
  })
})
