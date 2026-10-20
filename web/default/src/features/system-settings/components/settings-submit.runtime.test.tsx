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
import { act, createRef, type ReactElement } from 'react'
import type { Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, afterEach, beforeEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const previousGlobals = new Map<string, PropertyDescriptor | undefined>()
const i18n = createInstance()
const window = dom.window
Object.defineProperty(window, 'matchMedia', {
  value: () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }),
})
Object.defineProperty(window.HTMLElement.prototype, 'scrollIntoView', {
  value() {},
})
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
  resources: { en: { translation: {} } },
})
const { createRoot } = await import('react-dom/client')
const { fireEvent, waitFor, within } =
  await import('@testing-library/react/pure')
const { api } = await import('@/lib/api')
const { Accordion } = await import('@/components/ui/accordion')
const { PasskeySection } = await import('../auth/passkey-section')
const { ChatSettingsSection } = await import('../content/chat-settings-section')
const { JsonToggleSection } = await import('../content/json-toggle-section')
const { ClaudeSettingsCard } = await import('../models/claude-settings-card')
const { SettingsPageProvider, SettingsPageFormActions } =
  await import('./settings-page-context')

let root: Root
let container: HTMLDivElement
let actions: HTMLDivElement
let client: QueryClient
let originalAdapter: typeof api.defaults.adapter
let writes: Array<{ key: string; value: unknown }>

beforeEach(() => {
  container = document.createElement('div')
  actions = document.createElement('div')
  document.body.append(container, actions)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  writes = []
  originalAdapter = api.defaults.adapter
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'put')
    assert.equal(config.url, '/api/option/')
    writes.push(JSON.parse(String(config.data)))
    return {
      data: { success: true },
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
    }
  }
})

afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  api.defaults.adapter = originalAdapter
  container.remove()
  actions.remove()
})

after(() => {
  for (const [key, descriptor] of previousGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
  dom.window.close()
})

async function render(element: ReactElement) {
  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <SettingsPageProvider actionsContainer={actions}>
            {element}
          </SettingsPageProvider>
        </QueryClientProvider>
      </I18nextProvider>
    )
  })
}

async function save(name = 'Save Changes') {
  await act(async () => {
    fireEvent.click(within(actions).getByRole('button', { name }))
  })
}

async function change(label: string, value: string) {
  await act(async () => {
    fireEvent.change(within(container).getByLabelText(label), {
      target: { value },
    })
  })
}

async function submit() {
  const form = container.querySelector('form')
  assert.ok(form)
  await act(async () => {
    fireEvent.submit(form)
  })
}

test('Passkey button and form submit use current values and retain the saved baseline', async () => {
  const defaults = {
    'passkey.enabled': false,
    'passkey.rp_display_name': 'Initial',
    'passkey.rp_id': 'example.invalid',
    'passkey.origins': 'https://example.invalid',
    'passkey.allow_insecure_origin': false,
    'passkey.user_verification': 'preferred' as const,
    'passkey.attachment_preference': '' as const,
  }
  await render(<PasskeySection defaultValues={defaults} />)
  assert.equal(writes.length, 0)
  await change('Relying Party Display Name', 'First edit')
  await render(<PasskeySection defaultValues={{ ...defaults }} />)
  assert.equal(
    (
      within(container).getByLabelText(
        'Relying Party Display Name'
      ) as HTMLInputElement
    ).value,
    'First edit'
  )
  await save()
  await waitFor(() => assert.equal(writes.length, 1))
  await change('Relying Party Display Name', 'Second edit')
  await submit()
  await waitFor(() => assert.equal(writes.length, 2))
  await save()
  assert.deepEqual(writes, [
    { key: 'passkey.rp_display_name', value: 'First edit' },
    { key: 'passkey.rp_display_name', value: 'Second edit' },
  ])
  await render(
    <PasskeySection
      defaultValues={{ ...defaults, 'passkey.rp_display_name': 'Server value' }}
    />
  )
  assert.equal(
    (
      within(container).getByLabelText(
        'Relying Party Display Name'
      ) as HTMLInputElement
    ).value,
    'Server value'
  )
  await save()
  assert.equal(writes.length, 2)
})

test('chat submit validates JSON and saves the latest normalized preset', async () => {
  await render(<ChatSettingsSection defaultValue='[]' />)
  await save('Save chat settings')
  assert.equal(writes.length, 0)
  await act(async () => {
    fireEvent.click(within(container).getByRole('tab', { name: 'JSON' }))
  })
  await change('Chat configuration JSON', 'invalid')
  await submit()
  assert.equal(writes.length, 0)
  await change(
    'Chat configuration JSON',
    '[ { "Client": "https://example.invalid/chat" } ]'
  )
  await save('Save chat settings')
  await waitFor(() =>
    assert.deepEqual(writes, [
      { key: 'Chats', value: '[{"Client":"https://example.invalid/chat"}]' },
    ])
  )
})

test('JSON settings preserve explicit false and save JSON only after validation', async () => {
  await render(
    <Accordion defaultValue={['modules']}>
      <JsonToggleSection
        value='modules'
        title='Modules'
        optionKey='TestModules'
        enabledKey='TestModulesEnabled'
        defaultEnabled
        defaultValue='[]'
        textareaLabel='Modules JSON'
      />
    </Accordion>
  )
  await act(async () => {
    fireEvent.click(within(container).getByRole('switch'))
  })
  await change('Modules JSON', '[ { "name": "one" } ]')
  await submit()
  await waitFor(() =>
    assert.deepEqual(writes, [
      { key: 'TestModulesEnabled', value: false },
      { key: 'TestModules', value: '[{"name":"one"}]' },
    ])
  )
})

test('Claude submit keeps JSON normalization and skips unchanged values', async () => {
  await render(
    <ClaudeSettingsCard
      defaultValues={{
        claude: {
          model_headers_settings: '{}',
          default_max_tokens: '{}',
          thinking_adapter_enabled: false,
          thinking_adapter_budget_tokens_percentage: 0.5,
        },
      }}
    />
  )
  await save()
  assert.equal(writes.length, 0)
  await change(
    'Request Header Overrides',
    '{ "model": { "x-example": "value" } }'
  )
  await submit()
  await waitFor(() =>
    assert.deepEqual(writes, [
      {
        key: 'claude.model_headers_settings',
        value: '{"model":{"x-example":"value"}}',
      },
    ])
  )
})

test('settings actions forward the button ref and respect save/reset disabled states', async () => {
  const ref = createRef<HTMLButtonElement>()
  let saves = 0
  let resets = 0
  const props = {
    saveButtonRef: ref,
    onSave: () => {
      saves += 1
    },
    onReset: () => {
      resets += 1
    },
  }
  await render(<SettingsPageFormActions {...props} />)
  const button = within(actions).getByRole('button', { name: 'Save Changes' })
  assert.equal(ref.current === button, true)
  ref.current?.focus()
  assert.equal(document.activeElement === button, true)
  await save()
  await act(async () =>
    fireEvent.click(within(actions).getByRole('button', { name: 'Reset' }))
  )
  assert.equal(saves, 1)
  assert.equal(resets, 1)
  await render(<SettingsPageFormActions {...props} isSaving />)
  assert.equal(ref.current === button, true)
  assert.equal((button as HTMLButtonElement).disabled, true)
  assert.equal(
    (
      within(actions).getByRole('button', {
        name: 'Reset',
      }) as HTMLButtonElement
    ).disabled,
    true
  )
})
