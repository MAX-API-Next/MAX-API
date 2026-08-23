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
import { act, type ReactElement } from 'react'
import { createRoot } from 'react-dom/client'
import { createInstance, type Resource } from 'i18next'
import { JSDOM } from 'jsdom'
import { I18nextProvider } from 'react-i18next'

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

type GlobalKey = (typeof globalKeys)[number]

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

type AnimationFrameTimer = ReturnType<typeof setTimeout>
type TestWindow = typeof globalThis.window

const animationFrameHandles = new Map<AnimationFrameTimer, TestWindow>()

function createTestAnimationFrameScheduler(
  window: TestWindow
): (callback: FrameRequestCallback) => number {
  return (callback: FrameRequestCallback): number => {
    if (globalThis.window !== window) return 0
    const handle = setTimeout(() => {
      animationFrameHandles.delete(handle)
      if (globalThis.window !== window) return
      callback(Date.now())
    }, 0)
    animationFrameHandles.set(handle, window)
    return handle as unknown as number
  }
}

function createTestAnimationFrameCanceller(
  window: TestWindow
): (handle: number) => void {
  return (handle: number): void => {
    const timerHandle = handle as unknown as AnimationFrameTimer
    if (animationFrameHandles.get(timerHandle) !== window) return
    clearTimeout(timerHandle)
    animationFrameHandles.delete(timerHandle)
  }
}

function cancelPendingAnimationFrames(window: TestWindow) {
  for (const [handle, scheduledWindow] of animationFrameHandles) {
    if (scheduledWindow !== window) continue
    clearTimeout(handle)
    animationFrameHandles.delete(handle)
  }
}

export function createReactTestEnvironment(options?: {
  lng?: string
  fallbackLng?: string
  resources?: Resource
  url?: string
}) {
  const lng = options?.lng ?? 'en'
  const fallbackLng = options?.fallbackLng ?? lng
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: options?.url ?? 'http://localhost/',
  })
  const i18n = createInstance()
  const previousDescriptors = new Map<
    GlobalKey,
    PropertyDescriptor | undefined
  >()
  let installedWindow: TestWindow | undefined

  function setGlobal(key: GlobalKey, value: unknown) {
    previousDescriptors.set(
      key,
      Object.getOwnPropertyDescriptor(globalThis, key)
    )
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

  return {
    dom,
    i18n,
    async setup() {
      await i18n.init({
        lng,
        fallbackLng,
        resources: options?.resources ?? {
          [lng]: { translation: {} },
        },
        interpolation: { escapeValue: false },
      })

      const window = dom.window
      installedWindow = window as unknown as TestWindow
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
      setGlobal(
        'requestAnimationFrame',
        createTestAnimationFrameScheduler(installedWindow)
      )
      setGlobal(
        'cancelAnimationFrame',
        createTestAnimationFrameCanceller(installedWindow)
      )
      setGlobal('ResizeObserver', ResizeObserverStub)
      setGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    },
    teardown() {
      if (installedWindow) {
        cancelPendingAnimationFrames(installedWindow)
      }
      restoreGlobals()
      dom.window.close()
    },
    async render(element: ReactElement) {
      const container = dom.window.document.createElement('div')
      dom.window.document.body.append(container)
      const root = createRoot(container)

      await act(async () => {
        root.render(<I18nextProvider i18n={i18n}>{element}</I18nextProvider>)
      })

      return {
        container,
        async click(target: HTMLElement) {
          await act(async () => {
            target.click()
          })
        },
        async unmount() {
          await act(async () => root.unmount())
          container.remove()
        },
      }
    },
  }
}
