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
import { renderMarkdownForTest } from './markdown'

describe('renderMarkdownForTest', () => {
  test('renders markdown links without losing the marked parser context', () => {
    const html = renderMarkdownForTest('[MAX API](https://example.com)')

    assert.match(html, /<a href="https:\/\/example\.com"/)
    assert.match(html, /target="_blank"/)
    assert.match(html, /rel="noopener noreferrer"/)
    assert.match(html, />MAX API<\/a>/)
  })

  test('falls back to link text for invalid href values', () => {
    const badHref = `http://example.com/${String.fromCharCode(0xd800)}`
    const html = renderMarkdownForTest(`[Broken](${badHref})`)

    assert.equal(html, '<p>Broken</p>\n')
  })
})
