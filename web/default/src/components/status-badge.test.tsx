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
import i18n from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { StatusBadge } from './status-badge'

i18n.init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
  interpolation: { escapeValue: false },
})

function renderStatusBadge(element: React.ReactElement) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>{element}</I18nextProvider>
  )
}

describe('StatusBadge long-content layout', () => {
  test('keeps the default compact badge on one truncated line', () => {
    const markup = renderStatusBadge(<StatusBadge label='model-name' />)

    assert.match(markup, /whitespace-nowrap/)
    assert.match(markup, /truncate/)
    assert.doesNotMatch(markup, /\[overflow-wrap:anywhere\]/)
  })

  test('allows explicitly wrapped badge labels to use multiple lines', () => {
    const markup = renderStatusBadge(
      <StatusBadge label={'model-'.repeat(20)} wrap />
    )

    assert.match(markup, /whitespace-normal/)
    assert.match(markup, /\[overflow-wrap:anywhere\]/)
    assert.match(markup, /min-h-5/)
    assert.doesNotMatch(markup, /truncate/)
  })
})
