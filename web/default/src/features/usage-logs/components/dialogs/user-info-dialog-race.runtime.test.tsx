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
import { createReactTestEnvironment } from '@/test/react'
import { waitFor, within } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { api } from '@/lib/api'

const env = createReactTestEnvironment()
await env.setup()
const { UserInfoDialog } = await import('./user-info-dialog')
after(() => env.teardown())

test('a late user response cannot replace the selected user or their balance', async () => {
  const adapter = api.defaults.adapter
  const pending = new Map<string, () => void>()
  const requests: string[] = []
  api.defaults.adapter = async (config) => {
    const id = String(config.url?.split('/').at(-1))
    requests.push(id)
    await new Promise<void>((resolve) => pending.set(id, resolve))
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: {
          username: `user-${id}`,
          quota: id === '1' ? 900000 : 0,
          used_quota: 0,
          request_count: 0,
        },
      },
    }
  }
  function Harness() {
    const [id, setId] = useState(1)
    return (
      <>
        <button onClick={() => setId(2)}>Select another user</button>
        <UserInfoDialog open userId={id} onOpenChange={() => {}} />
      </>
    )
  }
  const view = await env.render(<Harness />)
  const body = within(env.dom.window.document.body)
  try {
    await waitFor(() => assert.ok(pending.has('1')))
    await view.click(within(view.container).getByText('Select another user'))
    await waitFor(() => assert.ok(pending.has('2')))
    await act(async () => pending.get('2')!())
    assert.ok(body.queryByText('user-2'))
    await act(async () => pending.get('1')!())
    assert.ok(body.queryByText('user-2'))
    assert.equal(body.queryByText('user-1'), null)
    await act(async () => {
      await env.i18n.changeLanguage('fr')
    })
    assert.deepEqual(requests, ['1', '2'])
  } finally {
    for (const resolve of pending.values()) resolve()
    await view.unmount()
    api.defaults.adapter = adapter
  }
})
