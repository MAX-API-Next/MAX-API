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
import type { ReactElement, ReactNode } from 'react'
import { createReactTestEnvironment } from '@/test/react'
import { cleanup, fireEvent, render, waitFor } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, beforeEach, mock, test } from 'bun:test'
import assert from 'node:assert/strict'

let resolveKeyRequest: ((value: string | null) => void) | undefined
let resolvePresetVerification: ((value: string | null) => void) | undefined
const resolveRealKey = mock(
  () =>
    new Promise<string | null>((resolve) => {
      resolveKeyRequest = resolve
    })
)
const withApiTokenVerification = mock(
  (_apiCall: () => Promise<string>) =>
    new Promise<string | null>((resolve) => {
      resolvePresetVerification = resolve
    })
)
const setOpenMobile = mock((_open: boolean) => undefined)
const replaceLocation = mock((_url: string) => undefined)
const closePopup = mock(() => undefined)
const popupWindow = {
  closed: false,
  close: closePopup,
  location: { replace: replaceLocation },
  opener: {} as Window,
} as unknown as Window
const openWindow = mock(() => popupWindow)
const copyToClipboard = mock(async () => false)
const toastError = mock(() => undefined)

function Container(props: { children?: ReactNode }): ReactElement {
  return <div>{props.children}</div>
}

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

mock.module('sonner', () => ({
  toast: {
    error: toastError,
    info: mock(() => undefined),
    success: mock(() => undefined),
  },
}))

mock.module('../src/lib/copy-to-clipboard', () => ({ copyToClipboard }))

mock.module('../src/components/ui/button', () => ({
  Button: (props: {
    children?: ReactNode
    onClick?: () => void
    type?: 'button' | 'submit' | 'reset'
  }) => (
    <button type={props.type} onClick={props.onClick}>
      {props.children}
    </button>
  ),
}))

mock.module('../src/components/ui/dropdown-menu', () => ({
  DropdownMenu: Container,
  DropdownMenuContent: Container,
  DropdownMenuItem: (props: {
    children?: ReactNode
    onClick?: () => void
  }) => <button onClick={props.onClick}>{props.children}</button>,
  DropdownMenuSeparator: () => null,
  DropdownMenuSub: Container,
  DropdownMenuSubContent: Container,
  DropdownMenuSubTrigger: Container,
  DropdownMenuShortcut: Container,
  DropdownMenuTrigger: (props: {
    children?: ReactNode
    render?: ReactElement
  }) => (
    <>
      {props.render}
      {props.children}
    </>
  ),
}))

mock.module('../src/components/ui/tooltip', () => ({
  Tooltip: Container,
  TooltipContent: Container,
  TooltipTrigger: (props: {
    children?: ReactNode
    render?: ReactElement
  }) => (
    <>
      {props.render}
      {props.children}
    </>
  ),
}))

mock.module('@tanstack/react-router', () => ({
  Link: (props: { children?: ReactNode }) => <a>{props.children}</a>,
  useLocation: (options: {
    select: (location: { href: string }) => string
  }) => options.select({ href: '/dashboard' }),
}))

mock.module('../src/components/ui/collapsible', () => ({
  Collapsible: Container,
  CollapsibleContent: Container,
  CollapsibleTrigger: Container,
}))

mock.module('../src/components/ui/sidebar', () => ({
  SidebarMenuButton: Container,
  SidebarMenuItem: Container,
  SidebarMenuSub: Container,
  SidebarMenuSubButton: (props: {
    children?: ReactNode
    onClick?: () => void
  }) => <button onClick={props.onClick}>{props.children}</button>,
  SidebarMenuSubItem: Container,
  useSidebar: () => ({
    state: 'expanded',
    isMobile: false,
    setOpenMobile,
  }),
}))

mock.module('../src/features/auth/secure-verification', () => ({
  useApiTokenVerification: () => withApiTokenVerification,
}))

mock.module('../src/features/chat/hooks/use-active-chat-key', () => ({
  fetchActiveChatKey: mock(async () => 'resolved-key'),
}))

mock.module('../src/features/chat/hooks/use-chat-presets', () => ({
  useChatPresets: () => ({
    serverAddress: 'https://api.example.test',
    chatPresets: [
      {
        id: 'browser-chat',
        name: 'Browser chat',
        type: 'web',
        url: 'https://chat.example.test/?key={key}',
      },
      {
        id: 'desktop-chat',
        name: 'Desktop chat',
        type: 'custom-protocol',
        url: 'desktop-chat://connect?key={key}',
      },
      {
        id: 'public-chat',
        name: 'Public chat',
        type: 'custom-protocol',
        url: 'https://chat.example.test/public',
      },
    ],
  }),
}))

mock.module('../src/features/chat/lib/chat-links', () => ({
  chatLinkRequiresApiKey: (url: string) => url.includes('{key}'),
  resolveChatUrl: ({ template }: { template: string }) =>
    template.includes('{key}')
      ? 'https://chat.example.test/?key=resolved-key'
      : 'https://chat.example.test/public',
}))

mock.module('../src/features/chat/lib/send-to-fluent', () => ({
  sendToFluent: () => false,
}))

mock.module('../src/features/keys/api', () => ({
  updateApiKeyStatus: mock(async () => ({ success: true })),
}))

mock.module('../src/features/keys/components/api-keys-provider', () => ({
  useApiKeys: () => ({
    setOpen: mock(() => undefined),
    setCurrentRow: mock(() => undefined),
    triggerRefresh: mock(() => undefined),
    setResolvedKey: mock(() => undefined),
    resolveRealKey,
  }),
}))

const { DataTableRowActions } = await import(
  '../src/features/keys/components/data-table-row-actions'
)
const { ChatPresetsItem } = await import(
  '../src/components/layout/components/chat-presets-item'
)
const testEnv = createReactTestEnvironment()
let originalWindowOpen: typeof window.open

function renderRowActions(): ReturnType<typeof render> {
  return render(
    <DataTableRowActions
      row={
        {
          original: {
            id: 42,
            name: 'chat-key',
            key: 'masked-key',
            status: 1,
            remain_quota: 100,
            used_quota: 0,
            unlimited_quota: false,
            expired_time: -1,
            created_time: 0,
            accessed_time: 0,
            group: 'default',
            cross_group_retry: false,
            routing: null,
            model_limits_enabled: false,
            model_limits: '',
            allow_ips: '',
          },
        } as never
      }
    />
  )
}

function renderChatPresets(): ReturnType<typeof render> {
  return render(<ChatPresetsItem item={{ title: 'Chat clients' } as never} />)
}

beforeAll(async () => {
  await testEnv.setup()
  originalWindowOpen = window.open
  window.open = openWindow as typeof window.open
})

beforeEach(() => {
  resolveKeyRequest = undefined
  resolvePresetVerification = undefined
  resolveRealKey.mockClear()
  withApiTokenVerification.mockClear()
  withApiTokenVerification.mockImplementation(
    (_apiCall: () => Promise<string>) =>
      new Promise<string | null>((resolve) => {
        resolvePresetVerification = resolve
      })
  )
  setOpenMobile.mockClear()
  openWindow.mockClear()
  replaceLocation.mockClear()
  closePopup.mockClear()
  copyToClipboard.mockClear()
  toastError.mockClear()
  popupWindow.opener = {} as Window
})

afterEach(() => cleanup())

afterAll(() => {
  window.open = originalWindowOpen
  testEnv.teardown()
})

test('opens a placeholder before resolving the API key', async () => {
  const view = renderRowActions()

  fireEvent.click(view.getByRole('button', { name: /Browser chat/ }))

  assert.equal(openWindow.mock.calls.length, 1)
  assert.equal(replaceLocation.mock.calls.length, 0)

  resolveKeyRequest?.('resolved-key')
  await waitFor(() => assert.equal(replaceLocation.mock.calls.length, 1))
  assert.deepEqual(replaceLocation.mock.calls[0], [
    'https://chat.example.test/?key=resolved-key',
  ])
  assert.equal(popupWindow.opener, null)
})

test('opens a placeholder for custom-protocol presets before resolving the API key', async () => {
  const view = renderRowActions()

  fireEvent.click(view.getByRole('button', { name: /Desktop chat/ }))

  assert.equal(openWindow.mock.calls.length, 1)
  assert.deepEqual(openWindow.mock.calls[0], ['about:blank', '_blank'])
  assert.equal(resolveRealKey.mock.calls.length, 1)
  assert.equal(replaceLocation.mock.calls.length, 0)

  resolveKeyRequest?.('resolved-key')
  await waitFor(() => assert.equal(replaceLocation.mock.calls.length, 1))
  assert.deepEqual(replaceLocation.mock.calls[0], [
    'https://chat.example.test/?key=resolved-key',
  ])
})

test('navigates public presets without resolving the API key', async () => {
  const view = renderRowActions()

  fireEvent.click(view.getByRole('button', { name: /Public chat/ }))

  await waitFor(() => assert.equal(replaceLocation.mock.calls.length, 1))
  assert.equal(resolveRealKey.mock.calls.length, 0)
  assert.deepEqual(replaceLocation.mock.calls[0], [
    'https://chat.example.test/public',
  ])
  assert.equal(openWindow.mock.calls.length, 1)
})

test('chat presets open a placeholder before secure API-key verification resolves', async () => {
  const view = renderChatPresets()

  fireEvent.click(view.getByRole('button', { name: /Desktop chat/ }))

  assert.equal(openWindow.mock.calls.length, 1)
  assert.deepEqual(openWindow.mock.calls[0], ['about:blank', '_blank'])
  assert.equal(replaceLocation.mock.calls.length, 0)

  resolvePresetVerification?.('resolved-key')
  await waitFor(() => assert.equal(replaceLocation.mock.calls.length, 1))
  assert.deepEqual(replaceLocation.mock.calls[0], [
    'https://chat.example.test/?key=resolved-key',
  ])
  assert.equal(popupWindow.opener, null)
  assert.deepEqual(setOpenMobile.mock.calls[0], [false])
})

test('chat presets close the placeholder when secure verification is cancelled', async () => {
  withApiTokenVerification.mockImplementationOnce(async () => null)
  const view = renderChatPresets()

  fireEvent.click(view.getByRole('button', { name: /Desktop chat/ }))

  await waitFor(() => assert.equal(closePopup.mock.calls.length, 1))
  assert.equal(replaceLocation.mock.calls.length, 0)
})

test('chat presets without an API key use a placeholder before navigating', async () => {
  const view = renderChatPresets()

  fireEvent.click(view.getByRole('button', { name: /Public chat/ }))

  await waitFor(() => assert.equal(openWindow.mock.calls.length, 1))
  assert.deepEqual(openWindow.mock.calls[0], ['about:blank', '_blank'])
  await waitFor(() => assert.equal(replaceLocation.mock.calls.length, 1))
  assert.deepEqual(replaceLocation.mock.calls[0], [
    'https://chat.example.test/public',
  ])
  assert.deepEqual(setOpenMobile.mock.calls[0], [false])
})

test('reports a clipboard failure when copying an API key', async () => {
  resolveRealKey.mockImplementationOnce(async () => 'resolved-key')
  const view = renderRowActions()

  fireEvent.click(view.getByRole('button', { name: /Copy Key/ }))

  await waitFor(() => {
    assert.equal(copyToClipboard.mock.calls.length, 1)
    assert.deepEqual(toastError.mock.calls[0], ['Failed to copy to clipboard'])
  })
})

test('reports a clipboard failure when copying connection info', async () => {
  resolveRealKey.mockImplementationOnce(async () => 'resolved-key')
  const view = renderRowActions()

  fireEvent.click(
    view.getByRole('button', { name: /Copy Connection Info/ })
  )

  await waitFor(() => {
    assert.equal(copyToClipboard.mock.calls.length, 1)
    assert.deepEqual(toastError.mock.calls[0], ['Failed to copy to clipboard'])
  })
})
