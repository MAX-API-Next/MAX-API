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
import { act, useEffect } from 'react'
import type { Root } from 'react-dom/client'
import { createInstance } from 'i18next'
import { JSDOM } from 'jsdom'
import assert from 'node:assert/strict'
import { after, afterEach, beforeEach, test } from 'node:test'
import { renderToString } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import {
  SecureVerificationContext,
  type SecureVerificationContextValue,
} from '../lib/verification-context'
import type { StartVerificationOptions } from '../types'
import { useApiTokenVerification } from './use-api-token-verification'

type VerifyCall = ReturnType<typeof useApiTokenVerification>
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
})
const i18n = createInstance()
const previousGlobals = new Map<string, PropertyDescriptor | undefined>()
for (const [key, value] of Object.entries({
  window: dom.window,
  document: dom.window.document,
  navigator: dom.window.navigator,
  IS_REACT_ACT_ENVIRONMENT: true,
})) {
  previousGlobals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
  Object.defineProperty(globalThis, key, {
    configurable: true,
    writable: true,
    value,
  })
}
await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })
const { createRoot } = await import('react-dom/client')
let container: HTMLDivElement
let root: Root

function GateProbe(props: { capture: (call: VerifyCall) => void }) {
  const verifyCall = useApiTokenVerification()
  const capture = props.capture
  useEffect(() => capture(verifyCall), [capture, verifyCall])
  return null
}

beforeEach((): void => {
  container = dom.window.document.createElement('div')
  dom.window.document.body.append(container)
  root = createRoot(container)
})
afterEach(async (): Promise<void> => {
  await act(async (): Promise<void> => root.unmount())
  container.remove()
})
after((): void => {
  for (const [key, descriptor] of [...previousGlobals].reverse()) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
  dom.window.close()
})

async function renderGate(
  value: SecureVerificationContextValue,
  capture: (call: VerifyCall) => void
): Promise<void> {
  await act(async (): Promise<void> => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <SecureVerificationContext.Provider value={value}>
          <GateProbe capture={capture} />
        </SecureVerificationContext.Provider>
      </I18nextProvider>
    )
  })
}

test('requires the verification provider instead of silently bypassing the gate', (): void => {
  assert.throws(
    () =>
      renderToString(
        <I18nextProvider i18n={i18n}>
          <GateProbe capture={() => undefined} />
        </I18nextProvider>
      ),
    /useSecureVerificationGate must be used within SecureVerificationProvider/
  )
})

test('keeps callback identity for the same gate method and uses a replacement method', async (): Promise<void> => {
  const captured: VerifyCall[] = []
  const capture = (call: VerifyCall): void => {
    captured.push(call)
  }
  const options: Array<StartVerificationOptions | undefined> = []
  let apiCalls = 0
  const withVerification: SecureVerificationContextValue['withVerification'] =
    async (apiCall, config) => {
      options.push(config)
      return apiCall()
    }
  await renderGate({ withVerification }, capture)
  const first = captured.at(-1)
  assert.ok(first)
  await renderGate({ withVerification }, capture)
  assert.equal(captured.at(-1), first)
  assert.equal(
    await first(async () => {
      apiCalls++
      return 'allowed'
    }),
    'allowed'
  )
  assert.equal(apiCalls, 1)
  assert.deepEqual(options, [
    {
      scope: 'api_token',
      title: 'Security verification',
      description:
        'Confirm your identity before sending an API key to a chat client.',
    },
  ])

  const replacement: SecureVerificationContextValue['withVerification'] =
    async () => null
  await renderGate({ withVerification: replacement }, capture)
  const next = captured.at(-1)
  assert.ok(next && next !== first)
  assert.equal(
    await next(async () => {
      apiCalls++
      return 'unexpected'
    }),
    null
  )
  assert.equal(apiCalls, 1, 'cancellation must not run the protected API call')
})

test('propagates a gate rejection without retrying the protected operation', async (): Promise<void> => {
  const failure = new Error('verification rejected')
  let gateCalls = 0
  let apiCalls = 0
  let verifyCall: VerifyCall | undefined
  await renderGate(
    {
      withVerification: async () => {
        gateCalls++
        throw failure
      },
    },
    (call) => {
      verifyCall = call
    }
  )
  assert.ok(verifyCall)
  await assert.rejects(
    verifyCall(async () => {
      apiCalls++
      return 'unexpected'
    }),
    (error) => error === failure
  )
  assert.equal(gateCalls, 1)
  assert.equal(apiCalls, 0)
})
