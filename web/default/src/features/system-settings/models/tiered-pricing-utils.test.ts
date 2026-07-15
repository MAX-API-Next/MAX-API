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
import type { TFunction } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatTokenHint } from './tiered-pricing-utils'

const translations: Record<string, string> = {
  '= {{count}} tokens': '= {{count}} localized tokens',
  '= {{count}}K tokens': '= {{count}} localized thousands',
  '= {{count}}M tokens': '= {{count}} localized millions',
}

const translate = ((key: string, options?: { count?: unknown }) =>
  translations[key]?.replace('{{count}}', String(options?.count)) ??
  key) as TFunction

describe('formatTokenHint', () => {
  test('keeps empty, invalid, and zero values stable', () => {
    assert.equal(formatTokenHint('', translate), '')
    assert.equal(formatTokenHint('invalid', translate), '')
    assert.equal(formatTokenHint(0, translate), '= 0')
  })

  test('localizes plain, thousand, and million token units', () => {
    assert.equal(formatTokenHint(999, translate), '= 999 localized tokens')
    assert.equal(
      formatTokenHint(1_250, translate),
      '= 1.25 localized thousands'
    )
    assert.equal(
      formatTokenHint(2_500_000, translate),
      '= 2.5 localized millions'
    )
  })
})
