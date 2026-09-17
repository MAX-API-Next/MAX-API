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
import { act, useState } from 'react'
import type { Root } from 'react-dom/client'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import {
  BILLING_EXTRA_VARS,
  parseTiersFromExpr,
} from '@/features/pricing/lib/billing-expr'
import type { ModelRatioData } from './model-pricing-sheet'

// Load DOM-dependent modules after installing JSDOM so Base UI selects use
// their browser lifecycle, including option registration and keyboard input.
let createRoot: typeof import('react-dom/client').createRoot
let fireEvent: typeof import('@testing-library/react').fireEvent
let within: typeof import('@testing-library/react').within
let ModelPricingEditorPanel: typeof import('./model-pricing-sheet').ModelPricingEditorPanel
let ModelRatioVisualEditor: typeof import('./model-ratio-visual-editor').ModelRatioVisualEditor
let TieredPricingEditor: typeof import('./tiered-pricing-editor').TieredPricingEditor

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

describe('optional zero prices in the actual editor', () => {
  for (const source of [
    'tier("base", p * 3.0 + c * 15.0 + cr * 0.0)',
    'tier("base", p * 3e0 + c * 1.5e1 + cr * 0e0)',
    'tier("custom", max(p, 1) * 3 + c * 15)',
  ]) {
    test(`preserves prices on mount and mode changes: ${source}`, async () => {
      const container = createContainer()
      const root = createRoot(container)
      const supported = !source.includes('max(')
      const expected = supported
        ? 'tier("base", p * 3 + c * 15 + cr * 0)'
        : source
      let saved = source
      function ControlledEditor() {
        const [expression, setExpression] = useState(source)
        return (
          <TieredPricingEditor
            billingExpr={expression}
            requestRuleExpr=''
            onBillingExprChange={(next) => {
              saved = next
              setExpression(next)
            }}
            onRequestRuleExprChange={() => undefined}
          />
        )
      }
      const selectMode = async (name: string) => {
        await act(async () => {
          fireEvent.keyDown(within(container).getAllByRole('combobox')[0], {
            key: 'ArrowDown',
          })
        })
        const option = await within(dom.window.document.body).findByRole(
          'option',
          { name }
        )
        await act(async () => {
          fireEvent.pointerDown(option, { pointerType: 'mouse' })
          fireEvent.click(option)
        })
      }
      try {
        await act(async () =>
          root.render(
            <I18nextProvider i18n={i18n}>
              <ControlledEditor />
            </I18nextProvider>
          )
        )
        assert.equal(saved, expected)
        if (supported) await selectMode('Expression editor')
        await selectMode('Visual editor')
        assert.equal(saved, expected)
        if (supported) {
          assert.equal(getInputValueByLabel(container, 'Input price'), '3')
          assert.equal(getInputValueByLabel(container, 'Output price'), '15')
          assert.equal(getInputValueByLabel(container, 'Cache read price'), '0')
        } else {
          assert.equal(
            (
              within(container).getByPlaceholderText(
                'tier("base", p * 3 + c * 15)'
              ) as HTMLTextAreaElement
            ).value,
            source
          )
        }
      } finally {
        await unmount(root, container)
      }
    })
  }

  test('keeps zero prices when switching between visual and expression modes', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const source =
      'tier("base", p * 3 + c * 15 + cr * 0 + cc1h * 0 + img_o * 0)'
    const changes: string[] = []
    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              billingExpr={source}
              requestRuleExpr=''
              onBillingExprChange={(next) => changes.push(next)}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      })
      const editor = within(container)
      const page = within(dom.window.document.body)
      await act(async () => {
        fireEvent.keyDown(editor.getAllByRole('combobox')[0], {
          key: 'ArrowDown',
        })
      })
      await act(async () => {
        const option = await page.findByRole('option', {
          name: 'Expression editor',
        })
        fireEvent.pointerDown(option, { pointerType: 'mouse' })
        fireEvent.click(option)
      })
      const expression = editor.getByPlaceholderText(
        'tier("base", p * 3 + c * 15)'
      ) as HTMLTextAreaElement
      assert.equal(expression.value, source)
      await act(async () => {
        fireEvent.keyDown(editor.getAllByRole('combobox')[0], {
          key: 'ArrowDown',
        })
      })
      await act(async () => {
        const option = await page.findByRole('option', {
          name: 'Visual editor',
        })
        fireEvent.pointerDown(option, { pointerType: 'mouse' })
        fireEvent.click(option)
      })
      assert.equal(getInputValueByLabel(container, 'Cache read price'), '0')
      assert.equal(
        getInputValueByLabel(container, 'Cache create (1h) price'),
        '0'
      )
      assert.equal(getInputValueByLabel(container, 'Image output price'), '0')
      assert.ok(changes.every((value) => value === source))
    } finally {
      await unmount(root, container)
    }
  })

  for (const variable of BILLING_EXTRA_VARS) {
    test(`keeps ${variable.key}=0 on mount, unrelated edit and remount`, async () => {
      const container = createContainer()
      const root = createRoot(container)
      let saved = `tier("base", p * 3 + c * 15 + ${variable.key} * 0)`
      const renderEditor = (key: string) => (
        <I18nextProvider i18n={i18n}>
          <TieredPricingEditor
            key={key}
            billingExpr={saved}
            requestRuleExpr=''
            onBillingExprChange={(next) => {
              saved = next
            }}
            onRequestRuleExprChange={() => undefined}
          />
        </I18nextProvider>
      )
      try {
        await act(async () => root.render(renderEditor('first')))
        assert.ok(saved.includes(`${variable.key} * 0`))
        assert.equal(getInputValueByLabel(container, variable.label), '0')
        const input = within(container).getByLabelText(
          'Input price'
        ) as HTMLInputElement
        await act(async () => {
          fireEvent.blur(input, { target: { value: '4' } })
        })
        assert.ok(saved.includes('p * 4'))
        assert.ok(saved.includes(`${variable.key} * 0`))
        await act(async () => root.render(renderEditor('remount')))
        assert.equal(getInputValueByLabel(container, variable.label), '0')
        assert.ok(saved.includes(`${variable.key} * 0`))
      } finally {
        await unmount(root, container)
      }
    })
  }

  test('distinguishes blank, zero and positive prices and preserves them when adding a tier', async () => {
    const container = createContainer()
    const root = createRoot(container)
    let saved = 'tier("base", p * 3 + c * 15)'
    function ControlledEditor() {
      const [expression, setExpression] = useState(saved)
      return (
        <TieredPricingEditor
          billingExpr={expression}
          requestRuleExpr=''
          onBillingExprChange={(next) => {
            saved = next
            setExpression(next)
          }}
          onRequestRuleExprChange={() => undefined}
        />
      )
    }
    try {
      await act(async () =>
        root.render(
          <I18nextProvider i18n={i18n}>
            <ControlledEditor />
          </I18nextProvider>
        )
      )
      const editor = within(container)
      const cache = editor.getByLabelText(
        'Cache read price'
      ) as HTMLInputElement
      assert.equal(cache.value, '')
      await act(async () => {
        fireEvent.blur(cache, { target: { value: '0' } })
      })
      assert.ok(saved.includes('cr * 0'))
      await act(async () => {
        fireEvent.blur(cache, { target: { value: '0.25' } })
      })
      assert.ok(saved.includes('cr * 0.25'))
      await act(async () => {
        fireEvent.blur(cache, { target: { value: '' } })
      })
      assert.equal(cache.value, '')
      assert.ok(!saved.includes('cr *'))
      await act(async () => {
        fireEvent.blur(cache, { target: { value: '0' } })
      })
      await act(async () => {
        fireEvent.click(editor.getByRole('button', { name: 'Add tier' }))
      })
      assert.equal((saved.match(/cr \* 0/g) || []).length, 2)
      for (const name of ['cc', 'cc1h', 'img', 'img_o', 'ai', 'ao']) {
        assert.ok(
          !saved.includes(`${name} *`),
          `unexpected default price ${name}`
        )
      }
    } finally {
      await unmount(root, container)
    }
  })
})

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
  ;({ createRoot } = await import('react-dom/client'))
  ;({ fireEvent, within } = await import('@testing-library/react/pure'))
  ;({ ModelPricingEditorPanel } = await import('./model-pricing-sheet'))
  ;({ ModelRatioVisualEditor } = await import('./model-ratio-visual-editor'))
  ;({ TieredPricingEditor } = await import('./tiered-pricing-editor'))
})

after(() => {
  restoreGlobals()
  dom.window.close()
})

describe('TieredPricingEditor runtime behavior', () => {
  test('keeps an empty timezone editable and applies the default only to the expression', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const source =
      'hour("Asia/Shanghai") >= 9 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    let saved = source
    try {
      await act(async () =>
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              billingExpr={source}
              requestRuleExpr=''
              onBillingExprChange={(next) => {
                saved = next
              }}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      )
      const input = within(container).getByLabelText(
        'Timezone'
      ) as HTMLInputElement
      await act(async () => fireEvent.change(input, { target: { value: '' } }))
      assert.equal(input.value, '')
      assert.match(saved, /hour\("Asia\/Shanghai"\)/)
      await act(async () =>
        fireEvent.change(input, { target: { value: 'UTC' } })
      )
      assert.equal(input.value, 'UTC')
      assert.match(saved, /hour\("UTC"\)/)
    } finally {
      await unmount(root, container)
    }
  })

  test('clears OR groups when deleting the final sibling and can add a fresh tier', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const source =
      '(hour("UTC") >= 9 && hour("UTC") < 17) || (len < 100) ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    let saved = source
    try {
      await act(async () =>
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              billingExpr={source}
              requestRuleExpr=''
              onBillingExprChange={(next) => {
                saved = next
              }}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      )
      const editor = within(container)
      assert.equal(editor.getAllByLabelText('Condition Value').length, 3)
      await act(async () =>
        fireEvent.click(
          editor.getAllByRole('button', { name: 'Remove tier' })[1]
        )
      )
      assert.equal(editor.queryAllByLabelText('Condition Value').length, 0)
      assert.equal(saved, 'tier("peak", p * 2 + c * 4)')
      await act(async () =>
        fireEvent.click(editor.getByRole('button', { name: 'Add tier' }))
      )
      assert.equal(editor.getAllByLabelText('Condition Value').length, 1)
      assert.deepEqual(parseTiersFromExpr(saved)[0].conditions, [
        { var: 'len', op: '<', value: 200000 },
      ])
    } finally {
      await unmount(root, container)
    }
  })

  test('chooses a new condition using only the target OR group', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const source =
      'len < 100 ? tier("peak", p * 2 + c * 4) : tier("off", p * 1 + c * 2)'
    let saved = source
    try {
      await act(async () =>
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              billingExpr={source}
              requestRuleExpr=''
              onBillingExprChange={(next) => {
                saved = next
              }}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      )
      const editor = within(container)
      await act(async () =>
        fireEvent.click(
          editor.getAllByRole('button', { name: 'Add condition group' })[0]
        )
      )
      await act(async () =>
        fireEvent.click(
          editor.getAllByRole('button', { name: 'Add condition' })[2]
        )
      )
      assert.deepEqual(
        parseTiersFromExpr(saved)[0].conditionGroups?.map((group) =>
          group.map((condition) => condition.var)
        ),
        [['len'], ['hour', 'len']]
      )
    } finally {
      await unmount(root, container)
    }
  })

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

  test('allows adding a condition to the initial tier and exposes time fields', async () => {
    const container = createContainer()
    const root = createRoot(container)
    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              modelName='new-model'
              billingExpr='tier("base", p * 1 + c * 2)'
              requestRuleExpr=''
              onBillingExprChange={() => undefined}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      })
      const editor = within(container)
      const addCondition = editor.getAllByRole('button', {
        name: 'Add condition',
      })[0]
      assert.equal((addCondition as HTMLButtonElement).disabled, false)
      fireEvent.click(addCondition)
      assert.ok(editor.getByLabelText('Condition Value'))
      assert.match(container.textContent || '', /Full input length/)

      assert.ok(editor.getAllByRole('button', { name: 'Add condition' }).length)
    } finally {
      await unmount(root, container)
    }
  })

  test('allows adding more than two conditions and condition groups', async () => {
    const container = createContainer()
    const root = createRoot(container)
    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              modelName='conditions-model'
              billingExpr='tier("base", p * 1 + c * 2)'
              requestRuleExpr=''
              onBillingExprChange={() => undefined}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      })
      const editor = within(container)
      const addCondition = editor.getAllByRole('button', {
        name: 'Add condition',
      })[0]
      fireEvent.click(addCondition)
      fireEvent.click(addCondition)
      fireEvent.click(addCondition)
      assert.equal(editor.getAllByLabelText('Condition Value').length, 3)

      fireEvent.click(
        editor.getByRole('button', { name: 'Add condition group' })
      )
      assert.equal(editor.getAllByText(/Condition group/).length, 2)
      fireEvent.click(
        editor.getAllByRole('button', { name: 'Remove condition group' })[1]
      )
      assert.equal(editor.getAllByText(/Condition group/).length, 1)
    } finally {
      await unmount(root, container)
    }
  })

  test('removes a condition from a non-fallback tier', async () => {
    const container = createContainer()
    const root = createRoot(container)
    const billingChanges: string[] = []
    try {
      await act(async () => {
        root.render(
          <I18nextProvider i18n={i18n}>
            <TieredPricingEditor
              modelName='multi-tier-model'
              billingExpr={
                'len < 200000 ? tier("short", p * 1 + c * 2) : tier("long", p * 3 + c * 4)'
              }
              requestRuleExpr=''
              onBillingExprChange={(next) => billingChanges.push(next)}
              onRequestRuleExprChange={() => undefined}
            />
          </I18nextProvider>
        )
      })

      const editor = within(container)
      const removeButtons = editor.getAllByRole('button', {
        name: 'Remove condition',
      })
      assert.equal(removeButtons.length, 1)
      fireEvent.click(removeButtons[0])
      assert.equal(
        editor.queryByRole('button', { name: 'Remove condition' }),
        null
      )
      await act(async () => undefined)
      assert.ok(
        billingChanges.some((expr) =>
          expr.startsWith('true ? tier("short", p * 1 + c * 2)')
        )
      )
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
