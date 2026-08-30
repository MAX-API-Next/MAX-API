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
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatLegacyLatency, formatLocalizedCount } from './format'

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
})
