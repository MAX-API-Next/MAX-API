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
import { act, useMemo, useState } from 'react'
import { AxiosError } from 'axios'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  getCoreRowModel,
  getFilteredRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { createReactTestEnvironment } from '@/test/react'
import { waitFor, within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { useUserStatusSelection } from '../hooks/use-user-status-selection'
import { canBatchChangeUserStatus } from '../lib/batch-status'
import type { User } from '../types'

let DataTableBulkActions: (typeof import('./data-table-bulk-actions'))['DataTableBulkActions']

const env = createReactTestEnvironment()
before(async () => {
  await env.setup()
  // Base UI determines DOM/portal availability when its modules are loaded.
  ;({ DataTableBulkActions } = await import('./data-table-bulk-actions'))
})
after(() => env.teardown())

const initialUsers = [
  {
    id: 3,
    username: 'account-three',
    role: 1,
    status: 1,
    email: '',
    group: 'default',
  },
  {
    id: 4,
    username: 'account-four',
    role: 1,
    status: 2,
    email: '',
    group: 'default',
  },
] as User[]

function Harness(props: { initialUsers?: User[] }) {
  const [scope, setScope] = useState('first')
  const [reversed, setReversed] = useState(false)
  const [fetching, setFetching] = useState(false)
  const selection = useUserStatusSelection(scope)
  const actor = useAuthStore((state) => state.auth.user)
  const users = props.initialUsers ?? initialUsers
  const data = useMemo(() => {
    const rows =
      scope === 'first'
        ? [...users]
        : [{ ...users[0], id: 99, username: 'another-page' }]
    return reversed ? rows.reverse() : rows
  }, [users, scope, reversed])
  const table = useReactTable({
    data,
    columns: [{ accessorKey: 'username' }],
    manualPagination: true,
    state: { rowSelection: selection.rowSelection },
    ...selection,
    enableRowSelection: (row) =>
      !fetching && canBatchChangeUserStatus(row.original, actor),
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  })
  return (
    <>
      <button
        onClick={() =>
          setScope((value) => (value === 'first' ? 'second' : 'first'))
        }
      >
        Change page
      </button>
      <button onClick={() => setReversed((value) => !value)}>Reverse</button>
      <button onClick={() => setFetching((value) => !value)}>Fetching</button>
      <button onClick={() => table.toggleAllRowsSelected(true)}>
        Select page
      </button>
      <output data-testid='selected-ids'>
        {table
          .getSelectedRowModel()
          .rows.map((row) => row.original.id)
          .sort()
          .join(',')}
      </output>
      {table.getRowModel().rows.map((row) => (
        <button
          key={row.id}
          disabled={!row.getCanSelect()}
          onClick={() => row.toggleSelected()}
        >
          {row.original.username}
        </button>
      ))}
      <DataTableBulkActions
        table={table}
        selectionScope={scope}
        isFetching={fetching}
      />
    </>
  )
}

async function renderHarness(users?: User[]) {
  useAuthStore
    .getState()
    .auth.setUser({ id: 2, role: 10, username: 'operator' })
  const client = new QueryClient({
    defaultOptions: {
      mutations: { retry: false, onError: (error) => handleServerError(error) },
      queries: { retry: false },
    },
  })
  const view = await env.render(
    <QueryClientProvider client={client}>
      <Harness initialUsers={users} />
    </QueryClientProvider>
  )
  return {
    ...view,
    async close() {
      await view.unmount()
      client.clear()
      useAuthStore.getState().auth.reset()
    },
  }
}

describe('Batch user status workflow', () => {
  for (const failure of ['business', 'http'] as const) {
    test(`reports ${failure} errors once while keeping account results unknown`, async () => {
      const adapter = api.defaults.adapter
      const messages: string[] = []
      const toastError = toast.error
      toast.error = (message) => {
        messages.push(String(message))
        return 0
      }
      let requests = 0
      api.defaults.adapter = async (config) => {
        requests++
        const response = {
          config,
          status: failure === 'http' ? 400 : 200,
          statusText: 'test',
          headers: {},
          data: { success: false, message: 'Batch rejected by server' },
        }
        if (failure === 'http') {
          throw new AxiosError(
            'Synthetic request failure',
            'ERR_BAD_REQUEST',
            config,
            undefined,
            response
          )
        }
        return response
      }
      const view = await renderHarness()
      try {
        await view.click(within(view.container).getByText('account-three'))
        await view.click(
          within(view.container).getByRole('button', { name: 'Batch disable' })
        )
        const dialog = within(within(document.body).getByRole('dialog'))
        await view.click(dialog.getByRole('button', { name: 'Confirm' }))
        await waitFor(() =>
          assert.ok(
            dialog.getByText(
              'Result unconfirmed. Refresh and verify before retrying.'
            )
          )
        )
        assert.deepEqual(messages, ['Batch rejected by server'])
        assert.equal(requests, 1)
        assert.equal(dialog.queryByText('Updated'), null)
        assert.equal(dialog.queryByRole('button', { name: 'Confirm' }), null)
      } finally {
        api.defaults.adapter = adapter
        toast.error = toastError
        await view.close()
      }
    })
  }

  test('keeps selection bound to IDs across reordering and clears it across pages, including returning', async () => {
    const view = await renderHarness()
    try {
      const query = within(view.container)
      await view.click(query.getByText('account-three'))
      await view.click(query.getByText('Reverse'))
      assert.equal(query.getByTestId('selected-ids').textContent, '3')
      await view.click(query.getByText('Change page'))
      assert.equal(query.getByTestId('selected-ids').textContent, '')
      await view.click(query.getByText('Change page'))
      assert.equal(query.getByTestId('selected-ids').textContent, '')
    } finally {
      await view.close()
    }
  })

  test('protects accounts in the selection UI and does not offer bulk deletion', async () => {
    const view = await renderHarness([
      ...initialUsers,
      { ...initialUsers[0], id: 2, username: 'self', role: 10 },
      { ...initialUsers[0], id: 10, username: 'peer', role: 10 },
      { ...initialUsers[0], id: 100, username: 'root', role: 100 },
      {
        ...initialUsers[0],
        id: 11,
        username: 'deleted',
        DeletedAt: '2026-01-01',
      },
    ])
    try {
      const query = within(view.container)
      for (const name of ['self', 'peer', 'root', 'deleted'])
        assert.equal(
          (query.getByText(name) as HTMLButtonElement).disabled,
          true
        )
      await view.click(query.getByText('Select page'))
      assert.equal(query.getByTestId('selected-ids').textContent, '3,4')
      assert.equal(
        query.queryByRole('button', { name: /^Batch delete$/i }),
        null
      )
    } finally {
      await view.close()
    }
  })

  test('requires confirmation, sends exact IDs and statuses once, and reports mixed results by ID', async () => {
    const adapter = api.defaults.adapter
    const requests: unknown[] = []
    let release: (() => void) | undefined
    api.defaults.adapter = async (config) => {
      requests.push({ url: config.url, payload: JSON.parse(config.data) })
      await new Promise<void>((resolve) => {
        release = resolve
      })
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: {
            results: [
              { id: 4, outcome: 'rejected', code: 'conflict' },
              { id: 3, outcome: 'updated', status: 2 },
            ],
          },
        },
      }
    }
    const view = await renderHarness()
    try {
      await view.click(within(view.container).getByText('Select page'))
      await view.click(
        within(view.container).getByRole('button', { name: 'Batch disable' })
      )
      const dialog = within(within(document.body).getByRole('dialog'))
      assert.ok(dialog.getByText('#3'))
      assert.ok(dialog.getByText('#4'))
      assert.equal(requests.length, 0)
      const confirm = dialog.getByRole('button', {
        name: 'Confirm',
      }) as HTMLButtonElement
      await view.click(confirm)
      await view.click(confirm)
      // React Query publishes pending state asynchronously; the second click
      // above still exercises the immediate duplicate-submission guard.
      await waitFor(() => assert.equal(confirm.disabled, true))
      assert.equal(
        (dialog.getByRole('button', { name: 'Cancel' }) as HTMLButtonElement)
          .disabled,
        true
      )
      assert.deepEqual(requests, [
        {
          url: '/api/user/manage/batch',
          payload: {
            action: 'disable',
            users: [
              { id: 3, status: 1 },
              { id: 4, status: 2 },
            ],
          },
        },
      ])
      await act(async () => {
        release?.()
      })
      await waitFor(() => assert.ok(dialog.getByText('Updated')))
      assert.ok(
        dialog.getByText('Account status changed. Refresh and select again.')
      )
      assert.ok(dialog.getByText('1 confirmed, 1 rejected or unconfirmed.'))
      assert.equal(dialog.queryByRole('button', { name: 'Confirm' }), null)
      assert.equal(
        within(view.container).getByTestId('selected-ids').textContent,
        ''
      )
    } finally {
      release?.()
      api.defaults.adapter = adapter
      await view.close()
    }
  })

  test('enable uses the same confirmation workflow without deleting or modifying quota', async () => {
    const adapter = api.defaults.adapter
    let payload: unknown
    api.defaults.adapter = async (config) => {
      payload = JSON.parse(config.data)
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: { results: [{ id: 4, outcome: 'updated', status: 1 }] },
        },
      }
    }
    const view = await renderHarness()
    try {
      await view.click(within(view.container).getByText('account-four'))
      await view.click(
        within(view.container).getByRole('button', { name: 'Batch enable' })
      )
      const dialog = within(within(document.body).getByRole('dialog'))
      assert.ok(dialog.getByText(/including existing valid API keys/))
      await view.click(dialog.getByRole('button', { name: 'Confirm' }))
      await waitFor(() => assert.ok(dialog.getByText('Updated')))
      assert.deepEqual(payload, {
        action: 'enable',
        users: [{ id: 4, status: 2 }],
      })
      assert.ok(dialog.getByText('Updated'))
    } finally {
      api.defaults.adapter = adapter
      await view.close()
    }
  })

  test('does not confirm after page changes or while the list is refreshing', async () => {
    const view = await renderHarness()
    try {
      await view.click(within(view.container).getByText('account-three'))
      await view.click(
        within(view.container).getByRole('button', { name: 'Batch disable' })
      )
      const dialog = within(within(document.body).getByRole('dialog'))
      await view.click(within(view.container).getByText('Fetching'))
      assert.equal(
        (dialog.getByRole('button', { name: 'Confirm' }) as HTMLButtonElement)
          .disabled,
        true
      )
      await view.click(within(view.container).getByText('Fetching'))
      await view.click(within(view.container).getByText('Change page'))
      assert.ok(
        dialog.getByText(
          'The user list changed. Close this dialog and select accounts again.'
        )
      )
      assert.equal(
        (dialog.getByRole('button', { name: 'Confirm' }) as HTMLButtonElement)
          .disabled,
        true
      )
    } finally {
      await view.close()
    }
  })

  test('network failure is unconfirmed, never retried or advertised as success', async () => {
    const adapter = api.defaults.adapter
    let count = 0
    api.defaults.adapter = async () => {
      count++
      throw new Error('connection lost')
    }
    const view = await renderHarness()
    try {
      await view.click(within(view.container).getByText('account-three'))
      await view.click(
        within(view.container).getByRole('button', { name: 'Batch disable' })
      )
      const dialog = within(within(document.body).getByRole('dialog'))
      await view.click(dialog.getByRole('button', { name: 'Confirm' }))
      await waitFor(() =>
        assert.ok(
          dialog.getByText(
            'Result unconfirmed. Refresh and verify before retrying.'
          )
        )
      )
      assert.equal(count, 1)
      assert.equal(dialog.queryByText('Updated'), null)
      assert.equal(dialog.queryByRole('button', { name: 'Confirm' }), null)
    } finally {
      api.defaults.adapter = adapter
      await view.close()
    }
  })
})
