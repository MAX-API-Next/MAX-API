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
import { createReactTestEnvironment } from '@/test/react'
import {
  act,
  cleanup,
  fireEvent,
  render,
  waitFor,
} from '@testing-library/react'
import { afterAll, afterEach, beforeEach, mock, spyOn, test } from 'bun:test'
import assert from 'node:assert/strict'
import { I18nextProvider } from 'react-i18next'

const testEnv = createReactTestEnvironment({
  url: 'https://console.example/channels?page=2#models',
})
await testEnv.setup()
const {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Link,
  Outlet,
  RouterProvider,
} = await import('@tanstack/react-router')
const { GeneralError } = await import('../src/features/errors/general-error')
const browserWindow = window
const previousScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo')
Object.defineProperty(globalThis, 'scrollTo', {
  configurable: true,
  value: () => undefined,
})
const reload = mock(() => undefined)
const consoleError = spyOn(console, 'error').mockImplementation(() => undefined)
const consoleWarn = spyOn(console, 'warn').mockImplementation(() => undefined)

beforeEach(() => {
  browserWindow.sessionStorage.clear()
  reload.mockClear()
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    writable: true,
    value: new Proxy(browserWindow, {
      get(target, property) {
        if (property === 'location') return { ...target.location, reload }
        return Reflect.get(target, property, target)
      },
    }),
  })
  const entry = document.createElement('script')
  entry.defer = true
  entry.src = '/static/js/index.oldbuild.js'
  document.head.append(entry)
})

afterEach(() => {
  cleanup()
  document.head.querySelectorAll('script').forEach((script) => script.remove())
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    writable: true,
    value: browserWindow,
  })
})

afterAll(() => {
  consoleError.mockRestore()
  consoleWarn.mockRestore()
  if (previousScrollTo)
    Object.defineProperty(globalThis, 'scrollTo', previousScrollTo)
  else Reflect.deleteProperty(globalThis, 'scrollTo')
  testEnv.teardown()
})

function createTestRouter(error: unknown): ReturnType<typeof createRouter> {
  const root = createRootRoute({
    component: Outlet,
    errorComponent: GeneralError,
  })
  const home = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => (
      <>
        <p>Current page</p>
        <Link to='/channels'>Channels</Link>
      </>
    ),
  })
  const broken = createRoute({
    getParentRoute: () => root,
    path: '/channels',
    component: lazyRouteComponent(() => Promise.reject(error)),
  })
  return createRouter({
    routeTree: root.addChildren([home, broken]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
    defaultPreload: false,
  })
}

function renderRouter(
  router: ReturnType<typeof createTestRouter>
): ReturnType<typeof render> {
  return render(
    <I18nextProvider i18n={testEnv.i18n}>
      <RouterProvider router={router} />
    </I18nextProvider>
  )
}

test.each([
  Object.assign(new Error('Loading chunk 42 failed. (missing: /42.old.js)'), {
    name: 'ChunkLoadError',
  }),
  new Error('Loading CSS chunk 42 failed. (/42.old.css)'),
  Object.assign(new Error('Stylesheet request failed'), {
    code: 'CSS_CHUNK_LOAD_FAILED',
  }),
])('recovers a displayed route resource failure: %s', async (error) => {
  const router = createTestRouter(error)
  const view = renderRouter(router)
  await view.findByText('Current page')
  await act(() => router.navigate({ to: '/channels' }))
  await waitFor(() => assert.equal(reload.mock.calls.length, 1))
  assert.equal(view.queryByText('500'), null)
  assert.ok(view.getByText('Page resources could not be loaded'))
})

test('limits different chunk failures to one reload per loaded build, then allows a new build', async () => {
  const builds = ['oldbuild', 'oldbuild', 'newbuild', 'oldbuild', 'newbuild']
  const expectedReloads = [1, 1, 2, 2, 2]
  for (const [index, build] of builds.entries()) {
    document.querySelector<HTMLScriptElement>('script')!.src =
      `/static/js/index.${build}.js`
    const error = Object.assign(new Error(`Loading chunk ${index} failed.`), {
      name: 'ChunkLoadError',
    })
    const router = createTestRouter(error)
    const view = renderRouter(router)
    await view.findByText('Current page')
    await act(() => router.navigate({ to: '/channels' }))
    await view.findByText('Page resources could not be loaded')
    assert.equal(reload.mock.calls.length, expectedReloads[index])
    view.unmount()
  }
})

test('preserves the legacy attempted build when migrating reload history', async () => {
  const legacyBuild = document.querySelector<HTMLScriptElement>('script')!.src
  browserWindow.sessionStorage.setItem(
    'maxapi:chunk-reload-build',
    legacyBuild
  )
  for (const build of ['newbuild', 'oldbuild']) {
    document.querySelector<HTMLScriptElement>('script')!.src =
      `/static/js/index.${build}.js`
    const router = createTestRouter(new Error('Loading chunk 46 failed.'))
    const view = renderRouter(router)
    await view.findByText('Current page')
    await act(() => router.navigate({ to: '/channels' }))
    await view.findByText('Page resources could not be loaded')
    assert.equal(reload.mock.calls.length, 1)
    assert.equal(
      browserWindow.sessionStorage.getItem('maxapi:chunk-reload-build'),
      legacyBuild,
      'new replicas must not overwrite the marker still read by old replicas'
    )
    view.unmount()
  }
})

test('invalid reload history retains manual recovery without auto reload', async () => {
  browserWindow.sessionStorage.setItem('maxapi:chunk-reload-builds', '[false]')
  const router = createTestRouter(new Error('Loading chunk 47 failed.'))
  const view = renderRouter(router)
  await view.findByText('Current page')
  await act(() => router.navigate({ to: '/channels' }))
  await view.findByText('Page resources could not be loaded')
  assert.equal(reload.mock.calls.length, 0)
  fireEvent.click(view.getByRole('button', { name: 'Reload page' }))
  assert.equal(reload.mock.calls.length, 1)
})

test('hover and failed route preload leave the page usable until navigation', async () => {
  const router = createTestRouter(new Error('Loading chunk 43 failed.'))
  const view = renderRouter(router)
  await view.findByText('Current page')
  await waitFor(() => assert.equal(router.state.isLoading, false))
  await act(async () => {
    fireEvent.mouseEnter(view.getByRole('link', { name: 'Channels' }))
  })
  await act(() => router.preloadRoute({ to: '/channels' }))
  assert.ok(view.getByText('Current page'))
  assert.equal(reload.mock.calls.length, 0)
  await act(() => router.navigate({ to: '/channels' }))
  await waitFor(() => assert.equal(reload.mock.calls.length, 1))
})

test('an offline resource error leaves recovery to the explicit reload action', async () => {
  const online = Object.getOwnPropertyDescriptor(
    browserWindow.navigator,
    'onLine'
  )
  Object.defineProperty(browserWindow.navigator, 'onLine', {
    configurable: true,
    value: false,
  })
  try {
    const router = createTestRouter(new Error('Loading chunk 45 failed.'))
    const view = renderRouter(router)
    await view.findByText('Current page')
    await act(() => router.navigate({ to: '/channels' }))
    await view.findByText('Page resources could not be loaded')
    assert.equal(reload.mock.calls.length, 0)
    fireEvent.click(view.getByRole('button', { name: 'Reload page' }))
    assert.equal(reload.mock.calls.length, 1)
  } finally {
    if (online) Object.defineProperty(browserWindow.navigator, 'onLine', online)
    else Reflect.deleteProperty(browserWindow.navigator, 'onLine')
  }
})

test.each([undefined, 401, 429, 500])(
  'preserves an ordinary error status %s without reloading',
  async (status) => {
    const error = status ? { response: { status } } : new Error('Render failed')
    const router = createTestRouter(error)
    const view = renderRouter(router)
    await view.findByText('Current page')
    await act(() => router.navigate({ to: '/channels' }))
    await view.findByText(
      status === 429 ? 'Too many requests' : /Oops! Something went wrong/
    )
    if (status) assert.ok(view.getByRole('heading', { name: String(status) }))
    else assert.equal(view.queryByText('500'), null)
    assert.equal(reload.mock.calls.length, 0)
  }
)

test('blocked storage retains manual reload and the current full URL', async () => {
  const storage = spyOn(
    browserWindow.Storage.prototype,
    'setItem'
  ).mockImplementation(() => {
    throw new Error('Storage blocked')
  })
  try {
    const router = createTestRouter(new Error('Loading chunk 44 failed.'))
    const view = renderRouter(router)
    await view.findByText('Current page')
    await act(() => router.navigate({ to: '/channels' }))
    await view.findByText('Page resources could not be loaded')
    assert.equal(reload.mock.calls.length, 0)
    fireEvent.click(view.getByRole('button', { name: 'Reload page' }))
    assert.equal(reload.mock.calls.length, 1)
    assert.equal(
      window.location.href,
      'https://console.example/channels?page=2#models'
    )
  } finally {
    storage.mockRestore()
  }
})
