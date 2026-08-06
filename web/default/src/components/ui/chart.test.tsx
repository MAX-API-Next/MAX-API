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
import React from 'react'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { ChartStyle } from './chart'

describe('ChartStyle', () => {
  test('does not allow chart config CSS to escape the style element', () => {
    const payload =
      'red;}</style><img id="chart-xss" src=x onerror="alert(1)"><style>{'
    const markup = renderToStaticMarkup(
      React.createElement(ChartStyle, {
        id: 'chart-test] {} body { background: url(https://example.com)',
        config: { attacker: { color: payload } },
      })
    )
    const document = new JSDOM(markup).window.document
    const style = document.querySelector('style')?.textContent ?? ''

    assert.equal(document.querySelector('#chart-xss'), null)
    assert.equal(document.querySelector('script'), null)
    assert.doesNotMatch(style, /url\s*\(|body\s*\{/i)
  })
})
