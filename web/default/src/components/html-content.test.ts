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
import { after, describe, test } from 'node:test'
import DOMPurify from 'dompurify'
import { JSDOM } from 'jsdom'
import { sanitizeHtmlContent } from '@/lib/html-sanitizer'

const dom = new JSDOM('', { url: 'http://localhost/' })
const previousWindowDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'window'
)

Object.defineProperty(globalThis, 'window', {
  configurable: true,
  value: dom.window,
})

after(() => {
  if (previousWindowDescriptor) {
    Object.defineProperty(globalThis, 'window', previousWindowDescriptor)
    return
  }

  delete (globalThis as Partial<typeof globalThis>).window
})

describe('sanitizeHtmlContentForTest', () => {
  test('removes executable attributes from custom HTML content', () => {
    const html = sanitizeHtmlContent(
      '<div>safe<img src="x" onerror=alert(1)><script>alert(2)</script></div>'
    )

    assert.match(html, /safe/)
    assert.match(html, /<img/i)
    assert.doesNotMatch(html, /onerror/i)
    assert.doesNotMatch(html, /<script/i)
    assert.doesNotMatch(html, /alert\(/i)
  })

  test('fails closed when the sanitizer throws', () => {
    const originalIsSupported = Object.getOwnPropertyDescriptor(
      DOMPurify,
      'isSupported'
    )
    const originalSanitize = Object.getOwnPropertyDescriptor(
      DOMPurify,
      'sanitize'
    )

    Object.defineProperty(DOMPurify, 'isSupported', {
      configurable: true,
      value: true,
    })
    Object.defineProperty(DOMPurify, 'sanitize', {
      configurable: true,
      value: () => {
        throw new Error('sanitize failed')
      },
    })

    try {
      assert.equal(sanitizeHtmlContent('<p>safe</p>'), '')
    } finally {
      if (originalSanitize) {
        Object.defineProperty(DOMPurify, 'sanitize', originalSanitize)
      } else {
        delete (DOMPurify as Partial<typeof DOMPurify>).sanitize
      }

      if (originalIsSupported) {
        Object.defineProperty(DOMPurify, 'isSupported', originalIsSupported)
      } else {
        delete (DOMPurify as Partial<typeof DOMPurify>).isSupported
      }
    }
  })

  test('fails closed when sanitizer factory setup throws', () => {
    const windowDescriptor = Object.getOwnPropertyDescriptor(
      globalThis,
      'window'
    )

    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      get() {
        throw new Error('window unavailable')
      },
    })

    try {
      assert.equal(sanitizeHtmlContent('<p>safe</p>'), '')
    } finally {
      if (windowDescriptor) {
        Object.defineProperty(globalThis, 'window', windowDescriptor)
      } else {
        delete (globalThis as Partial<typeof globalThis>).window
      }
    }
  })
})
