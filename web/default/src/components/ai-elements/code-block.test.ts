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
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, describe, test } from 'node:test'
import { renderHighlightedCode, sanitizeHighlightedCode } from './code-block'

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

describe('renderHighlightedCode', () => {
  test('sanitizes pre-rendered HTML independently of Shiki escaping', () => {
    const html = sanitizeHighlightedCode(
      '<pre><code>safe</code></pre><img src=x onerror=alert(1)><script>alert(2)</script>'
    )
    const fragment = JSDOM.fragment(html)

    const image = fragment.querySelector('img')

    assert.notEqual(image, null)
    assert.equal(image?.hasAttribute('onerror'), false)
    assert.equal(fragment.querySelector('script'), null)
    assert.equal(fragment.textContent?.includes('safe'), true)
  })

  test('sanitizes highlighted code before it reaches innerHTML', async () => {
    const payload =
      '</code></pre><img src=x onerror=alert(1)><script>alert(2)</script>'
    const html = await renderHighlightedCode(payload, 'html')
    const fragment = JSDOM.fragment(html)

    assert.equal(fragment.querySelector('img'), null)
    assert.equal(fragment.querySelector('script'), null)
    assert.match(fragment.textContent ?? '', /onerror/)
  })
})
