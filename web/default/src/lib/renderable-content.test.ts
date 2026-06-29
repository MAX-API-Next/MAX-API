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
import {
  getRenderableContentKind,
  getSafeIframeEmbedSrc,
} from './renderable-content'

const dom = new JSDOM('')
const previousDOMParserDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'DOMParser'
)

Object.defineProperty(globalThis, 'DOMParser', {
  configurable: true,
  value: dom.window.DOMParser,
})

after(() => {
  if (previousDOMParserDescriptor) {
    Object.defineProperty(
      globalThis,
      'DOMParser',
      previousDOMParserDescriptor
    )
    return
  }

  delete (globalThis as Partial<typeof globalThis>).DOMParser
})

describe('getRenderableContentKind', () => {
  test('treats a complete http URL as iframe content', () => {
    assert.equal(
      getRenderableContentKind('https://example.com/home.html'),
      'url'
    )
  })

  test('treats custom style and iframe markup as HTML content', () => {
    const content = `
      <style>
      .semi-layout-footer { display: none !important; }
      </style>
      <div>
        <iframe src="https://aioagi.tech/home.html"></iframe>
      </div>
    `

    assert.equal(getRenderableContentKind(content), 'html')
  })

  test('treats plain text as Markdown content', () => {
    assert.equal(getRenderableContentKind('# Welcome'), 'markdown')
  })
})

describe('getSafeIframeEmbedSrc', () => {
  test('extracts an https iframe src from a pure embed snippet', () => {
    assert.equal(
      getSafeIframeEmbedSrc(
        '<iframe src="https://example.com/home.html"></iframe>'
      ),
      'https://example.com/home.html'
    )
  })

  test('extracts an iframe src when the snippet only has wrapper elements', () => {
    assert.equal(
      getSafeIframeEmbedSrc(
        '<div><iframe src="https://example.com/home.html"></iframe></div>'
      ),
      'https://example.com/home.html'
    )
  })

  test('rejects iframe snippets with additional HTML content', () => {
    assert.equal(
      getSafeIframeEmbedSrc(
        '<section><h1>Welcome</h1><iframe src="https://example.com"></iframe></section>'
      ),
      null
    )
  })

  test('rejects non-http iframe src values', () => {
    assert.equal(
      getSafeIframeEmbedSrc('<iframe src="javascript:alert(1)"></iframe>'),
      null
    )
  })
})
