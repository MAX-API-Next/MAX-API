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
import { fireEvent, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import {
  ModelPricingEditorPanel,
  type ModelRatioData,
} from './model-pricing-sheet'
import { ModelRatioVisualEditor } from './model-ratio-visual-editor'
import { TieredPricingEditor } from './tiered-pricing-editor'

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
  'localStorage',
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

function getInputValueByLabel(container: HTMLElement, labelText: string) {
  const label = Array.from(container.querySelectorAll('label')).find(
    (candidate) => candidate.textContent === labelText
  )
  assert.ok(label, `missing ${labelText} label`)

  const inputId = label.getAttribute('for')
  assert.ok(inputId, `missing input id for ${labelText}`)

  const input = dom.window.document.getElementById(inputId) as HTMLInputElement
  assert.ok(input, `missing input for ${labelText}`)
  return input.value
}

const modelA: ModelRatioData = {
  name: 'model-a',
  billingMode: 'tiered_expr',
  billingExpr: 'tier("a", p * 1 + c * 2)',
  requestRuleExpr: '',
}
const modelB: ModelRatioData = {
  name: 'model-b',
  billingMode: 'tiered_expr',
  billingExpr: 'tier("b", p * 3 + c * 4)',
  requestRuleExpr: '',
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
  setGlobal('localStorage', window.localStorage)
  setGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

after(() => {
  restoreGlobals()
  dom.window.close()
})

describe('TieredPricingEditor runtime behavior', () => {
  test('mounts the visual editor without a runtime hook error', async () => {
    const container = createContainer()
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              modelName={modelA.name}
              billingExpr={modelA.billingExpr || ''}
              requestRuleExpr=''
              onBillingExprChange={() => undefined}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      })

      assert.match(container.textContent || '', /Tier 1 \/ 1/)
    } finally {
      await unmount(root, container)
    }
  })

  test('removes a newly added fallback tier', async () => {
    const container = createContainer()
    const root = createRoot(container)

    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              modelName={modelA.name}
              billingExpr={modelA.billingExpr || ''}
              requestRuleExpr=''
              onBillingExprChange={() => undefined}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      })

      const editor = within(container)
      const initialRemoveButton = editor.getByRole('button', {
        name: 'Remove tier',
      }) as HTMLButtonElement
      assert.equal(initialRemoveButton.disabled, true)

      fireEvent.click(editor.getByRole('button', { name: 'Add tier' }))
      editor.getByText('Tier 2 / 2')

      const removeTierButtons = editor.getAllByRole('button', {
        name: 'Remove tier',
      }) as HTMLButtonElement[]
      assert.equal(removeTierButtons.length, 2)

      const newTierRemoveButton = removeTierButtons[1]
      assert.equal(newTierRemoveButton.disabled, false)
      fireEvent.click(newTierRemoveButton)

      editor.getByText('Tier 1 / 1')
      assert.equal(editor.queryByText('Tier 2 / 2'), null)
      editor.getByText('Fallback tier')
      editor.getByText('Always matches (default tier).')
    } finally {
      await unmount(root, container)
    }
  })

  test('never emits model A billing through model B callbacks', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const changes: Array<{ modelName: string; next: string }> = []
    const renderEditor = (model: ModelRatioData) => (
      <I18nextProvider i18n={i18n}>
        <TieredPricingEditor
          modelName={model.name}
          billingExpr={model.billingExpr || ''}
          requestRuleExpr=''
          onBillingExprChange={(next) =>
            changes.push({ modelName: model.name, next })
          }
          onRequestRuleExprChange={() => undefined}
        />
      </I18nextProvider>
    )

    try {
      await act(async () => root.render(renderEditor(modelA)))
      await act(async () => root.render(renderEditor(modelB)))

      assert.equal(
        changes.some(
          ({ modelName, next }) =>
            modelName === modelB.name && next === modelA.billingExpr
        ),
        false
      )
    } finally {
      await unmount(root, container)
    }
  })

  test('does not carry model A billing state into model B', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const renderPanel = (editData: ModelRatioData) => (
      <I18nextProvider i18n={i18n}>
        <ModelPricingEditorPanel
          editData={editData}
          onSave={() => undefined}
          onCancel={() => undefined}
        />
      </I18nextProvider>
    )

    try {
      await act(async () => root.render(renderPanel(modelA)))
      assert.match(container.textContent || '', /tier\("a", p \* 1 \+ c \* 2\)/)
      assert.equal(getInputValueByLabel(container, 'Input price'), '1')
      assert.equal(getInputValueByLabel(container, 'Output price'), '2')

      await act(async () => root.render(renderPanel(modelB)))

      const text = container.textContent || ''
      assert.match(text, /tier\("b", p \* 3 \+ c \* 4\)/)
      assert.doesNotMatch(text, /tier\("a", p \* 1 \+ c \* 2\)/)
      assert.doesNotMatch(text, /tier\("base", p \* 0 \+ c \* 0\)/)
      assert.equal(getInputValueByLabel(container, 'Input price'), '3')
      assert.equal(getInputValueByLabel(container, 'Output price'), '4')
    } finally {
      await unmount(root, container)
    }
  })

  test('replaces expression state when the selected model snapshot changes', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const updatedModelA: ModelRatioData = {
      ...modelA,
      billingExpr: 'tier("a-updated", p * 5 + c * 6)',
    }
    const renderPanel = (editData: ModelRatioData) => (
      <I18nextProvider i18n={i18n}>
        <ModelPricingEditorPanel
          editData={editData}
          onSave={() => undefined}
          onCancel={() => undefined}
        />
      </I18nextProvider>
    )

    try {
      await act(async () => root.render(renderPanel(modelA)))
      assert.equal(getInputValueByLabel(container, 'Input price'), '1')
      assert.equal(getInputValueByLabel(container, 'Output price'), '2')

      await act(async () => root.render(renderPanel(updatedModelA)))
      assert.equal(getInputValueByLabel(container, 'Input price'), '5')
      assert.equal(getInputValueByLabel(container, 'Output price'), '6')
    } finally {
      await unmount(root, container)
    }
  })

  test('updates expression prices when selecting another model row', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const modeMap = JSON.stringify({
      [modelA.name]: 'tiered_expr',
      [modelB.name]: 'tiered_expr',
    })
    const exprMap = JSON.stringify({
      [modelA.name]: modelA.billingExpr,
      [modelB.name]: modelB.billingExpr,
    })
    const renderEditor = () => (
      <I18nextProvider i18n={i18n}>
        <ModelRatioVisualEditor
          savedModelPrice='{}'
          savedModelRatio='{}'
          savedCacheRatio='{}'
          savedCreateCacheRatio='{}'
          savedCompletionRatio='{}'
          savedImageRatio='{}'
          savedAudioRatio='{}'
          savedAudioCompletionRatio='{}'
          savedBillingMode={modeMap}
          savedBillingExpr={exprMap}
          modelPrice='{}'
          modelRatio='{}'
          cacheRatio='{}'
          createCacheRatio='{}'
          completionRatio='{}'
          imageRatio='{}'
          audioRatio='{}'
          audioCompletionRatio='{}'
          billingMode={modeMap}
          billingExpr={exprMap}
          onChange={() => undefined}
        />
      </I18nextProvider>
    )
    const selectRow = async (modelName: string) => {
      const cell = Array.from(container.querySelectorAll('td')).find(
        (candidate) => candidate.textContent?.trim().startsWith(modelName)
      )
      assert.ok(cell, `missing ${modelName} table cell`)
      const row = cell.closest('tr')
      assert.ok(row, `missing ${modelName} table row`)
      await act(async () =>
        row.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      )
    }

    try {
      await act(async () => root.render(renderEditor()))

      await selectRow(modelA.name)
      assert.equal(getInputValueByLabel(container, 'Input price'), '1')
      assert.equal(getInputValueByLabel(container, 'Output price'), '2')

      await selectRow(modelB.name)
      assert.equal(getInputValueByLabel(container, 'Input price'), '3')
      assert.equal(getInputValueByLabel(container, 'Output price'), '4')
    } finally {
      await unmount(root, container)
    }
  })
})
