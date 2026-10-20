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
import { act, useState, type ReactElement } from 'react'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, afterEach, before, describe, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import { MAX_MANUAL_ROUTING_GROUPS } from '../lib/api-key-form'
import type { TokenRoutingMode } from '../types'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

let ApiKeyRoutingEditor: typeof import('./api-key-routing-editor').ApiKeyRoutingEditor
let cleanup: typeof import('@testing-library/react/pure').cleanup
let fireEvent: typeof import('@testing-library/react/pure').fireEvent
let isInaccessible: typeof import('@testing-library/react/pure').isInaccessible
let render: typeof import('@testing-library/react/pure').render
let within: typeof import('@testing-library/react/pure').within

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const i18n = createInstance()
const previousGlobals = new Map<string, PropertyDescriptor | undefined>()

before(async (): Promise<void> => {
  await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })
  const globals = {
    window: dom.window,
    document: dom.window.document,
    navigator: dom.window.navigator,
    HTMLElement: dom.window.HTMLElement,
    Element: dom.window.Element,
    Node: dom.window.Node,
    Event: dom.window.Event,
    MouseEvent: dom.window.MouseEvent,
    MutationObserver: dom.window.MutationObserver,
    getComputedStyle: dom.window.getComputedStyle.bind(dom.window),
    requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
    cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
    ResizeObserver: class {
      observe(): void {}
      unobserve(): void {}
      disconnect(): void {}
    },
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
  dom.window.HTMLElement.prototype.scrollIntoView = (): void => undefined
  Object.defineProperty(dom.window, 'matchMedia', {
    value: (): Pick<
      MediaQueryList,
      | 'matches'
      | 'addListener'
      | 'removeListener'
      | 'addEventListener'
      | 'removeEventListener'
    > => ({
      matches: false,
      addListener(): void {},
      removeListener(): void {},
      addEventListener(): void {},
      removeEventListener(): void {},
    }),
  })
  // React and Base UI must detect the browser before mounting interactive portals.
  ;({ cleanup, fireEvent, isInaccessible, render, within } =
    await import('@testing-library/react/pure'))
  ;({ ApiKeyRoutingEditor } = await import('./api-key-routing-editor'))
})

afterEach(async (): Promise<void> => {
  await act(async (): Promise<void> => cleanup())
})

after((): void => {
  dom.window.close()
  for (const [key, descriptor] of previousGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
})

const groups = Array.from(
  { length: MAX_MANUAL_ROUTING_GROUPS },
  (_: unknown, index: number): string => `group-${index + 1}`
)

const availableGroups = Array.from(
  { length: 14 },
  (_: unknown, index: number): string => `group-${index + 1}`
)

type RoutingHarnessProps = {
  initialGroups: string[]
}

function RoutingHarness(props: RoutingHarnessProps): ReactElement {
  const [mode, setMode] = useState<TokenRoutingMode>('smart')
  const [manualGroups, setManualGroups] = useState(props.initialGroups)
  return (
    <I18nextProvider i18n={i18n}>
      <ApiKeyRoutingEditor
        mode={mode}
        route='auto'
        manualGroups={manualGroups}
        retryOnFailure
        autoRouteOptions={[
          { value: 'auto', label: 'Automatic', groups: availableGroups },
        ]}
        realGroupOptions={availableGroups.map(
          (group: string): ApiKeyGroupOption => ({
            value: group,
            label: group,
            ratio: 1,
          })
        )}
        onModeChange={setMode}
        onRouteChange={(): void => undefined}
        onManualGroupsChange={setManualGroups}
        onRetryOnFailureChange={(): void => undefined}
      />
    </I18nextProvider>
  )
}

async function findRoutingOptions(
  view: ReturnType<typeof render>
): Promise<Map<string, HTMLElement>> {
  // Compute each visible option's accessible name once instead of rescanning
  // every option for each group. Keep the same role, visibility and name checks.
  const names = new Map<Element, string>()
  const options = await view.findAllByRole('option', {
    name: (name: string, element: Element): boolean => {
      names.set(element, name)
      return true
    },
  })
  const result = new Map<string, HTMLElement>()
  for (const option of options) {
    const name = names.get(option)
    assert.ok(name !== undefined)
    assert.equal(result.has(name), false, `duplicate option name: ${name}`)
    result.set(name, option)
  }
  return result
}

function routingOption(
  options: Map<string, HTMLElement>,
  name: string
): HTMLElement {
  const option = options.get(name)
  assert.ok(option, `missing accessible routing option: ${name}`)
  return option
}

describe('ApiKeyRoutingEditor', (): void => {
  for (const initialGroups of [[], groups]) {
    test(`clears ${initialGroups.length} stored manual selections when automatic routing is disabled`, async (): Promise<void> => {
      const view = render(<RoutingHarness initialGroups={initialGroups} />)
      view.getByText('+11')
      fireEvent.click(
        view.getByRole('button', { name: 'Disable and select groups' })
      )

      view.getByText('0 / 8 groups selected')
      view.getByText('Select at least one manual routing group')
      assert.equal(
        view.queryAllByRole('button', { name: /^Remove group-/ }).length,
        0
      )

      fireEvent.click(view.getByRole('combobox', { name: 'Select groups' }))
      const options = await findRoutingOptions(view)
      assert.equal(options.size, availableGroups.length)
      for (const group of availableGroups) {
        const option = routingOption(options, `${group}1x Ratio`)
        assert.notEqual(option.getAttribute('aria-disabled'), 'true')
      }
      fireEvent.click(routingOption(options, 'group-141x Ratio'))
      within(view.getByRole('combobox', { name: 'Select groups' })).getByText(
        'group-14'
      )
      fireEvent.click(view.getByRole('combobox', { name: 'Select groups' }))

      fireEvent.click(
        view.getByRole('button', { name: 'Enable automatic routing' })
      )
      fireEvent.click(
        view.getByRole('button', { name: 'Disable and select groups' })
      )
      view.getByText('0 / 8 groups selected')
    })
  }

  test('keeps all available groups visible at the manual selection limit', async (): Promise<void> => {
    let changedGroups: string[] | undefined
    const view = render(
      <I18nextProvider i18n={i18n}>
        <ApiKeyRoutingEditor
          mode='manual'
          route='auto'
          manualGroups={groups}
          retryOnFailure
          autoRouteOptions={[]}
          realGroupOptions={availableGroups.map(
            (group: string): ApiKeyGroupOption => ({
              value: group,
              label: group,
              desc: group,
              ratio: 1,
            })
          )}
          onModeChange={(): void => undefined}
          onRouteChange={(): void => undefined}
          onManualGroupsChange={(nextGroups: string[]): void => {
            changedGroups = nextGroups
          }}
          onRetryOnFailureChange={(): void => undefined}
        />
      </I18nextProvider>
    )

    const trigger = view.getByRole('combobox', { name: 'Select groups' })
    assert.equal(trigger.getAttribute('aria-expanded'), 'false')
    for (const group of groups) {
      within(trigger).getByText(group)
    }

    fireEvent.click(trigger)

    assert.equal(trigger.getAttribute('aria-expanded'), 'true')
    const options = await findRoutingOptions(view)
    assert.equal(options.size, availableGroups.length)
    for (const group of availableGroups) {
      const option = routingOption(options, `${group}${group}1x Ratio`)
      assert.equal(
        option.getAttribute('aria-disabled'),
        String(!groups.includes(group))
      )
    }
    fireEvent.click(routingOption(options, 'group-14group-141x Ratio'))
    assert.equal(changedGroups, undefined)

    const removeButtons = view.getAllByLabelText(/^Remove group-/)
    assert.equal(removeButtons.length, MAX_MANUAL_ROUTING_GROUPS)
    for (const removeButton of removeButtons) {
      assert.equal(removeButton.tagName, 'BUTTON')
      assert.ok(
        removeButton.getAttribute('role') === null ||
          removeButton.getAttribute('role') === 'button'
      )
      assert.equal(removeButton.getAttribute('aria-labelledby'), null)
      assert.equal(isInaccessible(removeButton), false)
      assert.equal(trigger.contains(removeButton), false)
    }

    fireEvent.click(removeButtons[0])
    assert.deepEqual(changedGroups, groups.slice(1))
  })
})
