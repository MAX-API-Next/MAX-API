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
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import { JsonEditor } from './json-editor'

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
})
const i18n = createInstance()
const globalKeys = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'Element',
  'Node',
  'Event',
  'MouseEvent',
  'MutationObserver',
  'getComputedStyle',
  'IS_REACT_ACT_ENVIRONMENT',
] as const
const previousDescriptors = new Map<
  (typeof globalKeys)[number],
  PropertyDescriptor | undefined
>()

function setGlobal(key: (typeof globalKeys)[number], value: unknown) {
  previousDescriptors.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
  Object.defineProperty(globalThis, key, {
    configurable: true,
    writable: true,
    value,
  })
}

async function unmount(root: Root, container: HTMLElement) {
  await act(async () => root.unmount())
  container.remove()
}

before(async () => {
  await i18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })

  const window = dom.window
  setGlobal('window', window)
  setGlobal('document', window.document)
  setGlobal('navigator', window.navigator)
  setGlobal('HTMLElement', window.HTMLElement)
  setGlobal('Element', window.Element)
  setGlobal('Node', window.Node)
  setGlobal('Event', window.Event)
  setGlobal('MouseEvent', window.MouseEvent)
  setGlobal('MutationObserver', window.MutationObserver)
  setGlobal('getComputedStyle', window.getComputedStyle.bind(window))
  setGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

after(() => {
  for (const key of [...globalKeys].reverse()) {
    const descriptor = previousDescriptors.get(key)
    if (descriptor) {
      Object.defineProperty(globalThis, key, descriptor)
    } else {
      Reflect.deleteProperty(globalThis, key)
    }
  }
  dom.window.close()
})

describe('JsonEditor controlled value hydration', () => {
  test('renders existing JSON without clearing it when modes change', async () => {
    const container = dom.window.document.createElement('div')
    dom.window.document.body.append(container)
    const root = createRoot(container)
    const changes: string[] = []
    const value = '{"429":"503"}'

    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <JsonEditor value={value} onChange={(next) => changes.push(next)} />
          </I18nextProvider>
        )
      })

      assert.deepEqual(
        Array.from(container.querySelectorAll('input')).map(
          (input) => input.value
        ),
        ['429', '503']
      )

      const modeButton = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.includes('JSON Mode')
      )
      assert.ok(modeButton)
      await act(async () => {
        modeButton.dispatchEvent(
          new dom.window.MouseEvent('click', { bubbles: true })
        )
      })

      assert.equal(container.querySelector('textarea')?.value, value)
      assert.deepEqual(changes, [])
    } finally {
      await unmount(root, container)
    }
  })

  test('refreshes visual rows when the controlled value changes externally', async () => {
    const container = dom.window.document.createElement('div')
    dom.window.document.body.append(container)
    const root = createRoot(container)
    const renderEditor = (value: string) => (
      <I18nextProvider i18n={i18n}>
        <JsonEditor value={value} onChange={() => undefined} />
      </I18nextProvider>
    )

    try {
      await act(async () => root.render(renderEditor('{"429":"503"}')))
      await act(async () => root.render(renderEditor('{"500":"502"}')))

      assert.deepEqual(
        Array.from(container.querySelectorAll('input')).map(
          (input) => input.value
        ),
        ['500', '502']
      )
    } finally {
      await unmount(root, container)
    }
  })
})
