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
import { createInstance } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { StatusCodeRiskConfirmationContent } from './status-code-risk-dialog'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
  interpolation: { escapeValue: false },
})

describe('StatusCodeRiskDialog', () => {
  test('keeps risk notices without checkbox or text confirmation gates', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <StatusCodeRiskConfirmationContent
          detailItems={['200 -> 500']}
          onCancel={() => undefined}
          onConfirm={() => undefined}
        />
      </I18nextProvider>
    )

    assert.match(markup, /200 -&gt; 500/)
    for (let index = 1; index <= 4; index += 1) {
      assert.match(
        markup,
        new RegExp(`High-risk status code retry risk check ${index}`)
      )
    }
    assert.doesNotMatch(markup, /type="checkbox"/)
    assert.doesNotMatch(markup, /<input/)
    assert.doesNotMatch(markup, /<button[^>]*\sdisabled(?:=|\s|>)/)
    assert.match(markup, /I confirm enabling high-risk retry/)
  })
})
