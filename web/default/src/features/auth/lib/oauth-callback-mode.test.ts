import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'

import { resolveOAuthCallbackMode } from './oauth-callback-mode'

const originalLocalStorageDescriptor = Object.getOwnPropertyDescriptor(
  globalThis,
  'localStorage'
)

function setLocalStorage(value: Storage): void {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value,
  })
}

function fakeStorage(initial: Record<string, string> = {}): Storage {
  const data = new Map(Object.entries(initial))
  return {
    get length() {
      return data.size
    },
    clear: () => data.clear(),
    getItem: (key: string) => data.get(key) ?? null,
    key: (index: number) => Array.from(data.keys())[index] ?? null,
    removeItem: (key: string) => void data.delete(key),
    setItem: (key: string, value: string) => void data.set(key, value),
  }
}

afterEach(() => {
  if (originalLocalStorageDescriptor) {
    Object.defineProperty(
      globalThis,
      'localStorage',
      originalLocalStorageDescriptor
    )
  } else {
    Reflect.deleteProperty(globalThis, 'localStorage')
  }
})

describe('resolveOAuthCallbackMode', () => {
  test('matching bind state is treated as a bind flow', () => {
    const state = 'bind-state'
    setLocalStorage(
      fakeStorage({
        [`oauth:binding:state:github:${state}`]: String(Date.now()),
      })
    )

    assert.equal(resolveOAuthCallbackMode('github', state), 'bind')
  })

  test('callback without a local bind marker stays a login flow', () => {
    setLocalStorage(fakeStorage())

    assert.equal(
      resolveOAuthCallbackMode('github', 'foreign-opener-state'),
      'login'
    )
  })

  test('bind marker for another provider stays a login flow', () => {
    const state = 'bind-state'
    setLocalStorage(
      fakeStorage({
        [`oauth:binding:state:github:${state}`]: String(Date.now()),
      })
    )

    assert.equal(resolveOAuthCallbackMode('oidc', state), 'login')
  })

  test('explicit bind hint is treated as a bind flow', () => {
    setLocalStorage(fakeStorage())

    assert.equal(resolveOAuthCallbackMode('custom', undefined, true), 'bind')
  })

  test('stale bind marker falls back to login', () => {
    const state = 'stale-state'
    setLocalStorage(
      fakeStorage({
        [`oauth:binding:state:github:${state}`]: String(
          Date.now() - 11 * 60 * 1000
        ),
      })
    )

    assert.equal(resolveOAuthCallbackMode('github', state), 'login')
  })

  test('storage access failure falls back to login', () => {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get() {
        throw new Error('blocked')
      },
    })

    assert.equal(resolveOAuthCallbackMode('github', 'bind-state'), 'login')
  })
})
