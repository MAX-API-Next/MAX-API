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
import { act, type ComponentProps } from 'react'
import type { Root } from 'react-dom/client'
import type { AxiosResponse, InternalAxiosRequestConfig } from 'axios'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, afterEach, beforeEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { UserInfo } from '../../types'

let createRoot: typeof import('react-dom/client').createRoot
let within: typeof import('@testing-library/react/pure').within
let UserInfoDialog: typeof import('./user-info-dialog').UserInfoDialog
let api: typeof import('@/lib/api').api
let originalAdapter: typeof api.defaults.adapter
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const i18n = createInstance()
const previousGlobals = new Map<string, PropertyDescriptor | undefined>()
const requests: InternalAxiosRequestConfig[] = []
const user: UserInfo = {
  id: 7,
  username: 'lint-user',
  display_name: 'Lint User',
  quota: 1000000,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  aff_code: 'synthetic-invitation',
  aff_count: 0,
  aff_quota: 0,
}
let container: HTMLDivElement
let root: Root

async function setup(): Promise<void> {
  const window = dom.window
  const globals: Record<string, unknown> = {
    window,
    document: window.document,
    navigator: window.navigator,
    HTMLElement: window.HTMLElement,
    Element: window.Element,
    Node: window.Node,
    Event: window.Event,
    MouseEvent: window.MouseEvent,
    MutationObserver: window.MutationObserver,
    getComputedStyle: window.getComputedStyle.bind(window),
    requestAnimationFrame: window.requestAnimationFrame.bind(window),
    cancelAnimationFrame: window.cancelAnimationFrame.bind(window),
    localStorage: window.localStorage,
    IS_REACT_ACT_ENVIRONMENT: true,
  }
  for (const [key, value] of Object.entries(globals)) {
    previousGlobals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
    Object.defineProperty(globalThis, key, {
      configurable: true,
      writable: true,
      value,
    })
  }
  await i18n.init({
    lng: 'en',
    fallbackLng: 'en',
    resources: { en: { translation: {} } },
  })
  ;({ createRoot } = await import('react-dom/client'))
  ;({ within } = await import('@testing-library/react/pure'))
  ;({ UserInfoDialog } = await import('./user-info-dialog'))
  ;({ api } = await import('@/lib/api'))
  originalAdapter = api.defaults.adapter
}

await setup()

beforeEach((): void => {
  container = dom.window.document.createElement('div')
  dom.window.document.body.append(container)
  root = createRoot(container)
  requests.length = 0
  api.defaults.adapter = async (
    config: InternalAxiosRequestConfig
  ): Promise<AxiosResponse> => {
    requests.push(config)
    assert.equal(config.url, '/api/user/7')
    assert.equal(config.method, 'get')
    return {
      config,
      data: { success: true, data: user },
      status: 200,
      statusText: 'OK',
      headers: {},
    }
  }
})

afterEach(async (): Promise<void> => {
  await act(async (): Promise<void> => root.unmount())
  container.remove()
  api.defaults.adapter = originalAdapter
})

after((): void => {
  for (const [key, descriptor] of [...previousGlobals].reverse()) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
  dom.window.close()
})

async function renderDialog(
  onOpenChange: ComponentProps<typeof UserInfoDialog>['onOpenChange']
): Promise<void> {
  await act(async (): Promise<void> => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <UserInfoDialog userId={7} open onOpenChange={onOpenChange} />
      </I18nextProvider>
    )
  })
}

test('preserves selected user information across unrelated parent updates', async (): Promise<void> => {
  await renderDialog(() => undefined)
  const screen = within(dom.window.document.body)
  const username = screen.getByText('lint-user')
  const range = dom.window.document.createRange()
  range.selectNodeContents(username)
  const selection = dom.window.getSelection()
  assert.ok(selection)
  selection.removeAllRanges()
  selection.addRange(range)
  assert.equal(selection.toString(), 'lint-user')

  await renderDialog(() => undefined)

  assert.equal(requests.length, 1, 'parent updates must not refetch the user')
  assert.ok(
    screen.getByText('lint-user') === username,
    'user fields must stay mounted'
  )
  assert.equal(
    selection.toString(),
    'lint-user',
    'selected text must stay selected'
  )
  assert.ok(screen.getByText('Lint User'))
  assert.ok(screen.getByText('synthetic-invitation'))
  assert.equal(
    screen.getByText('Invited Users').nextElementSibling?.textContent,
    '0'
  )
})
