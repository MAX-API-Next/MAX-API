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
import { JSDOM } from 'jsdom'
import { sanitizeHtmlContent } from '@/lib/html-sanitizer'

const dom = new JSDOM('')
Object.defineProperty(globalThis, 'window', {
  configurable: true,
  value: dom.window,
})

describe('sanitizeHtmlContentForTest', () => {
  test('removes executable attributes from custom HTML content', () => {
    const html = sanitizeHtmlContent(
      '<div><img src=x onerror=alert(1)><script>alert(2)</script></div>'
    )

    assert.doesNotMatch(html, /onerror/i)
    assert.doesNotMatch(html, /<script/i)
    assert.doesNotMatch(html, /alert\(/i)
  })
})
