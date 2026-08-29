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
import {
  useState,
  type ComponentProps,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createReactTestEnvironment } from '@/test/react'
import { markSecureVerificationErrorReported } from '@/lib/secure-verification'
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  waitFor,
} from '@testing-library/react'
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  mock,
  test,
} from 'bun:test'
import assert from 'node:assert/strict'

let resolveTokenKeyRequest:
  | ((value: {
      success: boolean
      data: { key: string }
    }) => void)
  | undefined
let rejectTokenKeysBatchRequest: ((reason?: unknown) => void) | undefined

const fetchTokenKey = mock(
  () =>
    new Promise<{ success: boolean; data: { key: string } }>((resolve) => {
      resolveTokenKeyRequest = resolve
    })
)
const fetchTokenKeysBatch = mock(
  async (): Promise<{
    success: boolean
    data: { keys: Record<string, string> }
  }> => ({ success: true, data: { keys: {} } })
)
const copyToClipboard = mock(async () => false)
const toastError = mock(() => undefined)
const toastSuccess = mock(() => undefined)
const createApiKey = mock(async () => ({ success: true }))
const updateApiKey = mock(async () => ({ success: true }))
const getApiKey = mock(async () => ({ success: false }))
const translate = (key: string, values?: Record<string, unknown>) =>
  key.replace(/{{\s*(\w+)\s*}}/g, (match, name: string) => {
    const value = values?.[name]
    return value === undefined ? match : String(value)
  })
const userModelsQueryResult = { data: { data: [] }, isLoading: false }
const userGroupsQueryResult = {
  data: {
    data: {
      default: { desc: 'Default', ratio: 1 },
    },
    auto_routes: [{ key: 'auto', name: 'Automatic', groups: ['default'] }],
  },
  isLoading: false,
}
const emptyQueryResult = { data: undefined, isLoading: false, error: null }

interface TestSheetContainerProps {
  children?: ReactNode
}

function TestSheetContainer(props: TestSheetContainerProps): ReactElement {
  return <div>{props.children}</div>
}

function TestInput({
  onChange,
  ...props
}: ComponentProps<'input'>): ReactElement {
  return (
    <input
      {...props}
      onInput={onChange as ComponentProps<'input'>['onInput']}
    />
  )
}

mock.module('../src/features/auth/secure-verification', () => ({
  useApiTokenVerification:
    () =>
    <T,>(apiCall: () => Promise<T>) =>
      apiCall(),
}))

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: translate, i18n: { language: 'en' } }),
}))

mock.module('sonner', () => ({
  toast: { error: toastError, success: toastSuccess },
}))

mock.module('@tanstack/react-query', () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === 'user-models') {
      return userModelsQueryResult
    }
    if (queryKey[0] === 'user-groups') {
      return userGroupsQueryResult
    }
    return emptyQueryResult
  },
}))

mock.module('../src/hooks/use-status', () => ({
  useStatus: () => ({
    status: { default_auto_route: 'auto' },
    loading: false,
    error: null,
  }),
}))

mock.module('../src/components/ui/sheet', () => ({
  Sheet: ({ open, children }: { open?: boolean; children?: ReactNode }) =>
    open ? <div>{children}</div> : null,
  SheetClose: TestSheetContainer,
  SheetContent: TestSheetContainer,
  SheetDescription: TestSheetContainer,
  SheetFooter: TestSheetContainer,
  SheetHeader: TestSheetContainer,
  SheetTitle: TestSheetContainer,
}))

mock.module('../src/components/ui/input', () => ({ Input: TestInput }))

mock.module('../src/lib/copy-to-clipboard', () => ({
  copyToClipboard,
}))

mock.module('../src/features/keys/api', () => ({
  fetchTokenKey,
  fetchTokenKeysBatch,
  createApiKey,
  updateApiKey,
  getApiKey,
}))

const { ApiKeysProvider, useApiKeys } = await import(
  '../src/features/keys/components/api-keys-provider'
)
const { ApiKeyCell } = await import(
  '../src/features/keys/components/api-keys-cells'
)
const { ApiKeysMutateDrawer } = await import(
  '../src/features/keys/components/api-keys-mutate-drawer'
)
const testEnv = createReactTestEnvironment()

interface ApiKeysProviderWrapperProps {
  children: ReactNode
}

function CopiedKeyProbe(): ReactElement {
  const copiedKeyId = useApiKeys().copiedKeyId
  return <output data-testid='copied-key-id'>{copiedKeyId ?? 'none'}</output>
}

function RefreshProbe(): ReactElement {
  return (
    <output data-testid='refresh-trigger'>{useApiKeys().refreshTrigger}</output>
  )
}

function wrapper(props: ApiKeysProviderWrapperProps): ReactElement {
  return <ApiKeysProvider>{props.children}</ApiKeysProvider>
}

beforeAll(async () => {
  await testEnv.setup()
  window.requestAnimationFrame = globalThis.requestAnimationFrame
  window.cancelAnimationFrame = globalThis.cancelAnimationFrame
  Object.defineProperties(window.HTMLElement.prototype, {
    attachEvent: { configurable: true, value: () => undefined },
    detachEvent: { configurable: true, value: () => undefined },
    scrollIntoView: { configurable: true, value: () => undefined },
  })
})

beforeEach(() => {
  fetchTokenKey.mockClear()
  fetchTokenKeysBatch.mockClear()
  copyToClipboard.mockClear()
  toastError.mockClear()
  toastSuccess.mockClear()
  createApiKey.mockClear()
  updateApiKey.mockClear()
  getApiKey.mockClear()
  resolveTokenKeyRequest = undefined
  rejectTokenKeysBatchRequest = undefined
})

afterEach(async () => {
  cleanup()
  await new Promise((resolve) => setTimeout(resolve, 50))
})

afterAll(() => testEnv.teardown())

describe('API key plaintext lifecycle', () => {
  test('reports batch reveal failures and clears every loading state', async () => {
    fetchTokenKeysBatch.mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectTokenKeysBatchRequest = reject
        })
    )
    const { result } = renderHook(() => useApiKeys(), { wrapper })

    let request!: Promise<Record<number, string>>
    act(() => {
      request = result.current.resolveRealKeysBatch([201, 202])
    })

    await waitFor(() => {
      assert.equal(result.current.loadingKeys[201], true)
      assert.equal(result.current.loadingKeys[202], true)
    })

    await act(async () => {
      rejectTokenKeysBatchRequest?.(new Error('batch request failed'))
      assert.deepEqual(await request, {})
    })

    assert.equal(toastError.mock.calls.length, 1)
    assert.equal(toastError.mock.calls[0]?.[0], 'An unexpected error occurred')
    assert.equal(result.current.loadingKeys[201], undefined)
    assert.equal(result.current.loadingKeys[202], undefined)
  })

  test('reports single-key reveal failures with the generic fallback', async () => {
    fetchTokenKey.mockImplementationOnce(async () => {
      throw new Error('single key request failed')
    })
    const { result } = renderHook(() => useApiKeys(), { wrapper })

    await act(async () => {
      assert.equal(await result.current.resolveRealKey(203), null)
    })

    assert.equal(toastError.mock.calls.length, 1)
    assert.equal(toastError.mock.calls[0]?.[0], 'An unexpected error occurred')
    assert.equal(result.current.loadingKeys[203], undefined)
  })

  test('does not duplicate batch errors already reported by verification', async () => {
    const verificationError = new Error('batch verification failed')
    markSecureVerificationErrorReported(verificationError)
    fetchTokenKeysBatch.mockImplementationOnce(async () => {
      throw verificationError
    })
    const { result } = renderHook(() => useApiKeys(), { wrapper })

    await act(async () => {
      assert.deepEqual(await result.current.resolveRealKeysBatch([205, 206]), {})
    })

    assert.equal(toastError.mock.calls.length, 0)
    assert.equal(result.current.loadingKeys[205], undefined)
    assert.equal(result.current.loadingKeys[206], undefined)
  })

  test('does not duplicate single-key errors already reported by verification', async () => {
    const verificationError = new Error('verification failed')
    markSecureVerificationErrorReported(verificationError)
    fetchTokenKey.mockImplementationOnce(async () => {
      throw verificationError
    })
    const { result } = renderHook(() => useApiKeys(), { wrapper })

    await act(async () => {
      assert.equal(await result.current.resolveRealKey(204), null)
    })

    assert.equal(toastError.mock.calls.length, 0)
    assert.equal(result.current.loadingKeys[204], undefined)
  })

  test('does not cache a key that resolves after its reveal popover closes', async () => {
    const { result } = renderHook(() => useApiKeys(), { wrapper })

    let request: Promise<string | null> | undefined
    act(() => {
      request = result.current.resolveRealKey(101, { cache: true })
    })

    await waitFor(() => assert.equal(fetchTokenKey.mock.calls.length, 1))
    act(() => result.current.clearResolvedKey(101))

    await act(async () => {
      resolveTokenKeyRequest?.({
        success: true,
        data: { key: 'plaintext-test-key' },
      })
      assert.equal(await request, 'sk-plaintext-test-key')
    })

    assert.equal(result.current.resolvedKeys[101], undefined)
  })

  test('reports clipboard failures without marking the API key as copied', async () => {
    fetchTokenKey.mockImplementationOnce(async () => ({
      success: true,
      data: { key: 'plaintext-copy-key' },
    }))

    const view = render(
      <ApiKeysProvider>
        <ApiKeyCell
          apiKey={{
            id: 102,
            name: 'copy-test',
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
          }}
        />
        <CopiedKeyProbe />
      </ApiKeysProvider>
    )

    fireEvent.click(view.getByRole('button', { name: 'Copy API key' }))

    await waitFor(() => {
      assert.equal(copyToClipboard.mock.calls.length, 1)
      assert.equal(toastError.mock.calls.length, 1)
      assert.equal(toastError.mock.calls[0]?.[0], 'Failed to copy to clipboard')
      assert.equal(view.getByTestId('copied-key-id').textContent, 'none')
    })
  })

  test('finalizes a partially successful batch when a later create throws', async () => {
    createApiKey
      .mockImplementationOnce(async () => ({ success: true }))
      .mockImplementationOnce(async () => {
        throw new Error('second create failed')
      })
    const onOpenChange = mock(() => undefined)

    function DrawerHarness(): ReactElement {
      const [open, setOpen] = useState(true)
      return (
        <>
          <ApiKeysMutateDrawer
            open={open}
            onOpenChange={(nextOpen) => {
              onOpenChange(nextOpen)
              setOpen(nextOpen)
            }}
          />
          <RefreshProbe />
        </>
      )
    }

    const view = render(
      <ApiKeysProvider>
        <DrawerHarness />
      </ApiKeysProvider>
    )

    await waitFor(() => {
      assert.ok(view.getByRole('textbox', { name: 'Name' }))
    })
    fireEvent.input(view.getByRole('textbox', { name: 'Name' }), {
      target: { value: 'partial-batch' },
    })
    fireEvent.input(view.getByRole('spinbutton', { name: 'Quantity' }), {
      target: { value: '2' },
    })
    fireEvent.click(view.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => {
      assert.equal(createApiKey.mock.calls.length, 2)
      assert.equal(toastError.mock.calls.length, 1)
      assert.equal(
        toastError.mock.calls[0]?.[0],
        'An unexpected error occurred'
      )
      assert.equal(toastSuccess.mock.calls.length, 1)
      assert.equal(
        toastSuccess.mock.calls[0]?.[0],
        'Successfully created 1 API Key(s)'
      )
      assert.ok(onOpenChange.mock.calls.some(([open]) => open === false))
      assert.equal(view.getByTestId('refresh-trigger').textContent, '1')
      assert.equal(
        view.queryByRole('button', { name: 'Save changes' }),
        null
      )
    })
  })
})
