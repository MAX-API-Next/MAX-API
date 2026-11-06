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
import type { Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, afterEach, beforeEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { LogOtherData, UsageLog } from '../../types'

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const previousGlobals = new Map<string, PropertyDescriptor | undefined>()
const window = dom.window
const matchMedia = (query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener() {},
  removeListener() {},
  addEventListener() {},
  removeEventListener() {},
  dispatchEvent: () => false,
})
window.matchMedia = matchMedia
const globals: Record<string, unknown> = {
  window,
  document: window.document,
  navigator: window.navigator,
  getComputedStyle: window.getComputedStyle.bind(window),
  requestAnimationFrame: window.requestAnimationFrame.bind(window),
  cancelAnimationFrame: window.cancelAnimationFrame.bind(window),
  localStorage: window.localStorage,
  matchMedia,
  IS_REACT_ACT_ENVIRONMENT: true,
}
for (const name of [
  'HTMLElement',
  'HTMLInputElement',
  'Element',
  'Node',
  'NodeFilter',
  'DocumentFragment',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'customElements',
  'ShadowRoot',
  'SVGElement',
  'CSSStyleSheet',
] as const) {
  globals[name] = window[name]
}
for (const [key, value] of Object.entries(globals)) {
  previousGlobals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
  Object.defineProperty(globalThis, key, {
    configurable: true,
    writable: true,
    value,
  })
}
const i18n = createInstance()
await i18n.init({
  lng: 'en',
  fallbackLng: 'en',
  resources: { en: { translation: {} } },
})
const { createRoot } = await import('react-dom/client')
const { within } = await import('@testing-library/react/pure')
const { DetailsDialog } = await import('./details-dialog')
const { useCommonLogsColumns } = await import('../columns/common-logs-columns')
let root: Root
let container: HTMLDivElement
let client: QueryClient

beforeEach(() => {
  container = window.document.createElement('div')
  window.document.body.append(container)
  root = createRoot(container)
  client = new QueryClient()
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
})
after(() => {
  for (const [key, descriptor] of [...previousGlobals].reverse()) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
  dom.window.close()
})

function syntheticLog(other: LogOtherData): UsageLog {
  return {
    id: 0,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'synthetic',
    token_name: 'synthetic',
    model_name: 'synthetic',
    quota: 10,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 1,
    is_stream: false,
    channel: 1,
    group: 'default',
    other: JSON.stringify(other),
  }
}

async function show(other: LogOtherData) {
  // id=0 disables detail fetching: all facts come from this synthetic log.
  const log = syntheticLog(other)
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <DetailsDialog log={log} isAdmin open onOpenChange={() => {}} />
        </I18nextProvider>
      </QueryClientProvider>
    )
  )
  return within(window.document.body)
}

test('audio output unit price uses input audio and output multipliers', async () => {
  for (const [outputRatio, price] of [
    [3, 24],
    [1, 8],
    [0, 0],
  ] as const) {
    const screen = await show({
      model_price: -1,
      model_ratio: 1,
      audio_ratio: 4,
      audio_completion_ratio: outputRatio,
    })
    const row = screen.getByText('Audio output').parentElement
    assert.ok(
      row?.textContent?.includes(`$${price}/M`),
      `expected USD ${price}/M from 2 * 4 * ${outputRatio}, got ${row?.textContent}`
    )
  }
})

test('cache-only logs keep token breakdown and the full aggregate cache write', async () => {
  const screen = await show({
    model_price: -1,
    model_ratio: 1,
    cache_tokens: 100,
    cache_creation_tokens: 100,
    cache_creation_tokens_5m: 40,
    cache_creation_tokens_1h: 40,
  })
  assert.ok(screen.getByText('Token Breakdown'))
  assert.match(
    screen.getByText('Cache Read').parentElement?.textContent || '',
    /100/
  )
  // The configured legacy contract assigns the unspecified 20-token remainder to 5m.
  assert.match(
    screen.getByText('Cache Write (5m)').parentElement?.textContent || '',
    /60/
  )
  assert.match(
    screen.getByText('Cache Write (1h)').parentElement?.textContent || '',
    /40/
  )
})

function TokenCell(props: { other: LogOtherData }) {
  const columns = useCommonLogsColumns(false)
  const table = useReactTable({
    data: [syntheticLog(props.other)],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((item) => item.column.id === 'prompt_tokens')
  assert.ok(cell)
  return <div>{flexRender(cell.column.columnDef.cell, cell.getContext())}</div>
}

test('token table cell displays cache-only usage without dropping the aggregate remainder', async () => {
  await act(async () =>
    root.render(
      <I18nextProvider i18n={i18n}>
        <TokenCell
          other={{
            cache_tokens: 100,
            cache_creation_tokens: 100,
            cache_creation_tokens_5m: 40,
            cache_creation_tokens_1h: 40,
          }}
        />
      </I18nextProvider>
    )
  )
  assert.match(container.textContent || '', /Cache↓ 100/)
  assert.match(container.textContent || '', /↑ 100/)
})
