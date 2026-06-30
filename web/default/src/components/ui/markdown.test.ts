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
import { JSDOM } from 'jsdom'
import { renderMarkdown } from './markdown'

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

describe('renderMarkdown', () => {
  test('renders markdown links without losing the marked parser context', () => {
    const html = renderMarkdown('[MAX API](https://example.com)')

    assert.match(html, /<a href="https:\/\/example\.com"/)
    assert.match(html, /target="_blank"/)
    assert.match(html, /rel="noopener noreferrer"/)
    assert.match(html, />MAX API<\/a>/)
  })

  test('falls back to link text for invalid href values', () => {
    const badHref = `http://example.com/${String.fromCharCode(0xd800)}`
    const html = renderMarkdown(`[Broken](${badHref})`)

    assert.equal(html, '<p>Broken</p>\n')
  })

  test('sanitizes hostile html when rendered outside the browser', () => {
    const html = renderMarkdown('<img src=x onerror=alert(1)>')

    assert.doesNotMatch(html, /onerror/i)
    assert.doesNotMatch(html, /alert\(1\)/i)
  })

  test('removes unsafe link protocols when rendered outside the browser', () => {
    const cases = [
      {
        label: 'XSS',
        markdown: '[XSS](javascript:alert(1))',
        unsafe: /javascript:|alert\(1\)/i,
      },
      {
        label: 'Data',
        markdown: '[Data](data:text/html,<script>alert(1)</script>)',
        unsafe: /data:text\/html|<script|alert\(1\)/i,
      },
    ]

    cases.forEach(({ label, markdown, unsafe }) => {
      const html = renderMarkdown(markdown)

      assert.doesNotMatch(html, unsafe)
      assert.doesNotMatch(html, /href=/i)
      assert.match(html, new RegExp(`>${label}<`))
    })
  })

  test('fails closed when no DOM is available', () => {
    const windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'window')
    Object.defineProperty(globalThis, 'window', {
      configurable: true,
      value: undefined,
    })

    try {
      assert.equal(renderMarkdown('<img src=x onerror=alert(1)>'), '')
    } finally {
      if (windowDescriptor) {
        Object.defineProperty(globalThis, 'window', windowDescriptor)
      }
    }
  })
})
