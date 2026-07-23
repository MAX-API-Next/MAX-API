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
import { PricingSidebar, type PricingSidebarProps } from './pricing-sidebar'

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
  'KeyboardEvent',
  'MutationObserver',
  'getComputedStyle',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'ResizeObserver',
  'IS_REACT_ACT_ENVIRONMENT',
] as const
const previousDescriptors = new Map<
  (typeof globalKeys)[number],
  PropertyDescriptor | undefined
>()

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function setGlobal(key: (typeof globalKeys)[number], value: unknown) {
  previousDescriptors.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
  Object.defineProperty(globalThis, key, {
    configurable: true,
    writable: true,
    value,
  })
}

function restoreGlobals() {
  for (const key of [...globalKeys].reverse()) {
    const descriptor = previousDescriptors.get(key)
    if (descriptor) {
      Object.defineProperty(globalThis, key, descriptor)
    } else {
      Reflect.deleteProperty(globalThis, key)
    }
  }
}

function createContainer() {
  const container = dom.window.document.createElement('div')
  dom.window.document.body.append(container)
  return container
}

async function unmount(root: Root, container: HTMLElement) {
  await act(async () => root.unmount())
  container.remove()
}

const sidebarProps: PricingSidebarProps = {
  quotaTypeFilter: 'all',
  endpointTypeFilter: 'all',
  vendorFilter: 'all',
  groupFilter: 'all',
  tagFilter: 'all',
  onQuotaTypeChange: () => undefined,
  onEndpointTypeChange: () => undefined,
  onVendorChange: () => undefined,
  onGroupChange: () => undefined,
  onTagChange: () => undefined,
  vendors: [],
  groups: ['vip', 'fallback'],
  groupRatios: { vip: 1, fallback: 0.5 },
  autoGroups: [],
  autoRoutes: [
    {
      key: 'auto:fast',
      name: 'Fast lane',
      enabled: true,
      user_selectable: true,
      groups: ['vip', 'fallback'],
    },
  ],
  tags: [],
  models: [],
  hasActiveFilters: false,
  onClearFilters: () => undefined,
}

before(async () => {
  await i18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })

  const window = dom.window
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({
      matches: false,
      media: '',
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  })

  setGlobal('window', window)
  setGlobal('document', window.document)
  setGlobal('navigator', window.navigator)
  setGlobal('HTMLElement', window.HTMLElement)
  setGlobal('Element', window.Element)
  setGlobal('Node', window.Node)
  setGlobal('Event', window.Event)
  setGlobal('MouseEvent', window.MouseEvent)
  setGlobal('KeyboardEvent', window.KeyboardEvent)
  setGlobal('MutationObserver', window.MutationObserver)
  setGlobal('getComputedStyle', window.getComputedStyle.bind(window))
  setGlobal('requestAnimationFrame', (callback: FrameRequestCallback) =>
    setTimeout(() => callback(Date.now()), 0)
  )
  setGlobal('cancelAnimationFrame', (handle: number) => clearTimeout(handle))
  setGlobal('ResizeObserver', ResizeObserverStub)
  setGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

after(() => {
  restoreGlobals()
  dom.window.close()
})

describe('PricingSidebar auto route chains', () => {
  test('keeps configured auto route details collapsed until requested', async () => {
    const container = createContainer()
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <PricingSidebar {...sidebarProps} />
          </I18nextProvider>
        )
      })

      const trigger = Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent?.includes('Auto route chains')
      )

      assert.ok(trigger)
      assert.equal(trigger.getAttribute('aria-expanded'), 'false')
      assert.doesNotMatch(container.textContent || '', /Fast lane/)

      await act(async () => {
        trigger.click()
      })

      assert.equal(trigger.getAttribute('aria-expanded'), 'true')
      assert.match(container.textContent || '', /Fast lane/)
    } finally {
      await unmount(root, container)
    }
  })
})
